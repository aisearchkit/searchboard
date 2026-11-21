# Web Framework

`framework/` is Searchboard's in-house HTTP/1.1 server framework. It is modeled on [cloudwego/hertz](https://github.com/cloudwego/hertz) and built on the same ideas: a layered stack, a pluggable transport based on [netpoll](https://github.com/cloudwego/netpoll), pooled request contexts and a fast JSON codec ([sonic](https://github.com/bytedance/sonic)). It is deliberately small: only what Searchboard needs, with no code generation and no reflection-heavy magic.

**Status:** the framework is complete and tested but not wired in yet. `internal/api` still uses `chi`; migrating it onto this framework is a separate change.

The framework lives in the same Go module as the server and has no dependency on any other Searchboard package, so it can be moved into its own module later without changes.

## Layers

```mermaid
flowchart TB
    subgraph app["framework (application layer)"]
        Server["Server<br/>lifecycle, dispatch, shutdown"]
        Group["RouterGroup<br/>prefixes + middleware"]
        Ctx["Context<br/>params, binding, rendering"]
        MW["middleware<br/>Recovery, RequestID, AccessLog, CORS"]
    end

    subgraph routing["route"]
        Tree["Segment trie<br/>static > :param > *catch-all"]
    end

    subgraph proto["protocol"]
        Msg["Request / Response / Header"]
        H1["http1<br/>strict parser and writer"]
    end

    subgraph net["network"]
        Iface["Reader / Writer / Conn / Transport interfaces"]
        NP["netpoll transport<br/>epoll / kqueue event loop"]
        STD["standard transport<br/>goroutine per connection"]
    end

    subgraph support["support packages"]
        Bind["binding<br/>query, path, header, form, JSON"]
        Codec["codec<br/>sonic (std-compatible)"]
        UT["ut<br/>in-process test helper"]
    end

    Server --> Group
    Server --> Tree
    Server --> H1
    Ctx --> Bind
    Ctx --> Codec
    H1 --> Msg
    H1 --> Iface
    Iface --> NP
    Iface --> STD
    MW --> Ctx
    UT --> Server
```

| Package | Responsibility |
| :--- | :--- |
| `framework` | `Server`, `RouterGroup`, `Context`, options, graceful shutdown |
| `framework/route` | Generic routing tree, independent of the handler type |
| `framework/protocol` | Protocol-independent `Request`, `Response` and `Header` |
| `framework/protocol/http1` | HTTP/1.1 request parser and response writer on top of `network.Reader` / `network.Writer` |
| `framework/network` | Transport interfaces and the two implementations (`netpoll`, `standard`) |
| `framework/binding` | Struct binding and `binding:"required"` validation |
| `framework/codec` | JSON codec (sonic) |
| `framework/middleware` | Ready-made middleware |
| `framework/ut` | Run a request through a server without a network, for tests |

## Quick start

```go
logger := slog.Default()

s := framework.New(framework.WithHostPorts(":8080"))
s.Use(middleware.RequestID(), middleware.AccessLog(logger), middleware.Recovery(logger))

api := s.Group("/api/v1")
api.GET("/users/:id", func(ctx context.Context, c *framework.Context) {
    c.JSON(http.StatusOK, map[string]string{"id": c.Param("id")})
})

if err := s.Spin(); err != nil { // serves until SIGINT or SIGTERM, then drains
    log.Fatal(err)
}
```

Use `middleware.RequestID`, `middleware.AccessLog`, `middleware.Recovery` in that order: the access log then carries the request ID and still observes the 500 that `Recovery` produces.

## Request lifecycle

```mermaid
sequenceDiagram
    participant T as Transport
    participant S as Server.serveConn
    participant P as http1
    participant R as Router
    participant H as Middleware + handler

    T->>S: data available on a connection
    loop while requests are buffered
        S->>S: wait for the first byte (idle timeout)
        S->>P: ReadHeader (read timeout, size limits)
        alt malformed request
            P-->>S: ProtocolError
            S->>T: error response (400, 413, 431, 501, 505) and close
        end
        opt Expect: 100-continue
            S->>T: 100 Continue
        end
        S->>P: ReadBody (Content-Length or chunked, size limit)
        S->>R: find route for method and path
        R-->>S: handler chain and path params
        S->>H: Next(ctx) through the chain
        H-->>S: Response filled in
        S->>P: WriteResponse (keep-alive decision)
        P->>T: single flush
    end
```

### Routing rules

