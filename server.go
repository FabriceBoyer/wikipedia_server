package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/fabriceboyer/wikipedia_server/wikipedia"
)

const defaultSearchLimit = 20
const maxSearchLimit = 100

type source struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Pages       int    `json:"pages"`

	wiki *wikipedia.Wiki
}

type server struct {
	sources map[string]*source
	order   []string // stable ordering for listings
	webDir  string
	started time.Time
}

func newServer(sources []*source, webDir string) *server {
	s := &server{
		sources: map[string]*source{},
		webDir:  webDir,
		started: time.Now(),
	}
	for _, src := range sources {
		s.sources[src.Name] = src
		s.order = append(s.order, src.Name)
	}
	return s
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/sources", s.handleSources)
	mux.HandleFunc("GET /api/{source}/search", s.handleSearch)
	mux.HandleFunc("GET /api/{source}/page/{title...}", s.handlePage)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Legacy endpoints, kept for compatibility with pre-2.0 clients.
	mux.HandleFunc("GET /wiki", s.legacyPage("wiki"))
	mux.HandleFunc("GET /dict", s.legacyPage("dict"))
	mux.HandleFunc("GET /search", s.legacySearch)

	mux.Handle("GET /", s.staticHandler())

	return logRequests(recoverPanics(cors(mux)))
}

// --- API handlers ---

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	type status struct {
		Status  string    `json:"status"`
		Uptime  string    `json:"uptime"`
		Sources []*source `json:"sources"`
	}
	writeJSON(w, http.StatusOK, status{
		Status:  "ok",
		Uptime:  time.Since(s.started).Round(time.Second).String(),
		Sources: s.sourceList(),
	})
}

func (s *server) handleSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.sourceList())
}

func (s *server) sourceList() []*source {
	list := make([]*source, 0, len(s.order))
	for _, name := range s.order {
		list = append(list, s.sources[name])
	}
	return list
}

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	src, ok := s.sources[r.PathValue("source")]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_source", "unknown source: "+r.PathValue("source"))
		return
	}
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "missing_query", "query parameter 'q' is required")
		return
	}
	limit := defaultSearchLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "bad_limit", "'limit' must be a positive integer")
			return
		}
		limit = min(n, maxSearchLimit)
	}

	results := src.wiki.Search(query, limit)
	writeJSON(w, http.StatusOK, map[string]any{
		"source":  src.Name,
		"query":   query,
		"results": results,
	})
}

func (s *server) handlePage(w http.ResponseWriter, r *http.Request) {
	src, ok := s.sources[r.PathValue("source")]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_source", "unknown source: "+r.PathValue("source"))
		return
	}
	title := r.PathValue("title")
	if title == "" {
		writeError(w, http.StatusBadRequest, "missing_title", "page title is required")
		return
	}
	follow := r.URL.Query().Get("follow") != "0" && r.URL.Query().Get("follow") != "false"

	article, err := src.wiki.GetArticle(title, follow)
	if err != nil {
		if errors.Is(err, wikipedia.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
		} else {
			slog.Error("failed to read article", "source", src.Name, "title", title, "error", err)
			writeError(w, http.StatusInternalServerError, "read_error", "failed to read article")
		}
		return
	}

	if r.URL.Query().Get("raw") == "1" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(article.Text))
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Source string `json:"source"`
		*wikipedia.Article
	}{src.Name, article})
}

// --- legacy handlers ---

func (s *server) legacyPage(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		src, ok := s.sources[name]
		if !ok {
			writeError(w, http.StatusNotFound, "unknown_source", "source not loaded: "+name)
			return
		}
		article, err := src.wiki.GetArticle(r.URL.Query().Get("page"), true)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, wikipedia.ErrNotFound) {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(article.Text))
	}
}

func (s *server) legacySearch(w http.ResponseWriter, r *http.Request) {
	src, ok := s.sources["wiki"]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_source", "source not loaded: wiki")
		return
	}
	results := src.wiki.Search(r.URL.Query().Get("name"), defaultSearchLimit)
	titles := make([]string, 0, len(results))
	for _, res := range results {
		titles = append(titles, res.Title)
	}
	writeJSON(w, http.StatusOK, titles)
}

// --- static UI ---

func (s *server) staticHandler() http.Handler {
	if _, err := os.Stat(filepath.Join(s.webDir, "index.html")); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte("wikipedia_server is running. Web UI not built (see web/README). API lives under /api/.\n"))
		})
	}
	fs := http.FileServer(http.Dir(s.webDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Single-page app: unknown paths fall back to index.html.
		if r.URL.Path != "/" {
			if _, err := os.Stat(filepath.Join(s.webDir, filepath.Clean(r.URL.Path))); err != nil {
				http.ServeFile(w, r, filepath.Join(s.webDir, "index.html"))
				return
			}
		}
		fs.ServeHTTP(w, r)
	})
}

// --- helpers & middleware ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "elapsed", time.Since(start).Round(time.Microsecond))
	})
}

func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic in handler", "path", r.URL.Path, "panic", rec)
				writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
