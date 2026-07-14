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
cd web && npm install   # also vendors swagger-ui-dist assets (postinstall)
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

**SQLite-backed index (`wikipedia/store.go`)**: the text index is loaded into a small on-disk SQLite database (pure-Go driver `modernc.org/sqlite`, no cgo) — one `pages(title, id, seek)` row per page, indexed on `title` (**not** unique — see Gotchas) and on `seek`, plus a `meta` key/value table holding the schema version and the source index's size+mtime for staleness detection. Article bodies are **never** stored here — only the byte offset needed to find them in the `.xml.bz2` file. `buildFileStore` streams rows into a single transaction one line at a time (no full in-memory sort of ~30M titles) and creates the indexes only *after* the bulk insert, so SQLite builds each with one sort-and-build pass instead of maintaining a B-tree per random insert; `temp_store` is deliberately left at its file-based default (not overridden to memory) so that sort spills to disk instead of the Go heap. The cache file is persisted as `<index-name>.sqlite` next to the dumps via build-into-`.tmp`-then-`os.Rename` (atomic, crash-safe). On later startups, `openStore` just opens that file read-only (`PRAGMA query_only`) with a single pooled connection (`SetMaxOpenConns(1)`, so per-connection PRAGMAs like `mmap_size`/`cache_size` apply deterministically) — memory stays bounded by SQLite's own capped page cache, not by dump size.

**Lookup flow (`wikipedia/wikipedia.go`)**:
1. `lookup` normalizes the title (underscores→spaces, NFC) and tries capitalization variants (as-is, first-upper, Title Case, lower) as exact point queries (`store.find`) against the `title` index. Note: `x/text` `cases.Caser` is stateful/not goroutine-safe — it is created per call, do not hoist it onto `Wiki`.
2. `readPage` finds the containing stream's end via `store.streamEnd` (`SELECT seek FROM pages WHERE seek > ? ORDER BY seek LIMIT 1`, using the `seek` index), wraps the long-lived articles `*os.File` in an `io.NewSectionReader` (concurrency-safe, no shared seek state, no per-request open), decompresses just that one stream with stdlib `bzip2`, and XML-decodes pages until the id matches, up to `maxPagesPerStream` (a generous safety net, not a real limit — see Gotchas).
3. `GetArticle`/`RandomArticle`/`followRedirects` follow redirect pages (bounded hops, cycle-safe) and report the chain in `RedirectedFrom`; both entry points share the `followRedirects` loop, `pageToArticle` builds the response DTO.

After `Open` returns, the store is immutable — everything is safe for concurrent use without locks (`database/sql` itself is goroutine-safe regardless of pool size).