- A segment is `static`, `:param` or `*catchall`. At each segment the priority is static, then param, then catch-all, with backtracking: `/users/me` beats `/users/:id`, yet `/users/me/posts` still reaches `/users/:id/posts`.
- A `:param` never matches an empty segment. A catch-all must be the last segment and its value keeps the leading slash (`/files/*path` captures `/a/b.txt`).
- Routing uses the raw, still percent-encoded path. `c.Param` decodes values, so `%2F` never changes which route matches.
- `/a` and `/a/` are different routes. When only the other form exists the server redirects (301 for GET and HEAD, 308 otherwise), and the redirect target can never become a protocol-relative URL.
- `HEAD` falls back to the `GET` route. When the path exists for other methods the server answers `405` with an `Allow` header. Both behaviors are customizable with `NoRoute` and `NoMethod`.
- Registration panics on programmer errors: duplicate routes, conflicting parameter names, a catch-all that is not last.

## HTTP/1.1 strictness

The parser rejects ambiguous framing instead of guessing, which is how request smuggling is prevented:

| Input | Result |
| :--- | :--- |
| Both `Content-Length` and `Transfer-Encoding` | 400 |
| Conflicting or malformed `Content-Length` (sign, non-digits, duplicates that differ) | 400 |
| `Transfer-Encoding` other than exactly `chunked` | 501 |
| Bare LF line endings, obsolete line folding, whitespace before the colon | 400 or connection closed |
| Missing or duplicate `Host` on HTTP/1.1 | 400 |
| Header block larger than `MaxHeaderBytes` (default 32 KiB) or too many fields | 431 |
| Body larger than `MaxBodyBytes` (default 4 MiB) | 413 |
| Unsupported HTTP version | 505 |

Responses never trust handler-supplied `Content-Length`, `Connection` or `Transfer-Encoding`, and CR or LF inside header names or values are replaced by spaces to prevent response splitting.

## Transports

```mermaid
flowchart LR
    Conn["Connection"] --> Q{"Platform"}
    Q -- "Linux, macOS, BSD" --> NP["netpoll<br/>(default)"]
    Q -- "other" --> STD["standard"]
    NP --> Serve["ServeFunc"]
    STD --> Serve
```

- **netpoll** (default on Linux, macOS and the BSDs) multiplexes connections over epoll or kqueue and reads without copying. netpoll's own idle timeout is only a TCP keepalive period, so the transport arms a per-connection timer to close connections that stay silent for the configured `WithIdleTimeout`.
- **standard** uses one goroutine per connection and a `bufio` reader. Its buffer size (64 KiB by default) caps the largest request header it can read.
- Both implement `network.Transport`. Pass your own with `framework.WithTransport`.

## Graceful shutdown

```mermaid
sequenceDiagram
    participant Sig as SIGINT / SIGTERM
    participant S as Server
    participant C as Clients
    participant T as Transport

    Sig->>S: Spin receives the signal
    S->>S: draining = true
    C->>S: new request
    S-->>C: 503, Connection: close
    S->>S: wait until no request is in flight (or timeout)
    Note over S: in-flight responses finish and carry Connection: close
    S->>T: Shutdown: stop accepting, close connections
    T-->>S: Serve returns nil
```

`Shutdown(ctx)` always closes the transport, even when `ctx` expires first; in that case it returns `ctx.Err()`. `Spin` uses `WithShutdownTimeout` (10 seconds by default).

## Context and handler rules

- Handlers have the signature `func(ctx context.Context, c *framework.Context)`. Middleware calls `c.Next(ctx)`; `c.Abort()` and the `AbortWith...` helpers stop the chain.
- **Contexts, requests, responses and bodies are pooled.** Never keep `c`, `c.Request`, headers or the body after the handler returns. Copy what you need.
- Binding tags: `path`, `query`, `header`, `form` for their sources, `json` for bodies, and `binding:"required"` for validation (a required field must be non-zero; use a pointer to accept a zero value). `c.Bind` merges path, query and the body chosen by `Content-Type`.
- `c.ClientIP()` returns the directly connected peer. Forwarding headers are intentionally not trusted.
- `ctx` is not canceled when the client disconnects. Use timeouts on downstream calls.

## Dependencies

| Library | Used for |
| :--- | :--- |
| `github.com/cloudwego/netpoll` | Event-loop transport |
| `github.com/bytedance/sonic` | JSON codec in standard-library compatible mode |
| `github.com/bytedance/gopkg` (`lang/mcache`) | Pooled buffers for request bodies |

sonic below v1.15.5 does not support Go 1.27: it falls back to `encoding/json` and prints a warning at start-up. Keep sonic at v1.15.5 or newer. The codec is isolated in `framework/codec`, so swapping it is a one-file change.

## Not supported

HTTP/2, TLS termination (terminate TLS in front of Searchboard), WebSocket upgrades, multipart forms, cookies and trusted-proxy handling. They can be added behind the existing layers when needed.

## Testing

- `ut.PerformRequest(server, method, target, body, headers...)` runs a request through routing and middleware with no network.
- `framework/e2e_test.go` exercises real TCP connections against both transports: keep-alive, pipelining, chunked bodies, `Expect: 100-continue`, malformed requests, timeouts and graceful shutdown.
- Parser and writer tests feed the stream one byte at a time to prove they handle partial reads.
