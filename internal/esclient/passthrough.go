package esclient

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

// passthroughLimit caps what an interactive console request may return. Larger
// answers should be narrowed with size, _source filtering or scroll.
const passthroughLimit = 16 << 20

// ErrInvalidPath is returned for a request path that is not safe to forward.
var ErrInvalidPath = errors.New("esclient: invalid request path")

// Requester sends arbitrary requests to a cluster, as the DevTools console does.
type Requester interface {
	Passthrough(ctx context.Context, method, path string, body []byte) (RawResponse, error)
}

var _ Requester = (*Client)(nil)

// Passthrough forwards a request to the cluster and returns the answer as it is,
// including error statuses. path may carry a query string, for example
// "logs-*/_search?size=1". The path must stay below the configured base URL:
// ".." segments, absolute URLs, fragments and control characters are rejected.
//
// A body is sent as application/x-ndjson for the bulk-style endpoints that
// require it and as application/json otherwise.
func (c *Client) Passthrough(ctx context.Context, method, path string, body []byte) (RawResponse, error) {
	pathPart, rawQuery, _ := strings.Cut(path, "?")
	pathPart = strings.TrimPrefix(pathPart, "/")
	if !safePath(pathPart) || !safeQuery(rawQuery) {
		return RawResponse{}, ErrInvalidPath
	}

	target := c.base.JoinPath(pathPart)
	target.RawQuery = rawQuery
	if _, err := url.ParseQuery(rawQuery); err != nil {
		return RawResponse{}, ErrInvalidPath
	}

	contentType := "application/json"
	if isNDJSONEndpoint(pathPart) {
		contentType = "application/x-ndjson"
	}
	return c.roundTrip(ctx, method, target, body, contentType, passthroughLimit)
}

func safePath(p string) bool {
	if strings.Contains(p, "://") || strings.ContainsAny(p, "#\\") {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] <= 0x20 || p[i] == 0x7f {
			return false
		}
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." || segment == "." {
			return false
		}
		if decoded, err := url.PathUnescape(segment); err != nil || decoded == ".." {
			return false
		}
	}
	return true
}

func safeQuery(q string) bool {
	if strings.Contains(q, "#") {
		return false
	}
	for i := 0; i < len(q); i++ {
		if q[i] < 0x20 || q[i] == 0x7f {
			return false
		}
	}
	return true
}

func isNDJSONEndpoint(path string) bool {
	last := path[strings.LastIndex(path, "/")+1:]
	return last == "_bulk" || last == "_msearch"
}
