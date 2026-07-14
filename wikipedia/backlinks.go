package wikipedia

import (
	"compress/bzip2"
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// backlinksSchemaVersion guards against opening a cache written by an
// incompatible version of this code; bump it whenever the schema changes.
const backlinksSchemaVersion = "1"

// ErrBacklinksNotReady indicates the "what links here" index hasn't
// finished its (background, potentially long-running) build yet.
var ErrBacklinksNotReady = errors.New("backlinks index not ready")

// Backlink is one entry in a page's incoming-links list.
type Backlink struct {
	Title string `json:"title"`
}

// backlinkStore is the on-disk reverse link index: for a target page id, the
// titles of every page whose wikitext links to it. It is built once, in the
// background (see buildBacklinksAsync), from the full article dump - unlike
// the title index, this requires decompressing and parsing every page's
// text, not just the small index file, so it is deliberately never on the
// startup critical path.
type backlinkStore struct {
	db *sql.DB
}

func backlinksCachePath(indexPath, cacheDir string) string {
	base := strings.TrimSuffix(filepath.Base(indexPath), ".txt.bz2") + "-backlinks.sqlite"
	if cacheDir == "" {
		cacheDir = filepath.Dir(indexPath)
	}
	return filepath.Join(cacheDir, base)
}

func openBacklinkStore(path string) (*backlinkStore, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, p := range []string{
		"PRAGMA query_only = 1",
		"PRAGMA cache_size = -8000",
		"PRAGMA mmap_size = 1073741824",
	} {
		if err := execPragma(db, p); err != nil {
			db.Close()
			return nil, err
		}
	}
	b := &backlinkStore{db: db}
	if _, err := b.meta("schema_version"); err != nil {
		db.Close()
		return nil, fmt.Errorf("invalid or incompatible backlinks cache: %w", err)
	}
	return b, nil
}

func (b *backlinkStore) close() error { return b.db.Close() }

func (b *backlinkStore) meta(key string) (string, error) {
	var v string
	err := b.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("missing meta key %q", key)
	}
	return v, err
}

func (b *backlinkStore) sourceMatches(size, modTime int64) bool {
	sv, err1 := b.meta("src_size")
	mv, err2 := b.meta("src_mod_time")
	if err1 != nil || err2 != nil {
		return false
	}
	return sv == strconv.FormatInt(size, 10) && mv == strconv.FormatInt(modTime, 10)
}

// count returns the total number of pages linking to targetID.
func (b *backlinkStore) count(targetID int) int {
	var n int
	if err := b.db.QueryRow(`SELECT COUNT(*) FROM backlinks WHERE target_id = ?`, targetID).Scan(&n); err != nil {
		return 0
	}
	return n
}

// linksTo returns up to limit pages linking to targetID, ordered by title,
// starting strictly after the `after` title (empty = from the start). This
// is keyset pagination: cheap regardless of how deep into a huge backlink
// list the caller is (some pages have hundreds of thousands of incoming
// links), unlike OFFSET-based paging which gets slower the deeper you go.
func (b *backlinkStore) linksTo(targetID int, after string, limit int) ([]Backlink, error) {
	var rows *sql.Rows
	var err error
	if after == "" {
		rows, err = b.db.Query(
			`SELECT source_title FROM backlinks WHERE target_id = ? ORDER BY source_title LIMIT ?`,
			targetID, limit,
		)
	} else {
		rows, err = b.db.Query(
			`SELECT source_title FROM backlinks WHERE target_id = ? AND source_title > ? ORDER BY source_title LIMIT ?`,
			targetID, after, limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Backlink{}
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return nil, err
		}
		out = append(out, Backlink{Title: title})
	}
	return out, rows.Err()
}

