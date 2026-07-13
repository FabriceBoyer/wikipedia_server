// Package wikipedia serves articles straight out of Wikipedia multistream
// dump files (pages-articles-multistream.xml.bz2 + its .txt.bz2 index),
// without a database.
package wikipedia

import (
	"bytes"
	"compress/bzip2"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// ErrNotFound is returned when no page matches the requested title.
var ErrNotFound = errors.New("article not found")

const maxRedirects = 5

// decodesPerStream caps how many pages we decode inside one bzip2 stream
// while looking for the target id; dumps pack 100 pages per stream.
const decodesPerStream = 1000

type Options struct {
	// Limit caps the number of index entries loaded (tests/benchmarks).
	// When set, the on-disk index cache is bypassed.
	Limit int
	// CacheDir overrides where the binary index cache is written.
	// Empty means alongside the source index file.
	CacheDir string
}

// Wiki reads one dump (index + articles file pair). After Open returns, the
// index is immutable, so all methods are safe for concurrent use.
type Wiki struct {
	idx          *index
	articles     *os.File
	articlesSize int64
}

// Open loads the dump index, preferring a previously saved binary cache
// (mmapped, near-instant) and otherwise parsing the bz2 index and saving the
// cache best-effort for next time.
func Open(indexPath, articlesPath string, opts *Options) (*Wiki, error) {
	if opts == nil {
		opts = &Options{}
	}

	srcStat, err := os.Stat(indexPath)
	if err != nil {
		return nil, fmt.Errorf("index file: %w", err)
	}
	articles, err := os.Open(articlesPath)
	if err != nil {
		return nil, fmt.Errorf("articles file: %w", err)
	}
	artStat, err := articles.Stat()
	if err != nil {
		articles.Close()
		return nil, err
	}

	srcSize, srcMod := srcStat.Size(), srcStat.ModTime().Unix()
	cachePath := indexCachePath(indexPath, opts.CacheDir)

	var ix *index
	if opts.Limit <= 0 {
		start := time.Now()
		ix, err = loadIndexFile(cachePath, srcSize, srcMod)
		if err == nil {
			slog.Info("index cache loaded", "path", cachePath, "pages", ix.n, "elapsed", time.Since(start))
		} else if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("index cache unusable, rebuilding", "path", cachePath, "reason", err)
		}
	}
	if ix == nil {
		start := time.Now()
		ix, err = buildIndex(indexPath, opts.Limit, srcSize, srcMod)
		if err != nil {
			articles.Close()
			return nil, err
		}
		slog.Info("index parsed", "file", indexPath, "pages", ix.n, "elapsed", time.Since(start))
		if opts.Limit <= 0 {
			if err := saveIndexFile(ix, cachePath); err != nil {
				slog.Warn("could not save index cache (startup will re-parse next time)", "path", cachePath, "error", err)
			} else if reloaded, err := loadIndexFile(cachePath, srcSize, srcMod); err == nil {
				// Swap the heap-allocated index for the mmapped file so the
				// parsed copy can be reclaimed and resident memory drops.
				ix = reloaded
				slog.Info("index cache saved", "path", cachePath)
			}
		}
	}

	return &Wiki{
		idx:          ix,
		articles:     articles,
		articlesSize: artStat.Size(),
	}, nil
}

func indexCachePath(indexPath, cacheDir string) string {
	base := strings.TrimSuffix(filepath.Base(indexPath), ".txt.bz2") + ".idx"
	if cacheDir == "" {
		cacheDir = filepath.Dir(indexPath)
	}
	return filepath.Join(cacheDir, base)
}

func (w *Wiki) Close() error {
	err := w.idx.close()
	if cerr := w.articles.Close(); err == nil {
		err = cerr
	}
	return err
}

// Pages returns the number of indexed pages.
func (w *Wiki) Pages() int { return w.idx.n }

type SearchResult struct {
	Title string `json:"title"`
	ID    int    `json:"id"`
}

// Search returns up to limit titles starting with the given prefix.
// Case variants of the prefix (as typed, First-upper, Title Case, lower)
// are all tried, since dump titles are matched byte-exactly.
func (w *Wiki) Search(query string, limit int) []SearchResult {
	query = normalizeTitle(query)
	if query == "" || limit <= 0 {
		return []SearchResult{}
	}

	seen := map[string]struct{}{}
	results := []SearchResult{}
	for _, prefix := range w.titleVariants(query) {
		p := []byte(prefix)
		for i := w.idx.lowerBound(p); i < w.idx.n && len(results) < limit; i++ {
			t := w.idx.title(i)
			if !bytes.HasPrefix(t, p) {
				break
			}
			title := string(t)
			if _, dup := seen[title]; dup {
				continue
			}
			seen[title] = struct{}{}
			results = append(results, SearchResult{Title: title, ID: w.idx.id(i)})
		}
		if len(results) >= limit {
			break
		}
	}
	// Shorter titles first: better autocompletion ranking for prefixes.
	sort.Slice(results, func(i, j int) bool {
		if len(results[i].Title) != len(results[j].Title) {
			return len(results[i].Title) < len(results[j].Title)
		}
		return results[i].Title < results[j].Title
	})
	return results
}

