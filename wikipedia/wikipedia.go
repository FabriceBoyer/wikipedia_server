// Package wikipedia serves articles straight out of Wikipedia multistream
// dump files (pages-articles-multistream.xml.bz2 + its .txt.bz2 index),
// without a database.
package wikipedia

import (
	"compress/bzip2"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

// ErrNotFound is returned when no page matches the requested title.
var ErrNotFound = errors.New("article not found")

const maxRedirects = 5

// maxPagesPerStream is a safety net against corrupt input, not a real
// limit: the multistream format nominally packs ~100 pages per bzip2
// stream, but real dumps occasionally pack many more (e.g. runs of short
// redirect stubs), so the actual bound is simply the stream's own EOF -
// io.NewSectionReader in readPage already stops decoding at the next
// stream's offset. An earlier, much lower cap here silently dropped
// legitimate pages living past it in oversized streams.
const maxPagesPerStream = 1_000_000

type Options struct {
	// Limit caps the number of index entries loaded (tests/benchmarks).
	// When set, a fresh cache is always (re)built rather than reusing a
	// previously persisted one.
	Limit int
	// CacheDir overrides where the index cache database is written.
	// Empty means alongside the source index file.
	CacheDir string
}

// Wiki reads one dump (index + articles file pair). After Open returns, the
// store is immutable, so all methods are safe for concurrent use.
type Wiki struct {
	store        *store
	articles     *os.File
	articlesSize int64

	// backlinks is published once the background "what links here" index
	// (see backlinks.go) finishes building; nil until then. A Pointer gives
	// lock-free reads for the common case (already built, or not yet).
	backlinks atomic.Pointer[backlinkStore]
	bgCancel  context.CancelFunc
	bgWG      sync.WaitGroup
}

// Open loads the dump index, preferring a previously built on-disk cache
// and otherwise parsing the source bz2 index into a fresh one.
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

	storePath := storeCachePath(indexPath, opts.CacheDir)
	st, err := openOrBuildStore(indexPath, opts, srcStat.Size(), srcStat.ModTime().Unix())
	if err != nil {
		articles.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	w := &Wiki{
		store:        st,
		articles:     articles,
		articlesSize: artStat.Size(),
		bgCancel:     cancel,
	}

	// Building the reverse "what links here" index requires scanning every
	// page's full text (not just the small title index), which can take a
	// very long time on a large dump - so it never blocks startup. Pages
	// simply report backlinks as "not ready" until this finishes. Close()
	// cancels ctx and waits for this goroutine (and everything it spawns)
	// to actually stop before closing the files it reads from.
	backlinksPath := backlinksCachePath(indexPath, opts.CacheDir)
	w.bgWG.Add(1)
	go func() {
		defer w.bgWG.Done()
		w.buildBacklinksAsync(ctx, storePath, backlinksPath, artStat.Size(), artStat.ModTime().Unix())
	}()

	return w, nil
}

// openOrBuildStore returns a ready-to-query store, preferring a valid
// on-disk cache and otherwise parsing the source index into a new one,
// atomically installed so a crash mid-build never corrupts a good cache.
func openOrBuildStore(indexPath string, opts *Options, srcSize, srcMod int64) (*store, error) {
	cachePath := storeCachePath(indexPath, opts.CacheDir)

	if opts.Limit <= 0 {
		if s, err := openStore(cachePath); err == nil {
			if s.sourceMatches(srcSize, srcMod) {
				slog.Info("index cache loaded", "path", cachePath, "pages", s.pages())
				return s, nil
			}
			slog.Warn("index cache stale, rebuilding", "path", cachePath)
			s.close()
		} else if !os.IsNotExist(err) {
			slog.Warn("index cache unusable, rebuilding", "path", cachePath, "reason", err)
		}
	}

	start := time.Now()
	tmpPath := cachePath + ".tmp"
	n, err := buildFileStore(indexPath, tmpPath, opts.Limit, srcSize, srcMod)
	if err != nil {
		os.Remove(tmpPath)
		return nil, err
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("saving index cache: %w", err)
	}
	slog.Info("index built", "file", indexPath, "pages", n, "elapsed", time.Since(start))

	return openStore(cachePath)
}

func storeCachePath(indexPath, cacheDir string) string {
	base := strings.TrimSuffix(filepath.Base(indexPath), ".txt.bz2") + ".sqlite"
	if cacheDir == "" {
		cacheDir = filepath.Dir(indexPath)
	}
	return filepath.Join(cacheDir, base)
}

// Close stops the background backlinks build (if still running) and waits
// for it to actually exit before closing the underlying files, so no
// goroutine is left reading from them afterward.
func (w *Wiki) Close() error {
	w.bgCancel()
	w.bgWG.Wait()

	err := w.store.close()
	if bl := w.backlinks.Load(); bl != nil {
		if cerr := bl.close(); err == nil {
			err = cerr
		}
	}
	if cerr := w.articles.Close(); err == nil {
		err = cerr
	}
	return err
}

// Pages returns the number of indexed pages.
func (w *Wiki) Pages() int { return w.store.pages() }

// BacklinksReady reports whether the "what links here" index has finished
// its background build for this source.
func (w *Wiki) BacklinksReady() bool { return w.backlinks.Load() != nil }

// LinksTo returns pages that link to title, ordered by title and paginated
// via a keyset cursor: pass the empty string for the first page, then the
// previous call's last result's title for subsequent pages. hasMore
// indicates whether another page follows. Returns ErrBacklinksNotReady
// while the background index build is still in progress.
func (w *Wiki) LinksTo(title, after string, limit int) (results []Backlink, total int, hasMore bool, err error) {
	bl := w.backlinks.Load()
	if bl == nil {
		return nil, 0, false, ErrBacklinksNotReady
	}
	name := normalizeTitle(title)
	_, id, ok := w.lookup(name)
	if !ok {
		return nil, 0, false, fmt.Errorf("%w: %q", ErrNotFound, title)
	}
	results, err = bl.linksTo(id, after, limit+1)
	if err != nil {
		return nil, 0, false, err
	}
	if len(results) > limit {
		results = results[:limit]
		hasMore = true
	}
	return results, bl.count(id), hasMore, nil
}

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
		if len(results) >= limit {
			break
		}
		matches, err := w.store.searchPrefix(prefix, limit)
		if err != nil {
			slog.Error("search failed", "prefix", prefix, "error", err)
			continue
		}
		for _, r := range matches {
			if _, dup := seen[r.Title]; dup {
				continue
			}
			seen[r.Title] = struct{}{}
			results = append(results, r)
		}
	}
	// Shorter titles first: better autocompletion ranking for prefixes.
	sort.Slice(results, func(i, j int) bool {
		if len(results[i].Title) != len(results[j].Title) {
			return len(results[i].Title) < len(results[j].Title)
		}
		return results[i].Title < results[j].Title
	})
	if len(results) > limit {
		results = results[:limit]
	}
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
	seek, id, ok := w.lookup(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, title)
	}
	p, err := w.readPage(seek, id)
	if err != nil {
		return nil, err
	}
	if target := p.Redirect.Title; target == "" || !follow {
		return pageToArticle(p, nil), nil
	}
	return w.followRedirects(normalizeTitle(p.Redirect.Title), []string{p.Title}, map[string]struct{}{p.Title: {}})
}

