package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/searchboard/searchboard/internal/domain"
	"github.com/searchboard/searchboard/internal/esclient"
)

// addClusterIn registers a cluster in the given environment.
func (e *testEnv) addClusterIn(t *testing.T, name string, env domain.Environment) string {
	t.Helper()
	created, err := e.repo.CreateCluster(context.Background(), domain.Cluster{Name: name, URL: "http://es:9200", Env: env, Password: "hunter2"})
	if err != nil {
		t.Fatal(err)
	}
	return created.ID
}

func proxyBody(method, path, body string, confirm bool) string {
	encoded, _ := json.Marshal(map[string]any{"method": method, "path": path, "body": body, "confirm": confirm})
	return string(encoded)
}

type proxyResult struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        string `json:"body"`
	Risk        string `json:"risk"`
	DurationMS  int64  `json:"duration_ms"`
}

func TestProxyForwardsASafeRequest(t *testing.T) {
	env := newTestEnv()
	id := env.addClusterIn(t, "demo", domain.EnvProd)
	env.reader.proxyAnswer = esclient.RawResponse{Status: 200, ContentType: "application/json", Body: []byte(`{"hits":{"total":3}}`)}

	got := env.do(t, "POST", "/api/v1/clusters/"+id+"/proxy", proxyBody("post", " /logs/_search?size=1 ", `{"query":{"match_all":{}}}`, false))
	if got.Status != 200 {
		t.Fatalf("status = %d %s", got.Status, got.Body)
	}
	var result proxyResult
	if err := json.Unmarshal(got.Body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != 200 || result.Body != `{"hits":{"total":3}}` || result.ContentType != "application/json" || result.Risk != "safe" {
		t.Fatalf("result = %+v", result)
	}
	want := proxiedCall{Method: "POST", Path: "/logs/_search?size=1", Body: `{"query":{"match_all":{}}}`}
	if len(env.reader.proxiedCalls) != 1 || env.reader.proxiedCalls[0] != want {
		t.Fatalf("forwarded = %+v, want %+v", env.reader.proxiedCalls, want)
	}
	if strings.Contains(got.BodyString(), "hunter2") {
		t.Fatal("the response must never contain cluster credentials")
	}
}

func TestProxyShowsClusterErrorsAsAnswers(t *testing.T) {
	env := newTestEnv()
	id := env.addClusterIn(t, "demo", domain.EnvDev)
	env.reader.proxyAnswer = esclient.RawResponse{Status: 404, ContentType: "application/json", Body: []byte(`{"error":{"type":"index_not_found_exception"}}`)}

	got := env.do(t, "POST", "/api/v1/clusters/"+id+"/proxy", proxyBody("GET", "missing/_search", "", false))
	var result proxyResult
	_ = json.Unmarshal(got.Body, &result)
	if got.Status != 200 || result.Status != 404 || !strings.Contains(result.Body, "index_not_found_exception") {
		t.Fatalf("a 404 from the cluster is an answer to display: http %d, %+v", got.Status, result)
	}
}

func TestConfirmationPolicy(t *testing.T) {
	tests := []struct {
		name         string
		env          domain.Environment
		method, path string
		needsConfirm bool
		risk         string
	}{
		{"read on prod", domain.EnvProd, "GET", "_cluster/health", false, "safe"},
		{"read via POST on prod", domain.EnvProd, "POST", "logs/_search", false, "safe"},
		{"write on prod", domain.EnvProd, "PUT", "logs/_doc/1", true, "write"},
		{"write on staging", domain.EnvStaging, "PUT", "logs/_doc/1", false, "write"},
		{"write on dev", domain.EnvDev, "PUT", "logs/_doc/1", false, "write"},
		{"dangerous on prod", domain.EnvProd, "DELETE", "logs", true, "dangerous"},
		{"dangerous on staging", domain.EnvStaging, "DELETE", "logs", true, "dangerous"},
		{"dangerous on dev", domain.EnvDev, "POST", "logs/_delete_by_query", true, "dangerous"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv()
			id := env.addClusterIn(t, "demo", tt.env)
			url := "/api/v1/clusters/" + id + "/proxy"

			first := env.do(t, "POST", url, proxyBody(tt.method, tt.path, "", false))
			if !tt.needsConfirm {
				if first.Status != 200 || len(env.reader.proxiedCalls) != 1 {
					t.Fatalf("expected the request to run: %d %s", first.Status, first.Body)
				}
				return
			}

			var held struct {
				Error, Risk, Reason string
			}
			_ = json.Unmarshal(first.Body, &held)
			if first.Status != http.StatusPreconditionRequired || held.Risk != tt.risk || held.Reason == "" {
				t.Fatalf("expected 428 with the risk, got %d %s", first.Status, first.Body)
			}
			if len(env.reader.proxiedCalls) != 0 {
				t.Fatal("an unconfirmed risky request must not reach the cluster")
			}
			if len(env.history.entries) != 0 {
				t.Fatal("a held request must not be recorded as executed")
			}

			confirmed := env.do(t, "POST", url, proxyBody(tt.method, tt.path, "", true))
			if confirmed.Status != 200 || len(env.reader.proxiedCalls) != 1 {
				t.Fatalf("a confirmed request must run: %d %s", confirmed.Status, confirmed.Body)
			}
		})
	}
}

