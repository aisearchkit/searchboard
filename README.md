# Searchboard

Lightweight, reliable observability, migration and AI-ops platform for Elasticsearch / OpenSearch.
Design: [docs/architecture.md](docs/architecture.md)

| Component | Path | Stack |
|---|---|---|
| Server | `cmd/`, `internal/` | Go (chi, modernc SQLite) |
| Node agent | `agent-rs/` | Rust (tokio, rustls, optional quinn) |
| Web UI | `web/` | Vite, React 19, Tailwind v4, shadcn/ui |

## Quick start

```bash
make web      # build UI into web/dist (embedded into the Go binary)
make server   # build bin/searchboard
make run      # http://localhost:9800
make test     # go test + cargo test
```

Dev: `go run ./cmd/searchboard` and, in `web/`, `npm run dev` (proxies `/api` to :9800).

## Status

Skeleton with real logic + tests for: pressure guard, AIMD controller, ES version capability detection,
log anomaly rules, agent multiline merger / drop-oldest ring / UDP framing. Stubs (TODO) for: local TSDB
engine, remote write, agent hub, Drain clustering, migration reader/writer, inspector rules, API endpoints.
