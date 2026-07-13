# Wikipedia server

Minimal, self-hosted Wikipedia/Wiktionary API server working straight from the
official multistream dumps — no database, no external services.

Freely based on https://github.com/d4l3k/wikigopher.

## Features

- **JSON API**: fetch any article by title, with redirect resolution and
  prefix title search (autocompletion-friendly).
- **Low memory**: the dump index is compiled once into a compact binary cache
  that is **mmapped** on later startups — restarts are near-instant and
  resident memory stays proportional to what is actually read.
- **No preprocessing of the dumps**: articles are decompressed on demand,
  one bzip2 stream (~100 pages) at a time, directly from the `.xml.bz2` file.
- **Web UI**: a small React interface with search-as-you-type and a readable
  article preview.

## Quick start (Docker)

```sh
git clone https://github.com/fabriceboyer/wikipedia_server
cd wikipedia_server
cp .env.example .env          # set DUMP_PATH to where dumps should live
./download_data.sh            # downloads ~25 GB from dumps.wikimedia.org
./start.sh                    # builds and starts the compose stack
```

Then open http://localhost:9095.

The very first startup parses the dump indexes (a few minutes, and a transient
memory peak of a few GB while sorting ~30M titles); the resulting `*.idx`
cache files are saved next to the dumps, and every restart after that loads
them in milliseconds via mmap. Stop with `./stop.sh`.

## JSON API

| Endpoint | Description |
|---|---|
| `GET /api/sources` | Loaded sources (`wiki`, `dict`) with page counts |
| `GET /api/status` | Server status, uptime, sources |
| `GET /api/{source}/search?q=<prefix>&limit=20` | Title prefix search (case-variant aware) |
| `GET /api/{source}/page/{title}` | Full page as JSON (see below) |
| `GET /api/{source}/page/{title}?raw=1` | Wikitext only, as `text/plain` |
| `GET /api/{source}/page/{title}?follow=0` | Do not resolve redirects |
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

## How it works

Wikipedia *multistream* dumps come in pairs:

- `*-multistream-index.txt.bz2` — one `offset:id:title` line per page;
- `*-multistream.xml.bz2` — a concatenation of independent bzip2 streams of
  ~100 `<page>` elements each, so any stream can be decompressed on its own.

At first startup the index is parsed and compiled into a flat binary file
(`*.idx`, format `WIX1`): titles sorted and concatenated in one blob, plus
packed offset/id arrays. Lookups are binary searches over that buffer;
because the buffer is a read-only mmap of the cache file, the OS pages it in
on demand and can evict it under pressure. Fetching a page then seeks to its
stream in the `.xml.bz2`, decompresses just that stream, and scans it for the
page id.

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

The test fixtures under `wikipedia/testdata/` are real (tiny) multistream
dumps; regenerate them with `wikipedia/testdata/generate.sh` if the format
handling changes.

## Prerequisites (without Docker)

- Go ≥ 1.25, Node ≥ 20 (UI only)
- `pbzip2` (optional but strongly recommended: parallel decompression of the
  index on first startup)