func TestProxyValidation(t *testing.T) {
	env := newTestEnv()
	id := env.addClusterIn(t, "demo", domain.EnvDev)
	url := "/api/v1/clusters/" + id + "/proxy"

	tests := map[string]string{
		"malformed json": `{`,
		"unknown field":  `{"method":"GET","path":"_cat","extra":1}`,
		"bad method":     proxyBody("TRACE", "_cat", "", false),
		"connect method": proxyBody("CONNECT", "x", "", false),
		"empty method":   proxyBody("", "_cat", "", false),
		"empty path":     proxyBody("GET", "   ", "", false),
		"overlong path":  proxyBody("GET", strings.Repeat("a", maxProxyPathLen+1), "", false),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if got := env.do(t, "POST", url, body); got.Status != 400 {
				t.Fatalf("status = %d %s", got.Status, got.Body)
			}
		})
	}
	if len(env.reader.proxiedCalls) != 0 {
		t.Fatal("invalid requests must never reach the cluster")
	}

	if got := env.do(t, "POST", "/api/v1/clusters/missing/proxy", proxyBody("GET", "_cat", "", false)); got.Status != 404 {
		t.Errorf("unknown cluster = %d", got.Status)
	}
}

func TestProxyMapsClientErrors(t *testing.T) {
	env := newTestEnv()
	id := env.addClusterIn(t, "demo", domain.EnvDev)
	url := "/api/v1/clusters/" + id + "/proxy"

	env.reader.proxyErr = esclient.ErrInvalidPath
	if got := env.do(t, "POST", url, proxyBody("GET", "../x", "", false)); got.Status != 400 {
		t.Errorf("unsafe path = %d", got.Status)
	}

	env.reader.proxyErr = esclient.ErrResponseTooLarge
	if got := env.do(t, "POST", url, proxyBody("GET", "_search", "", false)); got.Status != 422 || !strings.Contains(got.BodyString(), "16 MiB") {
		t.Errorf("oversized answer = %d %s", got.Status, got.Body)
	}

	env.reader.proxyErr = errors.New("dial tcp 10.0.0.5:9200: connection refused")
	got := env.do(t, "POST", url, proxyBody("GET", "_search", "", false))
	if got.Status != 502 || strings.Contains(got.BodyString(), "10.0.0.5") {
		t.Errorf("unreachable cluster = %d %s (internal addresses must not leak)", got.Status, got.Body)
	}
	if len(env.history.entries) != 0 {
		t.Error("failed requests are not history")
	}
}

