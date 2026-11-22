package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/searchboard/searchboard/internal/domain"
	"github.com/searchboard/searchboard/internal/esclient"
)

const validCluster = `{"name":"prod-logs","url":"https://es.example.com:9200/","username":"elastic","password":"hunter2","env":"prod"}`

func TestHealthz(t *testing.T) {
	rec := newTestEnv().do(t, http.MethodGet, "/api/v1/healthz", "")
	if rec.Status != http.StatusOK || !strings.Contains(rec.BodyString(), `"ok"`) {
		t.Fatalf("healthz = %d %s", rec.Status, rec.Body)
	}
}

func TestClusterLifecycle(t *testing.T) {
	env := newTestEnv()

	created := env.do(t, http.MethodPost, "/api/v1/clusters", validCluster)
	if created.Status != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Status, created.Body)
	}
	if strings.Contains(created.BodyString(), "hunter2") || strings.Contains(created.BodyString(), "password") {
		t.Fatalf("create response leaks the password: %s", created.Body)
	}

	var cluster clusterResponse
	if err := json.Unmarshal(created.Body, &cluster); err != nil {
		t.Fatal(err)
	}
	if cluster.URL != "https://es.example.com:9200" || cluster.Env != domain.EnvProd {
		t.Fatalf("response was not normalized: %+v", cluster)
	}
	if got := env.connector.connected[0].Password; got != "hunter2" {
		t.Fatalf("connector received password %q", got)
	}

	list := env.do(t, http.MethodGet, "/api/v1/clusters", "")
	if list.Status != http.StatusOK || strings.Contains(list.BodyString(), "hunter2") {
		t.Fatalf("list = %d %s", list.Status, list.Body)
	}
	var clusters []clusterResponse
	if err := json.Unmarshal(list.Body, &clusters); err != nil || len(clusters) != 1 {
		t.Fatalf("list decode = %v, %v", clusters, err)
	}

	deleted := env.do(t, http.MethodDelete, "/api/v1/clusters/"+cluster.ID, "")
	if deleted.Status != http.StatusNoContent {
		t.Fatalf("delete = %d", deleted.Status)
	}
	if len(env.connector.forgotten) != 1 || env.connector.forgotten[0] != cluster.ID {
		t.Fatalf("connector cache not evicted: %v", env.connector.forgotten)
	}
	if again := env.do(t, http.MethodDelete, "/api/v1/clusters/"+cluster.ID, ""); again.Status != http.StatusNotFound {
		t.Fatalf("second delete = %d, want 404", again.Status)
	}
}

func TestListClustersEncodesEmptyArray(t *testing.T) {
	rec := newTestEnv().do(t, http.MethodGet, "/api/v1/clusters", "")
	if strings.TrimSpace(rec.BodyString()) != "[]" {
		t.Fatalf("body = %q, want []", rec.Body)
	}
}

