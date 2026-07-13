# Wikipedia server

Minimal, self-hosted Wikipedia/Wiktionary API server working straight from the
official multistream dumps — no database, no external services.

Freely based on https://github.com/d4l3k/wikigopher.

## Features

- **JSON API**: fetch any article by title, with redirect resolution and
  prefix title search (autocompletion-friendly).
- **Low memory, fast restarts**: the dump's title index is loaded into a
  small on-disk **SQLite** database once; every startup after that just
  opens that file — no re-parsing, no multi-gigabyte heap, and resident
  memory stays bounded by SQLite's own page cache rather than by the number
  of pages in the dump.
- **No preprocessing of the dumps**: article bodies are never stored in the
  database — they're decompressed on demand, one bzip2 stream (~100 pages)
  at a time, straight from the `.xml.bz2` file on disk.
- **Web UI**: a React interface with search-as-you-type, a readable article
  preview (with a table of contents, category tags, and working in-article
  links), a random-article button, and shareable/deep-linkable URLs
  (including same-page section anchors).
- **API docs**: an OpenAPI 3.0 spec at `/api/openapi.yaml`, browsable with a
  bundled (offline, no CDN) Swagger UI at `/swagger/`.

## Quick start (Docker)

```sh
git clone https://github.com/fabriceboyer/wikipedia_server
cd wikipedia_server
cp .env.example .env          # set DUMP_PATH to where dumps should live
./download_data.sh            # downloads ~25 GB from dumps.wikimedia.org
./start.sh                    # builds and starts the compose stack
```

Then open http://localhost:9095.

The very first startup parses the dump indexes into `*.sqlite` cache files
saved next to the dumps (a few minutes, streamed row by row rather than
sorted in memory, so it doesn't spike). Every restart after that just opens
those files. Stop with `./stop.sh`.

## JSON API

| Endpoint | Description |
|---|---|
| `GET /api/sources` | Loaded sources (`wiki`, `dict`) with page counts |
| `GET /api/status` | Server status, uptime, sources |
| `GET /api/{source}/search?q=<prefix>&limit=20` | Title prefix search (case-variant aware) |
| `GET /api/{source}/random` | A uniformly-random article |
| `GET /api/{source}/page/{title}` | Full page as JSON (see below) |
| `GET /api/{source}/page/{title}?raw=1` | Wikitext only, as `text/plain` |
| `GET /api/{source}/page/{title}?follow=0` | Do not resolve redirects |
| `GET /api/openapi.yaml` | OpenAPI 3.0 spec (browsable at `/swagger/`) |
| `GET /healthz` | Liveness probe |

`{source}` is `wiki` (Wikipedia) or `dict` (Wiktionary). Titles are matched
case-insensitively for common capitalizations, and underscores are treated as
spaces.

```sh
$ curl 'localhost:9095/api/wiki/page/Einstein'
{
  "source": "wiki",
  "title": "Albert Einstein",
  "id": 736,
  "ns": 0,
  "revisionId": "...",
  "timestamp": "...",
  "redirectedFrom": ["Einstein"],
  "text": "'''Albert Einstein''' was ..."
}

$ curl 'localhost:9095/api/wiki/search?q=alber&limit=5'
{"source":"wiki","query":"alber","results":[{"title":"Albert Einstein","id":736}, ...]}
```

Errors are structured: `{"error":{"code":"not_found","message":"..."}}` with
matching HTTP status codes.

The pre-2.0 endpoints (`/wiki?page=`, `/dict?page=`, `/search?name=`) still
work but are deprecated.

The full interactive reference — every endpoint, parameter, response shape
and error code — is at `/swagger/` (served from the same binary, no internet
required); the raw spec is `/api/openapi.yaml`.

## How it works

Wikipedia *multistream* dumps come in pairs:

- `*-multistream-index.txt.bz2` — one `offset:id:title` line per page;
- `*-multistream.xml.bz2` — a concatenation of independent bzip2 streams of
  ~100 `<page>` elements each, so any stream can be decompressed on its own.

At first startup the index is parsed and loaded into a small SQLite database
(`*.sqlite`, one row per page: `title, id, seek`), indexed on `title` and on
`seek`. Title lookups and prefix search are then plain indexed SQL queries;
SQLite's own page cache (capped, not the whole file) backs them, so memory
stays low regardless of dump size. Article bodies are **never** stored in
that database: fetching a page looks up its stream offset, seeks there in
the `.xml.bz2`, decompresses just that one bzip2 stream, and scans it for the
page id — exactly like before, unchanged. The `title` index is deliberately
*not* unique: the real enwiki index occasionally has two rows with the exact
same title (e.g. a stale entry left over from a page move racing the dump
snapshot); a stricter constraint would abort loading the entire ~26M-page
source over a handful of rows, so lookups instead deterministically prefer
the higher page id. Streams are also decoded to their natural end (bounded
by the next stream's offset) rather than a fixed page-per-stream guess,
since real dumps occasionally pack far more than the nominal ~100 pages into
one stream (long runs of short redirect stubs).

The web UI's article view renders wikitext with a small, deliberately
incomplete converter (see `web/src/wikitext.ts`): headings, emphasis, lists,
internal/external links and image captions render; templates, tables and
`<ref>` footnotes are stripped rather than evaluated (there is no template
engine). `[[Category:X]]` links are pulled out of the flow and shown as tags
at the bottom instead of as inline dead links; interwiki-style prefixes we
can't resolve (e.g. `fr:`, `commons:`) render as plain text rather than a
broken link; `w:`/`wikt:` prefixes cross-link to the other loaded source;
same-page section links (`[[#Section]]` or `[[Title#Section]]` where `Title`
is the current page) scroll to the matching heading instead of re-fetching.
A "wikitext" toggle always shows the unprocessed source for anything the
simplified renderer can't handle.

## Development

```sh
go test ./...                     # hermetic tests (tiny dumps in wikipedia/testdata)
go test -race ./...               # with the race detector
go test ./wikipedia/ -run TestGetArticle   # single test
go test ./wikipedia/ -bench .     # micro-benchmarks
DUMP_PATH=/path/to/dumps go test ./wikipedia/ -run TestRealDump   # against real dumps

go run .                          # needs DUMP_PATH in env or .env
```

Configuration is environment-based (a `.env` file is read if present):
`DUMP_PATH` (required), `PORT` (default `9095`), `WEB_DIR` (default
`web/dist`).

### Web UI

```sh
cd web
npm install
npm run dev      # dev server on :5173, proxies /api to :9095
npm run build    # production build into web/dist, served by the Go server
```

`npm install` also vendors the handful of `swagger-ui-dist` assets the
`/swagger/` page needs into `web/public/swagger/` (via the `postinstall`
script), so it works fully offline — no CDN fetch at build or run time.
Only `web/public/swagger/index.html` itself is committed; the vendored
JS/CSS/PNGs are regenerated on every install and gitignored.

The test fixtures under `wikipedia/testdata/` are real (tiny) multistream
dumps — including a duplicate title and an oversized (1200-page) stream, to
exercise the two dump-parsing edge cases above; regenerate them with
`wikipedia/testdata/generate.sh` if the format handling changes.

## Prerequisites (without Docker)

- Go ≥ 1.25, Node ≥ 20 (UI only)
- `pbzip2` (optional but strongly recommended: parallel decompression of the
  index on first startup)