**Search** (`Wiki.Search`) is prefix search: for each case variant, `store.searchPrefix` runs an indexed range scan (`title >= prefix AND title < prefixUpperBound(prefix)`, relying on SQLite's default BINARY collation comparing raw bytes — same semantics as the old byte-prefix walk), then Go-side dedupes and ranks shorter titles first. `store.random` picks a uniformly-random page via a direct rowid lookup (rows are only ever bulk-inserted once, so rowids are contiguous — no `ORDER BY RANDOM()` full scan).

**HTTP layer (`server.go`)**: stdlib `http.ServeMux` with Go 1.22 patterns (`GET /api/{source}/page/{title...}`). Sources (`wiki` = enwiki, `dict` = enwiktionary, defined in `main.go`) load sequentially at startup — deliberately, to bound the first-run memory peak — and missing dumps are skipped with a warning rather than failing. JSON errors use `{"error":{code,message}}`. Legacy pre-2.0 endpoints (`/wiki?page=`, `/dict?page=`, `/search?name=`) are kept for compatibility. The UI is served from `WEB_DIR` with an SPA fallback. `GET /api/openapi.yaml` serves `openapi.yaml` (embedded via `openapi.go`'s `//go:embed`) — keep it in sync when routes/schemas change.

**API docs**: `web/public/swagger/index.html` (hand-written, committed) loads Swagger UI against `/api/openapi.yaml`. The `swagger-ui-dist` JS/CSS/PNG assets it references are vendored into that same directory by `web/scripts/copy-swagger-ui.mjs`, run via the `postinstall` npm script — regenerated on every `npm install`, gitignored, never committed.

**Backlinks / "what links here" (`wikipedia/backlinks.go`)**: a second, separate on-disk index (`<index-name>-backlinks.sqlite`, own `backlinks(target_id, source_title)` table) mapping a page to every page whose wikitext links to it — the reverse of what the title index gives you. Unlike the title index this needs each page's *full text*, not just the small `.txt.bz2` index file, so it's a full pass over the `.xml.bz2` dump; `Open` kicks it off in its own goroutine (`Wiki.buildBacklinksAsync`) and returns immediately — it is never on the startup path. `buildBacklinkStore` groups by bzip2 stream (`store.streams()`, decompress once per stream, not once per page) and fans extraction out across a worker per CPU (`runtime.NumCPU()`), each resolving link targets via a *dedicated* connection pool to the title-index file (`sql.Open` on `storePath` again, `SetMaxOpenConns(numWorkers)`) so bulk resolution doesn't serialize behind the primary single-connection store used for normal request-serving; a single goroutine drains results into the one SQLite writer connection (only one writer is possible). `Wiki.LinksTo` returns `ErrBacklinksNotReady` (surfaced as `{"ready": false}` by the API, not an error status) until the background build finishes or a valid cache is found; `/api/sources`' `backlinksReady` field mirrors this per source. Pagination is keyset (`source_title > after`, not `OFFSET`) since some targets have hundreds of thousands of incoming links. `Wiki.Close` cancels a `context.Context` threaded through the whole build and blocks on a `sync.WaitGroup` until every worker goroutine has actually exited before closing the files they read from — **don't skip this if you touch this code**: an earlier version without it let a canceled build's goroutines keep running after their host `*os.File`/`*sql.DB` were closed, which is at best wasted work and at worst (mmap + concurrent external truncation of the same path, as `TestIndexCacheRoundtrip` does deliberately) a `SIGBUS` crash.

**Test fixtures**: `wikipedia/testdata/` contains real (tiny) multistream dumps built by `generate.sh` — actual concatenated bzip2 streams with a matching index, covering redirects, unicode, colon-in-title, a duplicate title, a 1200-page oversized stream, and 25 cross-linked pages for backlinks pagination tests (regression coverage for the dump-parsing bugs and the backlinks feature below). If you change dump parsing, regenerate rather than hand-edit.

**Wikitext rendering (`web/src/wikitext.ts`)**: a small non-evaluating converter, not a MediaWiki parser — templates/tables are stripped, not rendered. Worth knowing if you touch it: `<ref(?=[\s/>])` (not bare `<ref`) is required in the tag-stripping regexes because `ref` is a text-prefix of `references`, so an unanchored pattern also eats `<references>` and leaves an orphaned `</references>` behind. `[[Category:X]]` links are extracted out of the flow (`extractCategories`) rather than left as inline dead links. `[[File:...|caption]]` captions are pulled out with a depth-aware bracket scanner (`findMatchingClose`/`splitTopLevel`), not a regex, because captions routinely contain their own nested `[[link]]`. List markers whose entire content was a stripped template collapse to an empty string and must be dropped, not emitted as `<li>` — the list-detection regex intentionally matches zero-content (`(.*)`, not `(.+)`) so this case can be caught explicitly, instead of falling through to the plain-paragraph branch where runs of bare markers used to get joined into a stray line of literal asterisks.

## Gotchas

- `go-pbzip2` shells out to the system `pbzip2` binary and falls back to slow sequential bzip2 if absent. It is absent from the Docker image (removed from Alpine 3.24 repos), so the first in-container index build is sequential — a one-time cost thanks to the cache.
- The compose volume for `$DUMP_PATH` must stay writable so the server can persist `*.sqlite` cache files (it degrades gracefully but re-parses on every start otherwise). Any leftover `*.idx` file from the previous (pre-SQLite) cache format is now dead weight and can be deleted.
- `Options.Limit` (tests/benchmarks) no longer skips disk entirely — it always (re)builds into `CacheDir`, just without the "reuse an existing cache" check, so it's still fully disk-backed like production. Tests pass a fresh `t.TempDir()` as `CacheDir` to avoid colliding with any real cache.
- The Docker build compiles the web UI in a Node stage; `web/package-lock.json` must stay committed for `npm ci`.
- **The real enwiki index contains a handful of exact-duplicate titles** (confirmed empirically: ~16 out of ~26M rows, e.g. two different page ids both titled "WWBG" — likely a stale row from a page move racing the dump snapshot). `pages.title` must stay a non-unique index; a `UNIQUE` constraint there previously made the *entire* wiki source fail to load (silently skipped at startup, `enwiki` simply never available) over this handful of rows. `find` resolves duplicates deterministically via `ORDER BY id DESC LIMIT 1`.
- **Real multistream dumps do not reliably cap at ~100 pages per bzip2 stream** — some streams (long runs of short redirect stubs) pack over a thousand. An earlier fixed `decodesPerStream` cap of 1000 silently made pages past it in such streams "not found" even though they're in the dump. `readPage` now decodes until the stream's own EOF (bounded by the next stream's offset via `store.streamEnd`), with `maxPagesPerStream` only as a defensive backstop against corrupt input, not a real limit.
- **The backlinks build is genuinely slow on the real dump** — it's a full decompress-and-parse pass over the entire `.xml.bz2` (as opposed to the ~1-5 minute title-index build, which only touches the small `.txt.bz2` index), easily tens of minutes to a few hours for enwiki depending on CPU count and whether `pbzip2` is installed. This is expected and by design (background, non-blocking); don't "fix" it by making `Open` wait for it.
- If you add a test that deliberately corrupts/replaces a store's `.sqlite` file on disk (like `TestIndexCacheRoundtrip` does), **close that `Wiki` first**. A still-open `Wiki` may have a background backlinks build holding an mmapped connection to the same file; truncating it out from under that mmap is a `SIGBUS`, not a catchable Go error — this isn't something any mmap-based reader is expected to survive, so the fix is to not do it to a live instance, not to harden the code against it.
