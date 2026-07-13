package wikipedia

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	pbzip2 "github.com/d4l3k/go-pbzip2"
	_ "modernc.org/sqlite"
)

// storeSchemaVersion guards against opening a cache written by an
// incompatible version of this code; bump it whenever the schema changes.
const storeSchemaVersion = "1"

// store is the on-disk title -> (id, seek) index, backed by SQLite. Article
// bodies are never stored here: GetArticle always decompresses the relevant
// bzip2 stream straight from the dump file on disk. Keeping just this small
// index in SQLite instead of a bespoke in-memory structure means opening it
// is just opening a file - no parsing, no sorting, no multi-gigabyte heap -
// and steady-state memory is bounded by SQLite's own page cache rather than
// by the number of pages in the dump.
type store struct {
	db *sql.DB
}

// openStore opens a previously built cache file read-only. It returns an
// error satisfying os.IsNotExist if the file does not exist, and any other
// error (corruption, schema mismatch, ...) if it exists but cannot be used -
// both are treated as "rebuild" by the caller.
func openStore(path string) (*store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// A single connection for the store's whole lifetime: the data is
	// immutable once built, so a pool buys nothing, and it guarantees the
	// per-connection PRAGMAs below apply to every query instead of only
	// whichever connection happens to be handed out of a bigger pool.
	db.SetMaxOpenConns(1)
	for _, p := range []string{
		"PRAGMA query_only = 1",
		"PRAGMA cache_size = -8000",     // ~8MB page cache cap
		"PRAGMA mmap_size = 1073741824", // let the OS page in data lazily, on demand
	} {
		if err := execPragma(db, p); err != nil {
			db.Close()
			return nil, err
		}
	}
	s := &store{db: db}
	if _, err := s.meta("schema_version"); err != nil {
		db.Close()
		return nil, fmt.Errorf("invalid or incompatible index cache: %w", err)
	}
	return s, nil
}

func (s *store) close() error { return s.db.Close() }

func (s *store) meta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("missing meta key %q", key)
	}
	return v, err
}

// sourceMatches reports whether this cache was built from a source index of
// exactly this size and modification time - our staleness check.
func (s *store) sourceMatches(size, modTime int64) bool {
	sv, err1 := s.meta("src_size")
	mv, err2 := s.meta("src_mod_time")
	if err1 != nil || err2 != nil {
		return false
	}
	return sv == strconv.FormatInt(size, 10) && mv == strconv.FormatInt(modTime, 10)
}

func (s *store) pages() int {
	v, err := s.meta("pages")
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(v)
	return n
}

// find looks up the exact byte-for-byte title.
func (s *store) find(title string) (seek int64, id int, ok bool) {
	err := s.db.QueryRow(`SELECT seek, id FROM pages WHERE title = ?`, title).Scan(&seek, &id)
	if err != nil {
		return 0, 0, false
	}
	return seek, id, true
}

// searchPrefix returns up to limit (title, id) pairs whose title starts with
// prefix, ordered by title. SQLite's default BINARY collation on TEXT
// columns compares raw bytes, so "title >= prefix AND title < upperBound" is
// an index-only range scan equivalent to the byte-prefix search used
// before, without ever pulling the whole title set into Go.
func (s *store) searchPrefix(prefix string, limit int) ([]SearchResult, error) {
	rows, err := s.db.Query(
		`SELECT title, id FROM pages WHERE title >= ? AND title < ? ORDER BY title LIMIT ?`,
		prefix, prefixUpperBound(prefix), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Title, &r.ID); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// streamEnd returns the smallest indexed seek offset greater than seek, or
// fileSize if seek belongs to the last stream. This reuses the seek index
// already needed for page lookups instead of maintaining a second
// structure, and is equivalent to a binary search over distinct stream
// offsets: multiple pages sharing the same seek don't change the result.
func (s *store) streamEnd(seek, fileSize int64) int64 {
	var next int64
	err := s.db.QueryRow(`SELECT seek FROM pages WHERE seek > ? ORDER BY seek LIMIT 1`, seek).Scan(&next)
	if err != nil {
		return fileSize
	}
	return next
}

// prefixUpperBound returns the smallest string greater than every string
// with the given prefix, for use as an exclusive range bound.
func prefixUpperBound(prefix string) string {
	b := []byte(prefix)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < 0xff {
			b[i]++
			return string(b[:i+1])
		}
	}
	// prefix is all 0xff bytes: no finite upper bound exists (never
	// happens for real titles), so match everything after it.
	return string(append(b, 0xff))
}