// buildBacklinksAsync builds (or reuses a valid cached) backlinks index for
// this Wiki and publishes it once ready. Meant to run in its own goroutine;
// errors are logged, not returned, since nothing is waiting on this beyond
// BacklinksReady()/LinksTo() gracefully reporting "not ready". If ctx is
// canceled (Wiki.Close, e.g. on shutdown or in a test) before the build
// finishes, it stops promptly rather than running to completion, and the
// partial result is discarded - there's nothing to publish either way.
func (w *Wiki) buildBacklinksAsync(ctx context.Context, storePath, backlinksPath string, artSize, artModTime int64) {
	if bl, err := openBacklinkStore(backlinksPath); err == nil {
		if bl.sourceMatches(artSize, artModTime) {
			slog.Info("backlinks cache loaded", "path", backlinksPath)
			w.backlinks.Store(bl)
			return
		}
		slog.Warn("backlinks cache stale, rebuilding", "path", backlinksPath)
		bl.close()
	} else if !os.IsNotExist(err) {
		slog.Warn("backlinks cache unusable, rebuilding", "path", backlinksPath, "reason", err)
	}

	slog.Info("building backlinks index in the background (this can take a long time on large dumps)", "path", backlinksPath)
	tmpPath := fmt.Sprintf("%s.%d.tmp", backlinksPath, time.Now().UnixNano())
	n, err := buildBacklinkStore(ctx, w.store, storePath, w.articles, w.articlesSize, tmpPath, artSize, artModTime)
	if err != nil {
		os.Remove(tmpPath)
		if errors.Is(err, context.Canceled) {
			slog.Info("backlinks build stopped (shutting down)", "path", backlinksPath)
		} else {
			slog.Error("building backlinks index failed", "error", err)
		}
		return
	}
	if err := os.Rename(tmpPath, backlinksPath); err != nil {
		os.Remove(tmpPath)
		slog.Error("saving backlinks index failed", "error", err)
		return
	}
	bl, err := openBacklinkStore(backlinksPath)
	if err != nil {
		slog.Error("opening freshly-built backlinks index failed", "error", err)
		return
	}
	slog.Info("backlinks index built", "links", n)
	w.backlinks.Store(bl)
}

type linkRow struct {
	targetID    int
	sourceTitle string
}