// RandomArticle picks a page uniformly at random from the index. When
// follow is true and the pick happens to be a redirect, it is resolved the
// same way GetArticle resolves one.
func (w *Wiki) RandomArticle(follow bool) (*Article, error) {
	seek, id, _, ok := w.store.random()
	if !ok {
		return nil, fmt.Errorf("no pages available")
	}
	p, err := w.readPage(seek, id)
	if err != nil {
		return nil, err
	}
	if target := p.Redirect.Title; target != "" && follow {
		if a, err := w.followRedirects(normalizeTitle(target), []string{p.Title}, map[string]struct{}{p.Title: {}}); err == nil {
			return a, nil
		}
		// Landed on a dangling or cyclic redirect purely by chance: surface
		// the redirect stub itself rather than failing the whole request.
	}
	return pageToArticle(p, nil), nil
}

// followRedirects resolves a chain of redirects starting at name (bounded,
// cycle-safe), returning the final non-redirect page as an Article whose
// RedirectedFrom records every hop already taken (chain) plus any further
// ones needed to reach it.
func (w *Wiki) followRedirects(name string, chain []string, seen map[string]struct{}) (*Article, error) {
	for hop := len(chain); hop <= maxRedirects; hop++ {
		seek, id, ok := w.lookup(name)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrNotFound, name)
		}
		p, err := w.readPage(seek, id)
		if err != nil {
			return nil, err
		}
		if target := p.Redirect.Title; target != "" {
			if _, cycle := seen[p.Title]; !cycle {
				seen[p.Title] = struct{}{}
				chain = append(chain, p.Title)
				name = normalizeTitle(target)
				continue
			}
		}
		return pageToArticle(p, chain), nil
	}
	return nil, fmt.Errorf("too many redirects resolving %q", name)
}

func pageToArticle(p *page, chain []string) *Article {
	return &Article{
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
}

// lookup tries the title as given plus common capitalization variants.
func (w *Wiki) lookup(name string) (seek int64, id int, ok bool) {
	for _, cand := range w.titleVariants(name) {
		if seek, id, ok := w.store.find(cand); ok {
			return seek, id, true
		}
	}
	return 0, 0, false
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
// NFC normalization means visually-identical titles typed via a different
// keyboard/IME (which can produce a decomposed accent form) still match the
// precomposed form Wikipedia's dump titles are stored in.
func normalizeTitle(s string) string {
	return norm.NFC.String(strings.TrimSpace(strings.ReplaceAll(s, "_", " ")))
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
func (w *Wiki) readPage(seek int64, id int) (*page, error) {
	end := w.store.streamEnd(seek, w.articlesSize)

	section := io.NewSectionReader(w.articles, seek, end-seek)
	dec := xml.NewDecoder(bzip2.NewReader(section))
	for tries := 0; tries < maxPagesPerStream; tries++ {
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
