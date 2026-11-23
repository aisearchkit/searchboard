# Elasticsearch and OpenSearch Adapter

The `internal/esclient` package is the only place that knows how Elasticsearch (5.x to 9.x) and OpenSearch (1.x and 2.x) differ. The rest of the server depends on the `ClusterReader` interface and receives version-independent domain types from `internal/domain`.

## Request flow

```mermaid
sequenceDiagram
    participant Caller as Server code
    participant Client as esclient.Client
    participant Cluster as ES / OpenSearch

    Caller->>Client: Nodes(ctx)
    alt version not cached
        Client->>Cluster: GET / (filter_path=version.*)
        Cluster-->>Client: number + distribution
        Client->>Client: Parse flavor and version, cache it
    end
    Client->>Client: Choose columns for this version
    Client->>Cluster: GET _cat/nodes?format=json&h=...
    Cluster-->>Client: JSON rows
    Client-->>Caller: []domain.Node
```

## Design rules

- **Detect once, adapt per call.** `Client.Version` calls `GET /` on first use and caches the result. Version-dependent behavior is expressed as capability methods on `Version`, never as scattered `if major >= N` checks.
- **Ask for exactly the columns we need.** `_cat` endpoints are called with `format=json` and an explicit `h=` column list. Elasticsearch echoes the requested names as JSON keys, so the parser reads cells by the names it asked for.
- **Tolerate missing data.** Nulls and absent cells become empty values. Metrics that a node did not report are `nil` pointers in the domain model rather than zeros.
- **Fail loudly on malformed data.** An unparsable shard number is an error, not a silently skipped row.
- **Bounded I/O.** Every call has a timeout, and responses are capped at 128 MiB.
- **Typed errors.** A non-2xx response is an `*APIError`. Use `esclient.HasStatus(err, code)` to branch on it.

## Raw requests for the console

`Passthrough(ctx, method, path, body)` forwards a request exactly as the DevTools console typed it and returns the cluster's answer, including error statuses: a `404` or `400` from the cluster is an answer to show, not a failure of Searchboard.

- The path may carry a query string (`logs-*/_search?size=1`).
- It must stay below the configured base URL. `..` and `.` segments (also percent-encoded), absolute URLs, fragments, backslashes, whitespace and control characters are rejected with `ErrInvalidPath` before anything is sent.
- Bodies are sent as `application/x-ndjson` for `_bulk` and `_msearch`, and as `application/json` otherwise.
- Answers are capped at 16 MiB (`ErrResponseTooLarge`). Narrow the request with `size`, `_source` filtering or scroll.

## Schema for editor completion

`Schema(ctx)` returns the open indices, their aliases and their mapping fields flattened to dotted paths (`user.address.city`, `title.keyword`).

```mermaid
flowchart LR
    A["GET _mapping<br/>filter_path=*.mappings"] --> M["Flatten per index"]
    B["GET _alias<br/>filter_path=*.aliases"] --> J["Merge aliases"]
    M --> J
    J --> S["Schema<br/>regular indices first, hidden last"]
```

- Typeless mappings (Elasticsearch 7+ and OpenSearch) and mappings wrapped in a type name (Elasticsearch 5 and 6, and 7 with `include_type_name`) are both understood. With several types, their fields are merged.
- Object and nested sub-fields and multi-fields are included. Mapping settings such as `dynamic` and `_meta` are ignored.
- Limits keep huge clusters usable: 2000 indices, 2000 fields per index and 50000 fields in total. Hidden indices (names starting with a dot) sort last, so limits cut them first, and `truncated` tells the caller that something was left out.

## Version differences handled

| Concern | Elasticsearch | OpenSearch | Where |
| :--- | :--- | :--- | :--- |
| Flavor detection | `version.number` only | `version.distribution == "opensearch"` | `Client.Version` |
| Elected master column in `_cat/nodes` | `master` | `cluster_manager` (2.x), `master` (1.x) | `Client.Nodes` |
| Point-in-Time search | 7.10 and newer | 2.4 and newer | `Version.SupportsPIT` |
| Reliable `search_after` | 6.8 and newer | all versions | `Version.SupportsSearchAfter` |
| Mapping types removed | 8.0 and newer | 2.0 and newer | `Version.RemovedMappingTypes` |
| Relocating shard rendering | `a -> ip id b` in `node`, `a -> b` in `id` | same | `splitRelocation` |

The migration engine will use the capability methods to pick its reader strategy (see [architecture.md](architecture.md), section 7.2).

## Adding support for a new difference

1. Add a capability method to `Version` with a table-driven test in `version_test.go`.
2. Branch on that method inside the typed API (`Nodes`, `Shards`, ...), not in callers.
3. Add a fake-cluster test that asserts the request the new version receives.
4. Add a row to the table above.
