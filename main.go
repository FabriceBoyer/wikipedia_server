package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fabriceboyer/wikipedia_server/wikipedia"
)

var sourceDefs = []struct {
	name, description, indexFile, articlesFile string
}{
	{"wiki", "Wikipedia (English)", "enwiki-pages-articles-multistream-index.txt.bz2", "enwiki-pages-articles-multistream.xml.bz2"},
	{"dict", "Wiktionary (English)", "enwiktionary-pages-articles-multistream-index.txt.bz2", "enwiktionary-pages-articles-multistream.xml.bz2"},
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}

	// Sources load sequentially, on purpose: a first-run index build has a
	// significant transient memory peak and doing two at once would double it.
	var sources []*source
	for _, def := range sourceDefs {
		indexPath := filepath.Join(cfg.DumpPath, def.indexFile)
		articlesPath := filepath.Join(cfg.DumpPath, def.articlesFile)
		w, err := wikipedia.Open(indexPath, articlesPath, nil)
		if err != nil {
			slog.Warn("skipping source", "source", def.name, "error", err)
			continue
		}
		defer w.Close()
		sources = append(sources, &source{
			Name:        def.name,
			Description: def.description,
			Pages:       w.Pages(),
			wiki:        w,
		})
		slog.Info("source ready", "source", def.name, "pages", w.Pages())
	}
	if len(sources) == 0 {
		slog.Error("no sources could be loaded; check DUMP_PATH and run download_data.sh", "dumpPath", cfg.DumpPath)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           newServer(sources, cfg.WebDir).handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	slog.Info("listening", "addr", cfg.Addr)

	select {
	case err := <-errCh:
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("graceful shutdown incomplete", "error", err)
		}
	}
}
