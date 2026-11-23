package esclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// recorder captures the last request a handler saw.
type recorder struct {
	mu          sync.Mutex
	method      string
	path        string
	rawQuery    string
	contentType string
	body        string
}

func (r *recorder) handler(status int, contentType, response string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		body := new(strings.Builder)
		buf := make([]byte, 4096)
		for {
			n, err := req.Body.Read(buf)
			body.Write(buf[:n])
			if err != nil {
				break
			}
		}
		r.mu.Lock()
		r.method, r.path, r.rawQuery = req.Method, req.URL.Path, req.URL.RawQuery
		r.contentType, r.body = req.Header.Get("Content-Type"), body.String()
		r.mu.Unlock()

		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}
}

func newPassthroughClient(t *testing.T, baseSuffix string, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(Config{BaseURL: server.URL + baseSuffix})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestPassthroughForwardsRequestAndAnswer(t *testing.T) {
	rec := &recorder{}
	client := newPassthroughClient(t, "", rec.handler(200, "application/json; charset=UTF-8", `{"took":1}`))

	resp, err := client.Passthrough(context.Background(), "POST", "/logs-*/_search?size=1&pretty", []byte(`{"query":{"match_all":{}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 || resp.ContentType != "application/json; charset=UTF-8" || string(resp.Body) != `{"took":1}` {
		t.Fatalf("response = %+v", resp)
	}
	if rec.method != "POST" || rec.path != "/logs-*/_search" || rec.rawQuery != "size=1&pretty" ||
		rec.contentType != "application/json" || rec.body != `{"query":{"match_all":{}}}` {
		t.Fatalf("forwarded request = %+v", rec)
	}
}

func TestPassthroughReturnsErrorStatusesAsAnswers(t *testing.T) {
	rec := &recorder{}
	client := newPassthroughClient(t, "", rec.handler(404, "application/json", `{"error":{"type":"index_not_found_exception"},"status":404}`))

	resp, err := client.Passthrough(context.Background(), "GET", "missing/_search", nil)
	if err != nil {
		t.Fatalf("an error status from the cluster is an answer, not a failure: %v", err)
	}
	if resp.Status != 404 || !strings.Contains(string(resp.Body), "index_not_found_exception") {
		t.Fatalf("response = %+v", resp)
	}
}

func TestPassthroughUsesNDJSONForBulkEndpoints(t *testing.T) {
	for path, want := range map[string]string{
		"_bulk":              "application/x-ndjson",
		"logs/_bulk?refresh": "application/x-ndjson",
		"_msearch":           "application/x-ndjson",
		"logs/_search":       "application/json",
		"logs/_doc/_bulkish": "application/json",
		"my_bulk_index/_doc": "application/json",
		"_cluster/settings":  "application/json",
	} {
		rec := &recorder{}
		client := newPassthroughClient(t, "", rec.handler(200, "application/json", `{}`))
		if _, err := client.Passthrough(context.Background(), "POST", path, []byte("{}\n")); err != nil {
			t.Fatal(err)
		}
		if rec.contentType != want {
			t.Errorf("%s: Content-Type = %q, want %q", path, rec.contentType, want)
		}
	}
}

func TestPassthroughRejectsUnsafePaths(t *testing.T) {
	rec := &recorder{}
	client := newPassthroughClient(t, "/es", rec.handler(200, "application/json", `{}`))

	for _, path := range []string{
		"../_cluster/settings",
		"logs/../../admin",
		"logs/%2e%2e/admin",
		"logs/%2E%2E/admin",
		"./x",
		"http://evil.example/x",
		"logs//../x",
		"a b/_search",
		"logs\t/_search",
		"logs\\..\\x",
		"logs#frag",
		"logs/_search?q=a#frag",
		"logs/_search?q=a\nb",
		"logs/%zz",
		"logs/_search?x=%zz",
	} {
		if _, err := client.Passthrough(context.Background(), "GET", path, nil); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("path %q: error = %v, want ErrInvalidPath", path, err)
		}
	}
	if rec.path != "" {
		t.Errorf("an unsafe path reached the cluster: %q", rec.path)
	}
}

func TestPassthroughStaysBelowTheBasePath(t *testing.T) {
	rec := &recorder{}
	client := newPassthroughClient(t, "/es", rec.handler(200, "application/json", `{}`))
	if _, err := client.Passthrough(context.Background(), "GET", "_cat/indices", nil); err != nil {
		t.Fatal(err)
	}
	if rec.path != "/es/_cat/indices" {
		t.Fatalf("path = %q, want it under the base path", rec.path)
	}
}

func TestPassthroughRefusesOversizedAnswers(t *testing.T) {
	big := strings.Repeat("x", passthroughLimit+1)
	client := newPassthroughClient(t, "", (&recorder{}).handler(200, "application/json", big))
	if _, err := client.Passthrough(context.Background(), "GET", "_search", nil); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("error = %v, want ErrResponseTooLarge", err)
	}
}
