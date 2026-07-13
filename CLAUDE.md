# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go HTTP server that serves Wikipedia/Wiktionary article content directly from local multistream XML dump files (no database), with a JSON API and a React web UI. Based originally on https://github.com/d4l3k/wikigopher.

## Commands

```bash
# Backend — tests are hermetic (tiny committed dumps in wikipedia/testdata)
go test ./...
go test -race ./...
go test ./wikipedia/ -run TestGetArticle          # single test
go test ./wikipedia/ -bench .                     # benchmarks
DUMP_PATH=/path go test ./wikipedia/ -run TestRealDump  # opt-in integration test

go build ./... && go vet ./...
go run .            # needs DUMP_PATH via env or .env file

# Frontend (web/)
cd web && npm install
npm run dev         # Vite dev server on :5173, proxies /api to :9095
npm run build       # emits web/dist, served by the Go binary

# Docker
./start.sh          # docker compose up -d --build
./stop.sh
./download_data.sh  # fetch dumps (~25 GB) into $DUMP_PATH
wikipedia/testdata/generate.sh  # regenerate test fixtures (needs bzip2 CLI)
```

Config is env-based (`config.go` reads a `.env` file for missing vars): `DUMP_PATH` (required), `PORT` (default 9095), `WEB_DIR` (default `web/dist`).

## Architecture

The substance is in the `wikipedia` package; `main.go`/`server.go` are the HTTP layer, `web/` is the UI.

**Dump format**: Wikipedia publishes per-wiki pairs: an index (`...-multistream-index.txt.bz2`, lines of `seekOffset:pageID:title`) and an articles file (`...-multistream.xml.bz2`) made of independently-decompressible bzip2 streams of ~100 pages each.

**SQLite-backed index (`wikipedia/store.go`)**: the text index is loaded into a small on-disk SQLite database (pure-Go driver `modernc.org/sqlite`, no cgo) — one `pages(title, id, seek)` row per page, indexed on `title` (unique) and on `seek`, plus a `meta` key/value table holding the schema version and the source index's size+mtime for staleness detection. Article bodies are **never** stored here — only the byte offset needed to find them in the `.xml.bz2` file. `buildFileStore` streams rows into a single transaction one line at a time (no full in-memory sort of ~30M titles) and creates the indexes only *after* the bulk insert, so SQLite builds each with one sort-and-build pass instead of maintaining a B-tree per random insert; `temp_store` is deliberately left at its file-based default (not overridden to memory) so that sort spills to disk instead of the Go heap. The cache file is persisted as `<index-name>.sqlite` next to the dumps via build-into-`.tmp`-then-`os.Rename` (atomic, crash-safe). On later startups, `openStore` just opens that file read-only (`PRAGMA query_only`) with a single pooled connection (`SetMaxOpenConns(1)`, so per-connection PRAGMAs like `mmap_size`/`cache_size` apply deterministically) — memory stays bounded by SQLite's own capped page cache, not by dump size.

**Lookup flow (`wikipedia/wikipedia.go`)**:
1. `lookup` normalizes the title (underscores→spaces) and tries capitalization variants (as-is, first-upper, Title Case, lower) as exact point queries (`store.find`) against the unique `title` index. Note: `x/text` `cases.Caser` is stateful/not goroutine-safe — it is created per call, do not hoist it onto `Wiki`.
2. `readPage` finds the containing stream's end via `store.streamEnd` (`SELECT seek FROM pages WHERE seek > ? ORDER BY seek LIMIT 1`, using the `seek` index), wraps the long-lived articles `*os.File` in an `io.NewSectionReader` (concurrency-safe, no shared seek state, no per-request open), decompresses just that one stream with stdlib `bzip2`, and XML-decodes pages until the id matches.
3. `GetArticle` follows redirect pages (bounded hops, cycle-safe) and reports the chain in `RedirectedFrom`.

After `Open` returns, the store is immutable — everything is safe for concurrent use without locks (`database/sql` itself is goroutine-safe regardless of pool size).

**Search** (`Wiki.Search`) is prefix search: for each case variant, `store.searchPrefix` runs an indexed range scan (`title >= prefix AND title < prefixUpperBound(prefix)`, relying on SQLite's default BINARY collation comparing raw bytes — same semantics as the old byte-prefix walk), then Go-side dedupes and ranks shorter titles first.

**HTTP layer (`server.go`)**: stdlib `http.ServeMux` with Go 1.22 patterns (`GET /api/{source}/page/{title...}`). Sources (`wiki` = enwiki, `dict` = enwiktionary, defined in `main.go`) load sequentially at startup — deliberately, to bound the first-run memory peak — and missing dumps are skipped with a warning rather than failing. JSON errors use `{"error":{code,message}}`. Legacy pre-2.0 endpoints (`/wiki?page=`, `/dict?page=`, `/search?name=`) are kept for compatibility. The UI is served from `WEB_DIR` with an SPA fallback.

**Test fixtures**: `wikipedia/testdata/` contains real (tiny) multistream dumps built by `generate.sh` — actual concatenated bzip2 streams with a matching index, covering redirects, unicode and colon-in-title cases. If you change dump parsing, regenerate rather than hand-edit.

## Gotchas

- `go-pbzip2` shells out to the system `pbzip2` binary and falls back to slow sequential bzip2 if absent. It is absent from the Docker image (removed from Alpine 3.24 repos), so the first in-container index build is sequential — a one-time cost thanks to the cache.
- The compose volume for `$DUMP_PATH` must stay writable so the server can persist `*.sqlite` cache files (it degrades gracefully but re-parses on every start otherwise). Any leftover `*.idx` file from the previous (pre-SQLite) cache format is now dead weight and can be deleted.
- `Options.Limit` (tests/benchmarks) no longer skips disk entirely — it always (re)builds into `CacheDir`, just without the "reuse an existing cache" check, so it's still fully disk-backed like production. Tests pass a fresh `t.TempDir()` as `CacheDir` to avoid colliding with any real cache.
- The Docker build compiles the web UI in a Node stage; `web/package-lock.json` must stay committed for `npm ci`.
