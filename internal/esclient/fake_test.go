package esclient

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeCluster is an in-memory stand-in for an Elasticsearch / OpenSearch HTTP
// endpoint. Routes map a request path (for example "/_cat/nodes") to a canned
// JSON response body. It records what it received so tests can assert on it.
type fakeCluster struct {
	distribution string // "" for Elasticsearch, "opensearch" for OpenSearch
	number       string
	routes       map[string]string

	mu         sync.Mutex
	rootHits   int
	queries    map[string]url.Values
	bodies     map[string]string
	lastHeader http.Header
}

func newFakeCluster(distribution, number string, routes map[string]string) *fakeCluster {
	return &fakeCluster{
		distribution: distribution,
		number:       number,
		routes:       routes,
		queries:      map[string]url.Values{},
		bodies:       map[string]string{},
	}
}

func (f *fakeCluster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	f.mu.Lock()
	f.lastHeader = r.Header.Clone()
	f.queries[r.URL.Path] = r.URL.Query()
	f.bodies[r.URL.Path] = string(body)
	if r.URL.Path == "/" {
		f.rootHits++
	}
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	if r.URL.Path == "/" {
		distribution := ""
		if f.distribution != "" {
			distribution = `,"distribution":"` + f.distribution + `"`
		}
		_, _ = io.WriteString(w, `{"version":{"number":"`+f.number+`"`+distribution+`}}`)
		return
	}

	payload, ok := f.routes[r.URL.Path]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"no handler for `+r.URL.Path+`"}`)
		return
	}
	// A route value starting with "!<status> " forces an error response.
	if strings.HasPrefix(payload, "!") {
		status, message, _ := strings.Cut(payload[1:], " ")
		code := http.StatusInternalServerError
		if n, err := parseStatus(status); err == nil {
			code = n
		}
		w.WriteHeader(code)
		_, _ = io.WriteString(w, message)
		return
	}
	_, _ = io.WriteString(w, payload)
}

func parseStatus(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, io.ErrUnexpectedEOF
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

// start serves the fake and returns a client configured for it.
func (f *fakeCluster) start(t *testing.T, cfg Config) *Client {
	t.Helper()
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)

	if cfg.BaseURL == "" {
		cfg.BaseURL = server.URL
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func (f *fakeCluster) query(path string) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queries[path]
}

func (f *fakeCluster) body(path string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[path]
}

func (f *fakeCluster) header() http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastHeader
}

func (f *fakeCluster) rootRequests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rootHits
}