// Article is the page representation exposed through the API.
type Article struct {
	Title          string   `json:"title"`
	ID             int      `json:"id"`
	NS             int      `json:"ns"`
	RevisionID     string   `json:"revisionId,omitempty"`
	Timestamp      string   `json:"timestamp,omitempty"`
	Model          string   `json:"model,omitempty"`
	Format         string   `json:"format,omitempty"`
	RedirectTo     string   `json:"redirectTo,omitempty"`
	RedirectedFrom []string `json:"redirectedFrom,omitempty"`
	Text           string   `json:"text"`
}

// GetArticle fetches a page by title. When follow is true, redirect pages
// are resolved transparently (bounded, cycle-safe) and the traversed chain
// is reported in RedirectedFrom.
func (w *Wiki) GetArticle(title string, follow bool) (*Article, error) {
	name := normalizeTitle(title)
	var chain []string
	seen := map[string]struct{}{}
	for hop := 0; hop <= maxRedirects; hop++ {
		i, ok := w.lookup(name)
		if !ok {
			if len(chain) > 0 {
				return nil, fmt.Errorf("%w: %q (redirect target of %q)", ErrNotFound, name, title)
			}
			return nil, fmt.Errorf("%w: %q", ErrNotFound, title)
		}
		p, err := w.readPage(i)
		if err != nil {
			return nil, err
		}
		if target := p.Redirect.Title; target != "" && follow {
			if _, cycle := seen[p.Title]; !cycle {
				seen[p.Title] = struct{}{}
				chain = append(chain, p.Title)
				name = normalizeTitle(target)
				continue
			}
		}
		a := &Article{
			Title:          p.Title,
			ID:             p.ID,
			NS:             p.NS,
			RevisionID:     p.RevisionID,
			Timestamp:      p.Timestamp,
			Model:          p.Model,
			Format:         p.Format,
			RedirectTo:     p.Redirect.Title,
			RedirectedFrom: chain,
			Text:           p.Text,
		}
		return a, nil
	}
	return nil, fmt.Errorf("too many redirects resolving %q", title)
}

// lookup tries the title as given plus common capitalization variants.
func (w *Wiki) lookup(name string) (int, bool) {
	for _, cand := range w.titleVariants(name) {
		if i, ok := w.idx.find([]byte(cand)); ok {
			return i, true
		}
	}
	return 0, false
}

func (w *Wiki) titleVariants(name string) []string {
	variants := []string{name}
	add := func(v string) {
		for _, existing := range variants {
			if v == existing {
				return
			}
		}
		variants = append(variants, v)
	}
	add(upperFirst(name))
	// cases.Caser carries internal state and is not safe for concurrent
	// use, so build one per call instead of sharing it on the Wiki.
	add(cases.Title(language.AmericanEnglish).String(strings.ToLower(name)))
	add(strings.ToLower(name))
	return variants
}

func upperFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError || unicode.IsUpper(r) {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// normalizeTitle applies Wikipedia title conventions to user input.
func normalizeTitle(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "_", " "))
}

type redirect struct {
	Title string `xml:"title,attr"`
}

type page struct {
	XMLName    xml.Name `xml:"page"`
	Title      string   `xml:"title"`
	NS         int      `xml:"ns"`
	ID         int      `xml:"id"`
	Redirect   redirect `xml:"redirect"`
	RevisionID string   `xml:"revision>id"`
	Timestamp  string   `xml:"revision>timestamp"`
	Model      string   `xml:"revision>model"`
	Format     string   `xml:"revision>format"`
	Text       string   `xml:"revision>text"`
}

// readPage decompresses only the bzip2 stream containing the page (the
// multistream format packs ~100 pages per stream) and scans it for the id.
// io.NewSectionReader keeps this safe under concurrency: no shared seeking.
func (w *Wiki) readPage(i int) (*page, error) {
	seek := w.idx.seek(i)
	id := w.idx.id(i)
	end := w.idx.streamEnd(seek, w.articlesSize)

	section := io.NewSectionReader(w.articles, seek, end-seek)
	dec := xml.NewDecoder(bzip2.NewReader(section))
	for tries := 0; tries < decodesPerStream; tries++ {
		var p page
		if err := dec.Decode(&p); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decoding stream at offset %d: %w", seek, err)
		}
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("page id %d not found in stream at offset %d", id, seek)
}
