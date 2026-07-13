package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fabriceboyer/wikipedia_server/wikipedia"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	w, err := wikipedia.Open(
		filepath.Join("wikipedia", "testdata", "fixture-index.txt.bz2"),
		filepath.Join("wikipedia", "testdata", "fixture-articles.xml.bz2"),
		&wikipedia.Options{CacheDir: t.TempDir()},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })

	srv := newServer([]*source{{
		Name:        "wiki",
		Description: "Test wiki",
		Pages:       w.Pages(),
		wiki:        w,
	}}, t.TempDir() /* no web UI built */)
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)
	return ts
}

func getJSON(t *testing.T, url string, wantStatus int, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s: status %d, want %d", url, resp.StatusCode, wantStatus)
	}
	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAPIPage(t *testing.T) {
	ts := testServer(t)

	var page struct {
		Source string `json:"source"`
		Title  string `json:"title"`
		ID     int    `json:"id"`
		Text   string `json:"text"`
	}
	getJSON(t, ts.URL+"/api/wiki/page/Anarchism", http.StatusOK, &page)
	if page.Source != "wiki" || page.Title != "Anarchism" || page.ID != 12 || page.Text == "" {
		t.Errorf("unexpected page: %+v", page)
	}

	// Redirect follow + chain reporting.
	var redir struct {
		Title          string   `json:"title"`
		RedirectedFrom []string `json:"redirectedFrom"`
	}
	getJSON(t, ts.URL+"/api/wiki/page/Einstein", http.StatusOK, &redir)
	if redir.Title != "Albert Einstein" || len(redir.RedirectedFrom) != 1 {
		t.Errorf("redirect not followed: %+v", redir)
	}

	getJSON(t, ts.URL+"/api/wiki/page/Nope", http.StatusNotFound, nil)
	getJSON(t, ts.URL+"/api/nope/page/Anarchism", http.StatusNotFound, nil)
}

func TestAPIPageRaw(t *testing.T) {
	ts := testServer(t)
	resp, err := http.Get(ts.URL + "/api/wiki/page/Autism?raw=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
}

func TestAPISearch(t *testing.T) {
	ts := testServer(t)

	var res struct {
		Results []struct {
			Title string `json:"title"`
			ID    int    `json:"id"`
		} `json:"results"`
	}
	getJSON(t, ts.URL+"/api/wiki/search?q=a", http.StatusOK, &res)
	if len(res.Results) == 0 {
		t.Error("no results for prefix 'a'")
	}

	getJSON(t, ts.URL+"/api/wiki/search", http.StatusBadRequest, nil)
	getJSON(t, ts.URL+"/api/wiki/search?q=a&limit=0", http.StatusBadRequest, nil)
}

func TestAPIStatusAndSources(t *testing.T) {
	ts := testServer(t)

	var st struct {
		Status  string `json:"status"`
		Sources []struct {
			Name  string `json:"name"`
			Pages int    `json:"pages"`
		} `json:"sources"`
	}
	getJSON(t, ts.URL+"/api/status", http.StatusOK, &st)
	if st.Status != "ok" || len(st.Sources) != 1 || st.Sources[0].Pages != 6 {
		t.Errorf("unexpected status: %+v", st)
	}
}

func TestLegacyEndpoints(t *testing.T) {
	ts := testServer(t)

	resp, err := http.Get(ts.URL + "/wiki?page=Anarchism")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/wiki?page= status %d", resp.StatusCode)
	}

	var titles []string
	getJSON(t, ts.URL+"/search?name=alb", http.StatusOK, &titles)
	if len(titles) != 1 || titles[0] != "Albert Einstein" {
		t.Errorf("legacy search = %v", titles)
	}
}
