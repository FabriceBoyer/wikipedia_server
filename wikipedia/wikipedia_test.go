package wikipedia

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func openFixture(t *testing.T) *Wiki {
	t.Helper()
	w, err := Open(
		filepath.Join("testdata", "fixture-index.txt.bz2"),
		filepath.Join("testdata", "fixture-articles.xml.bz2"),
		&Options{CacheDir: t.TempDir()},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

func TestGetArticle(t *testing.T) {
	w := openFixture(t)

	if got := w.Pages(); got != 6 {
		t.Errorf("Pages() = %d, want 6", got)
	}

	tests := []struct {
		name      string
		query     string
		wantTitle string
		wantID    int
	}{
		{"exact", "Anarchism", "Anarchism", 12},
		{"case-insensitive", "autism", "Autism", 25},
		{"underscores", "Albert_Einstein", "Albert Einstein", 736},
		{"multi-word lowercase", "albert einstein", "Albert Einstein", 736},
		{"colon in title", "Category:Test", "Category:Test", 42},
		{"unicode", "Ω", "Ω", 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := w.GetArticle(tt.query, true)
			if err != nil {
				t.Fatal(err)
			}
			if a.Title != tt.wantTitle || a.ID != tt.wantID {
				t.Errorf("got (%q, %d), want (%q, %d)", a.Title, a.ID, tt.wantTitle, tt.wantID)
			}
			if a.Text == "" {
				t.Error("empty article text")
			}
		})
	}
}

func TestGetArticleNotFound(t *testing.T) {
	w := openFixture(t)
	_, err := w.GetArticle("Nonexistent Page", true)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestRedirects(t *testing.T) {
	w := openFixture(t)

	a, err := w.GetArticle("Einstein", true)
	if err != nil {
		t.Fatal(err)
	}
	if a.Title != "Albert Einstein" || a.ID != 736 {
		t.Errorf("redirect not followed: got (%q, %d)", a.Title, a.ID)
	}
	if len(a.RedirectedFrom) != 1 || a.RedirectedFrom[0] != "Einstein" {
		t.Errorf("RedirectedFrom = %v, want [Einstein]", a.RedirectedFrom)
	}

	a, err = w.GetArticle("Einstein", false)
	if err != nil {
		t.Fatal(err)
	}
	if a.Title != "Einstein" || a.RedirectTo != "Albert Einstein" {
		t.Errorf("follow=false: got title %q redirectTo %q", a.Title, a.RedirectTo)
	}
}

func TestSearch(t *testing.T) {
	w := openFixture(t)

	results := w.Search("A", 10)
	if len(results) != 3 { // Anarchism, Autism, Albert Einstein
		var titles []string
		for _, r := range results {
			titles = append(titles, r.Title)
		}
		t.Errorf("Search(A) = %v, want 3 results", titles)
	}

	results = w.Search("albert", 10)
	if len(results) != 1 || results[0].Title != "Albert Einstein" {
		t.Errorf("Search(albert) = %v, want [Albert Einstein]", results)
	}

	if got := w.Search("zzz", 10); len(got) != 0 {
		t.Errorf("Search(zzz) = %v, want empty", got)
	}
	if got := w.Search("", 10); len(got) != 0 {
		t.Errorf("Search('') = %v, want empty", got)
	}
	if got := w.Search("A", 2); len(got) != 2 {
		t.Errorf("Search(A, limit=2) returned %d results", len(got))
	}
}

func TestIndexCacheRoundtrip(t *testing.T) {
	cacheDir := t.TempDir()
	indexPath := filepath.Join("testdata", "fixture-index.txt.bz2")
	articlesPath := filepath.Join("testdata", "fixture-articles.xml.bz2")

	w1, err := Open(indexPath, articlesPath, &Options{CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	w1.Close()

	cachePath := filepath.Join(cacheDir, "fixture-index.idx")
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}

	// Second open must come from the cache and behave identically.
	w2, err := Open(indexPath, articlesPath, &Options{CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()

	a, err := w2.GetArticle("Anarchism", true)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != 12 {
		t.Errorf("cached lookup: ID = %d, want 12", a.ID)
	}

	// A corrupted cache must be rebuilt, not crash.
	if err := os.WriteFile(cachePath, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	w3, err := Open(indexPath, articlesPath, &Options{CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("open with corrupted cache: %v", err)
	}
	defer w3.Close()
	if _, err := w3.GetArticle("Autism", true); err != nil {
		t.Errorf("lookup after cache rebuild: %v", err)
	}
}

func TestConcurrentReads(t *testing.T) {
	w := openFixture(t)
	titles := []string{"Anarchism", "Autism", "Albert Einstein", "Einstein", "Ω"}
	done := make(chan error, 50)
	for i := 0; i < 50; i++ {
		go func(i int) {
			_, err := w.GetArticle(titles[i%len(titles)], true)
			done <- err
		}(i)
	}
	for i := 0; i < 50; i++ {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}

// TestRealDump exercises a real dump when DUMP_PATH is configured; CI and
// machines without the ~20GB dumps skip it.
func TestRealDump(t *testing.T) {
	dumpPath := os.Getenv("DUMP_PATH")
	if dumpPath == "" {
		t.Skip("DUMP_PATH not set")
	}
	indexPath := filepath.Join(dumpPath, "enwiki-pages-articles-multistream-index.txt.bz2")
	if _, err := os.Stat(indexPath); err != nil {
		t.Skipf("dump not present: %v", err)
	}

	w, err := Open(indexPath, filepath.Join(dumpPath, "enwiki-pages-articles-multistream.xml.bz2"), &Options{Limit: 10000, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	a, err := w.GetArticle("Anarchism", true)
	if err != nil {
		t.Fatal(err)
	}
	if a.Text == "" {
		t.Error("empty text for Anarchism")
	}
}

func BenchmarkGetArticle(b *testing.B) {
	w, err := Open(
		filepath.Join("testdata", "fixture-index.txt.bz2"),
		filepath.Join("testdata", "fixture-articles.xml.bz2"),
		&Options{CacheDir: b.TempDir()},
	)
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.GetArticle("Albert Einstein", true); err != nil {
			b.Fatal(err)
		}
	}
}