func TestHistoryRecordsExecutedRequests(t *testing.T) {
	env := newTestEnv()
	id := env.addClusterIn(t, "demo", domain.EnvDev)
	other := env.addClusterIn(t, "other", domain.EnvDev)

	env.do(t, "POST", "/api/v1/clusters/"+id+"/proxy", proxyBody("GET", "_cat/indices", "", false))
	env.do(t, "POST", "/api/v1/clusters/"+id+"/proxy", proxyBody("POST", "logs/_search", `{"size":1}`, false))
	env.do(t, "POST", "/api/v1/clusters/"+other+"/proxy", proxyBody("GET", "_cluster/health", "", false))

	got := env.do(t, "GET", "/api/v1/clusters/"+id+"/devtools/history", "")
	var entries []historyResponse
	if err := json.Unmarshal(got.Body, &entries); err != nil || got.Status != 200 {
		t.Fatalf("history = %d %s", got.Status, got.Body)
	}
	if len(entries) != 2 || entries[0].Request != "POST /logs/_search\n{\"size\":1}" || entries[1].Request != "GET /_cat/indices" {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Status != 200 || entries[0].Risk != "safe" || entries[0].Method != "POST" {
		t.Errorf("entry fields = %+v", entries[0])
	}

	search := env.do(t, "GET", "/api/v1/clusters/"+id+"/devtools/history?q=indices", "")
	_ = json.Unmarshal(search.Body, &entries)
	if len(entries) != 1 || !strings.Contains(entries[0].Request, "indices") {
		t.Errorf("search = %+v", entries)
	}
	limited := env.do(t, "GET", "/api/v1/clusters/"+id+"/devtools/history?limit=1", "")
	_ = json.Unmarshal(limited.Body, &entries)
	if len(entries) != 1 {
		t.Errorf("limit = %d entries", len(entries))
	}

	for _, bad := range []string{"limit=0", "limit=-3", "limit=abc"} {
		if got := env.do(t, "GET", "/api/v1/clusters/"+id+"/devtools/history?"+bad, ""); got.Status != 400 {
			t.Errorf("%s = %d", bad, got.Status)
		}
	}
	if got := env.do(t, "GET", "/api/v1/clusters/missing/devtools/history", ""); got.Status != 404 {
		t.Errorf("unknown cluster = %d", got.Status)
	}
}

func TestClearHistory(t *testing.T) {
	env := newTestEnv()
	id := env.addClusterIn(t, "demo", domain.EnvDev)
	other := env.addClusterIn(t, "other", domain.EnvDev)
	env.do(t, "POST", "/api/v1/clusters/"+id+"/proxy", proxyBody("GET", "_cat", "", false))
	env.do(t, "POST", "/api/v1/clusters/"+other+"/proxy", proxyBody("GET", "_cat", "", false))

	if got := env.do(t, "DELETE", "/api/v1/clusters/"+id+"/devtools/history", ""); got.Status != 204 {
		t.Fatalf("clear = %d", got.Status)
	}
	if got := env.do(t, "GET", "/api/v1/clusters/"+id+"/devtools/history", ""); strings.TrimSpace(got.BodyString()) != "[]" {
		t.Errorf("history after clear = %s", got.Body)
	}
	var kept []historyResponse
	_ = json.Unmarshal(env.do(t, "GET", "/api/v1/clusters/"+other+"/devtools/history", "").Body, &kept)
	if len(kept) != 1 {
		t.Errorf("clearing one cluster must not clear another, other has %d entries", len(kept))
	}
	if got := env.do(t, "DELETE", "/api/v1/clusters/missing/devtools/history", ""); got.Status != 404 {
		t.Errorf("unknown cluster = %d", got.Status)
	}
}

func TestAHistoryFailureDoesNotFailTheRequest(t *testing.T) {
	env := newTestEnv()
	id := env.addClusterIn(t, "demo", domain.EnvDev)
	env.history.addErr = errors.New("disk full")

	got := env.do(t, "POST", "/api/v1/clusters/"+id+"/proxy", proxyBody("GET", "_cat", "", false))
	if got.Status != 200 {
		t.Fatalf("the user already ran the request; a recording failure must not hide the answer: %d %s", got.Status, got.Body)
	}
}

func TestSchemaIsCachedBrieflyAndForgottenOnDelete(t *testing.T) {
	env := newTestEnv()
	id := env.addClusterIn(t, "demo", domain.EnvDev)
	env.reader.schema = domain.Schema{Indices: []domain.SchemaIndex{{Name: "logs", Aliases: []string{"l"}, Fields: []domain.SchemaField{{Path: "message", Type: "text"}}}}}
	url := "/api/v1/clusters/" + id + "/schema"

	first := env.do(t, "GET", url, "")
	if first.Status != 200 || !strings.Contains(first.BodyString(), `"message"`) || !strings.Contains(first.BodyString(), `"truncated":false`) {
		t.Fatalf("schema = %d %s", first.Status, first.Body)
	}
	env.do(t, "GET", url, "")
	if env.reader.schemaCalls != 1 {
		t.Fatalf("the cluster was asked %d times, want 1 within the TTL", env.reader.schemaCalls)
	}

	time.Sleep(250 * time.Millisecond) // the test environment uses a 150ms TTL
	env.do(t, "GET", url, "")
	if env.reader.schemaCalls != 2 {
		t.Fatalf("an expired schema must be fetched again, calls = %d", env.reader.schemaCalls)
	}

	env.do(t, "DELETE", "/api/v1/clusters/"+id, "")
	if got := env.do(t, "GET", url, ""); got.Status != 404 {
		t.Errorf("schema of a deleted cluster = %d", got.Status)
	}
}

func TestSchemaErrors(t *testing.T) {
	env := newTestEnv()
	id := env.addClusterIn(t, "demo", domain.EnvDev)
	env.reader.schemaErr = &esclient.APIError{Status: 403, Body: "secret detail"}

	got := env.do(t, "GET", "/api/v1/clusters/"+id+"/schema", "")
	if got.Status != 502 || strings.Contains(got.BodyString(), "secret detail") {
		t.Fatalf("schema failure = %d %s", got.Status, got.Body)
	}
	if got := env.do(t, "GET", "/api/v1/clusters/missing/schema", ""); got.Status != 404 {
		t.Errorf("unknown cluster = %d", got.Status)
	}
}