func execPragma(db *sql.DB, pragma string) error {
	rows, err := db.Query(pragma)
	if err != nil {
		return err
	}
	return rows.Close()
}

// buildFileStore parses a Wikipedia multistream index file (lines of the
// form "seek:id:title") and loads it into a fresh SQLite database at
// dbPath, returning the number of rows written. limit > 0 caps rows (used
// by tests and benchmarks).
//
// Rows stream straight from the scanner into the table one at a time inside
// a single transaction, so the Go heap never holds more than the current
// line - unlike sorting a full in-memory copy of every title, this scales
// to the ~30M-row enwiki index without a multi-gigabyte allocation. The
// title/seek indexes are created only after all rows are loaded: SQLite
// then builds each with one external sort-and-build pass instead of
// maintaining a B-tree one random insert at a time, which is both much
// faster and (left at SQLite's default file-based temp store, deliberately
// not overridden to memory) keeps that sort's working set on disk rather
// than in RSS.
func buildFileStore(indexPath, dbPath string, limit int, srcSize, srcModTime int64) (int, error) {
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // SQLite allows one writer; avoid pool churn during build

	for _, p := range []string{
		"PRAGMA journal_mode = OFF",
		"PRAGMA synchronous = OFF",
		"PRAGMA cache_size = -20000", // ~20MB cap on the build's own page cache
	} {
		if err := execPragma(db, p); err != nil {
			return 0, err
		}
	}

	if _, err := db.Exec(`CREATE TABLE pages (title TEXT NOT NULL, id INTEGER NOT NULL, seek INTEGER NOT NULL)`); err != nil {
		return 0, err
	}
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return 0, err
	}

	f, err := os.Open(indexPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r, err := pbzip2.NewReader(f)
	if err != nil {
		return 0, err
	}
	defer r.Close()

	n, err := insertPages(db, r, limit)
	if err != nil {
		return 0, err
	}

	slog.Info("building index B-tree", "entries", n)
	if _, err := db.Exec(`CREATE UNIQUE INDEX idx_pages_title ON pages(title)`); err != nil {
		return 0, err
	}
	if _, err := db.Exec(`CREATE INDEX idx_pages_seek ON pages(seek)`); err != nil {
		return 0, err
	}

	if err := writeMeta(db, map[string]string{
		"schema_version": storeSchemaVersion,
		"src_size":       strconv.FormatInt(srcSize, 10),
		"src_mod_time":   strconv.FormatInt(srcModTime, 10),
		"pages":          strconv.Itoa(n),
	}); err != nil {
		return 0, err
	}

	if err := execPragma(db, "PRAGMA journal_mode = DELETE"); err != nil {
		slog.Warn("could not reset journal mode after build", "error", err)
	}

	return n, nil
}

func insertPages(db *sql.DB, r io.Reader, limit int) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	stmt, err := tx.Prepare(`INSERT INTO pages (title, id, seek) VALUES (?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	defer stmt.Close()

	n := 0
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		seekStr, rest, ok := strings.Cut(line, ":")
		if !ok {
			tx.Rollback()
			return 0, fmt.Errorf("malformed index line %d: %q", n+1, line)
		}
		idStr, title, ok := strings.Cut(rest, ":")
		if !ok {
			tx.Rollback()
			return 0, fmt.Errorf("malformed index line %d: %q", n+1, line)
		}
		seek, err := strconv.ParseInt(seekStr, 10, 64)
		if err != nil {
			tx.Rollback()
			return 0, fmt.Errorf("index line %d: %w", n+1, err)
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			tx.Rollback()
			return 0, fmt.Errorf("index line %d: %w", n+1, err)
		}
		if _, err := stmt.Exec(title, id, seek); err != nil {
			tx.Rollback()
			return 0, err
		}
		n++
		if n%5_000_000 == 0 {
			slog.Info("index progress", "entries", n)
		}
		if limit > 0 && n >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

func writeMeta(db *sql.DB, kv map[string]string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO meta (key, value) VALUES (?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for k, v := range kv {
		if _, err := stmt.Exec(k, v); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
