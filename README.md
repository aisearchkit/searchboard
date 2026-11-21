# Searchboard

A lightweight, reliable observability, data migration and AI operations platform for Elasticsearch and OpenSearch.

| Component | Path | Stack |
| :--- | :--- | :--- |
| Server | `cmd/`, `internal/` | Go (chi, pure Go SQLite) |
| Node agent | `agent-rs/` | Rust (tokio, rustls, optional quinn) |
| Web UI | `web/` | Vite, React 19, Tailwind CSS v4, shadcn/ui |

## Quick start

```bash
make web      # build the UI into web/dist (embedded into the Go binary)
make server   # build bin/searchboard
make run      # http://localhost:9800
make check    # run everything CI runs
```

## Documentation

Everything lives in [docs/](docs/README.md): [architecture](docs/architecture.md), [development guide](docs/development.md), [contributing](docs/contributing.md) and [AI agent guidelines](docs/ai-guidelines.md).

Searchboard is developed by multiple AI agents together with human maintainers. All changes go through pull requests with a conventional-commit title, and a pull request is squash merged by its author once CI is green. See [docs/contributing.md](docs/contributing.md) and [docs/ai-guidelines.md](docs/ai-guidelines.md).