func TestCreateClusterValidation(t *testing.T) {
	tests := map[string]string{
		"malformed json": `{`,
		"unknown field":  `{"name":"a","url":"http://es:9200","bogus":1}`,
		"missing name":   `{"url":"http://es:9200"}`,
		"blank name":     `{"name":"   ","url":"http://es:9200"}`,
		"long name":      `{"name":"` + strings.Repeat("x", 65) + `","url":"http://es:9200"}`,
		"missing url":    `{"name":"a"}`,
		"relative url":   `{"name":"a","url":"es:9200"}`,
		"bad scheme":     `{"name":"a","url":"ftp://es:9200"}`,
		"bad env":        `{"name":"a","url":"http://es:9200","env":"qa"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv()
			if rec := env.do(t, http.MethodPost, "/api/v1/clusters", body); rec.Status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", rec.Status, rec.Body)
			}
			if len(env.connector.connected) != 0 {
				t.Error("invalid input must not reach the cluster")
			}
		})
	}
}

func TestCreateClusterDefaultsEnvToDev(t *testing.T) {
	env := newTestEnv()
	rec := env.do(t, http.MethodPost, "/api/v1/clusters", `{"name":"a","url":"http://es:9200"}`)
	if rec.Status != http.StatusCreated || !strings.Contains(rec.BodyString(), `"env":"dev"`) {
		t.Fatalf("create = %d %s", rec.Status, rec.Body)
	}
}

func TestCreateClusterRequiresReachableCluster(t *testing.T) {
	env := newTestEnv()
	env.reader.versionErr = errBoom

	rec := env.do(t, http.MethodPost, "/api/v1/clusters", validCluster)
	if rec.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", rec.Status, rec.Body)
	}
	if clusters, _ := env.repo.ListClusters(context.Background()); len(clusters) != 0 {
		t.Fatal("an unreachable cluster must not be stored")
	}
}

func TestCreateClusterRejectsDuplicateName(t *testing.T) {
	env := newTestEnv()
	env.addCluster(t, "prod-logs")
	if rec := env.do(t, http.MethodPost, "/api/v1/clusters", validCluster); rec.Status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Status)
	}
}

func TestMatrix(t *testing.T) {
	env := newTestEnv()
	id := env.addCluster(t, "demo")

	rec := env.do(t, http.MethodGet, "/api/v1/clusters/"+id+"/matrix", "")
	if rec.Status != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Status, rec.Body)
	}
	if strings.Contains(rec.BodyString(), "hunter2") {
		t.Fatalf("matrix response leaks the password: %s", rec.Body)
	}

	var body struct {
		Cluster clusterResponse `json:"cluster"`
		Version string          `json:"version"`
		Health  struct {
			Status string `json:"status"`
		} `json:"health"`
		Nodes   []struct{ ID string } `json:"nodes"`
		Indices []struct {
			Name       string                           `json:"name"`
			Health     string                           `json:"health"`
			Cells      map[string][]struct{ Shard int } `json:"cells"`
			Unassigned []struct {
				UnassignedReason string `json:"unassigned_reason"`
			} `json:"unassigned"`
		} `json:"indices"`
		GeneratedAt string `json:"generated_at"`
	}
	if err := json.Unmarshal(rec.Body, &body); err != nil {
		t.Fatal(err)
	}

	if body.Cluster.ID != id || body.Version != "elasticsearch 7.10.2" || body.Health.Status != "yellow" || body.GeneratedAt == "" {
		t.Fatalf("unexpected envelope: %+v", body)
	}
	if len(body.Nodes) != 1 || len(body.Indices) != 1 {
		t.Fatalf("unexpected grid: %+v", body)
	}
	index := body.Indices[0]
	if index.Name != "logs" || index.Health != "yellow" || len(index.Cells["n1"]) != 1 {
		t.Fatalf("unexpected index row: %+v", index)
	}
	if len(index.Unassigned) != 1 || index.Unassigned[0].UnassignedReason != "NODE_LEFT" {
		t.Fatalf("unexpected unassigned shards: %+v", index.Unassigned)
	}
}

func TestMatrixUnknownCluster(t *testing.T) {
	if rec := newTestEnv().do(t, http.MethodGet, "/api/v1/clusters/missing/matrix", ""); rec.Status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Status)
	}
}

func TestMatrixUpstreamFailureIsBadGateway(t *testing.T) {
	env := newTestEnv()
	id := env.addCluster(t, "demo")
	env.reader.healthErr = &esclient.APIError{Status: http.StatusUnauthorized, Body: `{"internal":"secret detail"}`}

	rec := env.do(t, http.MethodGet, "/api/v1/clusters/"+id+"/matrix", "")
	if rec.Status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Status)
	}
	if strings.Contains(rec.BodyString(), "secret detail") {
		t.Fatalf("upstream error body leaked: %s", rec.Body)
	}
}

func TestAllocationExplain(t *testing.T) {
	env := newTestEnv()
	id := env.addCluster(t, "demo")
	env.reader.explain = domain.AllocationExplain{Index: "logs", Shard: 0, CurrentState: "unassigned", CanAllocate: "no"}

	rec := env.do(t, http.MethodGet, "/api/v1/clusters/"+id+"/allocation-explain?index=logs&shard=0&primary=false", "")
	if rec.Status != http.StatusOK || !strings.Contains(rec.BodyString(), `"can_allocate":"no"`) {
		t.Fatalf("explain = %d %s", rec.Status, rec.Body)
	}
}

func TestAllocationExplainValidation(t *testing.T) {
	env := newTestEnv()
	id := env.addCluster(t, "demo")
	base := "/api/v1/clusters/" + id + "/allocation-explain"

	for name, query := range map[string]string{
		"missing index":  "?shard=0&primary=true",
		"bad shard":      "?index=logs&shard=x&primary=true",
		"negative shard": "?index=logs&shard=-1&primary=true",
		"bad primary":    "?index=logs&shard=0&primary=maybe",
	} {
		t.Run(name, func(t *testing.T) {
			if rec := env.do(t, http.MethodGet, base+query, ""); rec.Status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Status)
			}
		})
	}
}

func TestAllocationExplainOfAssignedShard(t *testing.T) {
	env := newTestEnv()
	id := env.addCluster(t, "demo")
	env.reader.explainErr = &esclient.APIError{Status: http.StatusBadRequest, Body: "unable to find any unassigned shards"}

	rec := env.do(t, http.MethodGet, "/api/v1/clusters/"+id+"/allocation-explain?index=logs&shard=0&primary=true", "")
	if rec.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Status)
	}
}
