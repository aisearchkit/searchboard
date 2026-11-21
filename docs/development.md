# Development Guide

## Prerequisites

| Tool | Version | Used for |
| :--- | :--- | :--- |
| Go | version in `go.mod` | server |
| Rust | stable, with `rustfmt` and `clippy` | agent |
| Node.js | 22 or newer | web UI |
| GitHub CLI (`gh`) | recent | opening, watching and merging pull requests |

## Common commands

```bash
make web      # build the UI into web/dist (embedded into the Go binary)
make server   # build bin/searchboard
make run      # run the server on http://localhost:9800
make test     # Go tests and Rust tests
make check    # everything CI runs: formatting, linting, tests, policy checks
```

During development run the server and the Vite dev server side by side. Vite proxies `/api` to the server.

```bash
go run ./cmd/searchboard
cd web && npm install && npm run dev
```

## Configuration

The server reads `configs/searchboard.yaml` (override with `-config`). Missing files fall back to built-in defaults. See `internal/config` for every option.

## Code conventions

- **Go**
  - Keep packages small and cohesive. Define interfaces in the package that consumes them.
  - Pass `context.Context` as the first argument of anything that does I/O.
  - Wrap errors with `%w` and add context. Do not log and return the same error.
  - No package-level mutable state. Wire dependencies in `cmd/searchboard`.
  - Table-driven tests next to the code. Use `httptest` for HTTP collaborators.
- **Rust**
  - `cargo fmt` and `cargo clippy -- -D warnings` must pass.
  - Keep pure logic (parsing, framing, merging) separate from I/O so it is unit-testable.
- **TypeScript and React**
  - `strict` TypeScript. No `any`.
  - Server state goes through TanStack Query. Local UI state stays in components. Zustand is only for state that must persist.
  - Feature code lives under `web/src/features/<feature>`. Shared primitives live in `web/src/components/ui`.
- **Everywhere**
  - English only, including identifiers, comments, logs, errors and docs.
  - Prefer small, readable functions and descriptive names over comments that restate the code.
  - Never commit secrets, tokens or real cluster addresses.

## Tests

Every behavior change comes with tests. Run `make check` before opening a pull request.