// buildBacklinkStore scans every page in the dump exactly once (grouped by
// bzip2 stream, so each stream is decompressed once rather than once per
// page), extracts its outgoing [[links]], resolves each target title
// against the already-built title index, and records (target, source)
// pairs. Extraction and title lookups run on a worker per CPU; SQLite only
// allows one writer, so a single goroutine drains the results into it.
//
// mainStore is the Wiki's own already-open title index (reused directly for
// the one-time streams() query, rather than opening yet another connection
// to storePath): storePath is only needed separately to open additional
// connections for concurrent title lookups below.
func buildBacklinkStore(ctx context.Context, mainStore *store, storePath string, articles *os.File, articlesSize int64, dbPath string, artSize, artModTime int64) (int, error) {
	streams, err := mainStore.streams()
	if err != nil {
		return 0, err
	}

	// A dedicated read-only connection pool for concurrent title lookups,
	// separate from the title index's own single serving connection, so
	// this bulk resolution work and normal request traffic never serialize
	// behind each other.
	numWorkers := runtime.NumCPU()
	lookupDB, err := sql.Open("sqlite", storePath)
	if err != nil {
		return 0, err
	}
	defer lookupDB.Close()
	lookupDB.SetMaxOpenConns(numWorkers)

	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, p := range []string{
		"PRAGMA journal_mode = OFF",
		"PRAGMA synchronous = OFF",
		"PRAGMA cache_size = -20000",
	} {
		if err := execPragma(db, p); err != nil {
			return 0, err
		}
	}
	if _, err := db.Exec(`CREATE TABLE backlinks (target_id INTEGER NOT NULL, source_title TEXT NOT NULL)`); err != nil {
		return 0, err
	}
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return 0, err
	}

	type streamRange struct{ start, end int64 }
	jobs := make(chan streamRange, numWorkers*2)
	results := make(chan []linkRow, numWorkers*2)

	var streamsDone atomic.Int64
	totalStreams := int64(len(streams))

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				rows, err := extractStreamBacklinks(articles, job.start, job.end, lookupDB)
				done := streamsDone.Add(1)
				if done%50_000 == 0 || done == totalStreams {
					slog.Info("backlinks build progress", "streams", done, "totalStreams", totalStreams)
				}
				if err != nil {
					slog.Warn("skipping stream during backlinks build", "offset", job.start, "error", err)
					continue
				}
				if len(rows) == 0 {
					continue
				}
				select {
				case results <- rows:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for i, s := range streams {
			end := articlesSize
			if i+1 < len(streams) {
				end = streams[i+1]
			}
			select {
			case jobs <- streamRange{s, end}:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	stmt, err := tx.Prepare(`INSERT INTO backlinks (target_id, source_title) VALUES (?, ?)`)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	n := 0
loop:
	for {
		select {
		case batch, ok := <-results:
			if !ok {
				break loop
			}
			for _, r := range batch {
				if _, err := stmt.Exec(r.targetID, r.sourceTitle); err != nil {
					stmt.Close()
					tx.Rollback()
					return 0, err
				}
				n++
			}
		case <-ctx.Done():
			stmt.Close()
			tx.Rollback()
			// Block until every worker (and the job feeder) has actually
			// stopped - they all select on ctx.Done() too, so this
			// finishes quickly - rather than returning while they might
			// still be running and touching articles/lookupDB after our
			// callers close those out from under them.
			for range results {
			}
			return 0, ctx.Err()
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	slog.Info("building backlinks B-tree", "links", n)
	if _, err := db.Exec(`CREATE INDEX idx_backlinks_target ON backlinks(target_id, source_title)`); err != nil {
		return 0, err
	}

	if err := writeMeta(db, map[string]string{
		"schema_version": backlinksSchemaVersion,
		"src_size":       strconv.FormatInt(artSize, 10),
		"src_mod_time":   strconv.FormatInt(artModTime, 10),
		"links":          strconv.Itoa(n),
	}); err != nil {
		return 0, err
	}

	if err := execPragma(db, "PRAGMA journal_mode = DELETE"); err != nil {
		slog.Warn("could not reset journal mode after backlinks build", "error", err)
	}

	return n, nil
}

// extractStreamBacklinks decompresses one bzip2 stream, decodes every page
// in it, and resolves each page's distinct outgoing link targets against
// lookupDB. Mirrors readPage's decode loop but collects every page in the
// stream instead of stopping at one matching id.
func extractStreamBacklinks(articles *os.File, start, end int64, lookupDB *sql.DB) ([]linkRow, error) {
	section := io.NewSectionReader(articles, start, end-start)
	dec := xml.NewDecoder(bzip2.NewReader(section))

	var rows []linkRow
	for tries := 0; tries < maxPagesPerStream; tries++ {
		var p page
		if err := dec.Decode(&p); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return rows, fmt.Errorf("decoding stream at offset %d: %w", start, err)
		}
		targets := extractLinkTargets(p.Text)
		if len(targets) == 0 {
			continue
		}
		seen := make(map[string]struct{}, len(targets))
		for _, t := range targets {
			if _, dup := seen[t]; dup {
				continue
			}
			seen[t] = struct{}{}
			if id, ok := lookupTitle(lookupDB, t); ok {
				rows = append(rows, linkRow{targetID: id, sourceTitle: p.Title})
			}
		}
	}
	return rows, nil
}

func lookupTitle(db *sql.DB, title string) (int, bool) {
	var id int
	if err := db.QueryRow(`SELECT id FROM pages WHERE title = ? ORDER BY id DESC LIMIT 1`, title).Scan(&id); err == nil {
		return id, true
	}
	// MediaWiki always capitalizes the first letter of the actual page
	// title, so an as-written lowercase-first link (very common in casual
	// wikitext) should still resolve. Other case variants (titleVariants)
	// are deliberately not tried here: this query runs per distinct link
	// target across the whole dump, so keeping it to at most two lookups
	// matters for how long the (already long) background build takes.
	alt := upperFirst(title)
	if alt == title {
		return 0, false
	}
	if err := db.QueryRow(`SELECT id FROM pages WHERE title = ? ORDER BY id DESC LIMIT 1`, alt).Scan(&id); err == nil {
		return id, true
	}
	return 0, false
}

var (
	commentTagRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	linkTargetRe = regexp.MustCompile(`\[\[([^\]|#]+)`)
)

// extractLinkTargets pulls every [[target]] / [[target|label]] /
// [[target#section]] out of wikitext, normalized to the form a title
// lookup expects. It doesn't distinguish File:/Category:/Template:/etc.
// links from plain ones - all count as "this page links to that page".
func extractLinkTargets(wikitext string) []string {
	text := commentTagRe.ReplaceAllString(wikitext, "")
	matches := linkTargetRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	targets := make([]string, 0, len(matches))
	for _, m := range matches {
		raw := strings.TrimPrefix(strings.TrimSpace(m[1]), ":")
		t := normalizeTitle(raw)
		if t == "" {
			continue
		}
		targets = append(targets, t)
	}
	return targets
}
