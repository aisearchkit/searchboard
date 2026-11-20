package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/searchboard/searchboard/internal/domain"
	"github.com/searchboard/searchboard/internal/esclient"
)

// memoryRepo is an in-memory ClusterRepository.
type memoryRepo struct {
	mu       sync.Mutex
	next     int
	clusters map[string]domain.Cluster
}

func newMemoryRepo() *memoryRepo { return &memoryRepo{clusters: map[string]domain.Cluster{}} }

func (m *memoryRepo) ListClusters(context.Context) ([]domain.Cluster, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]domain.Cluster, 0, len(m.clusters))
	for _, c := range m.clusters {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

func (m *memoryRepo) GetCluster(_ context.Context, id string) (domain.Cluster, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.clusters[id]
	if !ok {
		return domain.Cluster{}, domain.ErrNotFound
	}
	return c, nil
}

func (m *memoryRepo) CreateCluster(_ context.Context, c domain.Cluster) (domain.Cluster, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.clusters {
		if existing.Name == c.Name {
			return domain.Cluster{}, domain.ErrConflict
		}
	}
	m.next++
	c.ID = "c" + string(rune('0'+m.next))
	m.clusters[c.ID] = c
	return c, nil
}

func (m *memoryRepo) DeleteCluster(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.clusters[id]; !ok {
		return domain.ErrNotFound
	}
	delete(m.clusters, id)
	return nil
}

// fakeReader is a canned esclient.ClusterReader.
type fakeReader struct {
	versionErr error
	healthErr  error
	explain    domain.AllocationExplain
	explainErr error
}

func (f *fakeReader) Version(context.Context) (esclient.Version, error) {
	if f.versionErr != nil {
		return esclient.Version{}, f.versionErr
	}
	return esclient.ParseVersion(esclient.FlavorElasticsearch, "7.10.2")
}

func (f *fakeReader) Health(context.Context) (domain.ClusterHealth, error) {
	return domain.ClusterHealth{ClusterName: "demo", Status: domain.HealthYellow, UnassignedShards: 1}, f.healthErr
}

func (f *fakeReader) Nodes(context.Context) ([]domain.Node, error) {
	heap := 40.0
	return []domain.Node{{ID: "n1", Name: "node-1", Roles: "dim", IsMaster: true, HeapPct: &heap}}, nil
}

func (f *fakeReader) Shards(context.Context) ([]domain.Shard, error) {
	return []domain.Shard{
		{Index: "logs", Number: 0, Primary: true, State: domain.ShardStarted, NodeID: "n1", NodeName: "node-1"},
		{Index: "logs", Number: 0, State: domain.ShardUnassigned, UnassignedReason: "NODE_LEFT"},
	}, nil
}

func (f *fakeReader) AllocationExplain(context.Context, domain.AllocationExplainRequest) (domain.AllocationExplain, error) {
	return f.explain, f.explainErr
}

// fakeConnector returns the same reader for every cluster and records usage.
type fakeConnector struct {
	reader    *fakeReader
	connected []domain.Cluster
	forgotten []string
}

func (f *fakeConnector) Connect(c domain.Cluster) (esclient.ClusterReader, error) {
	f.connected = append(f.connected, c)
	return f.reader, nil
}

func (f *fakeConnector) Forget(id string) { f.forgotten = append(f.forgotten, id) }

// testEnv wires a router against fakes.
type testEnv struct {
	router    http.Handler
	repo      *memoryRepo
	connector *fakeConnector
	reader    *fakeReader
}

func newTestEnv() *testEnv {
	reader := &fakeReader{}
	connector := &fakeConnector{reader: reader}
	repo := newMemoryRepo()
	router := NewRouter(Deps{
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clusters:  repo,
		Connector: connector,
	})
	return &testEnv{router: router, repo: repo, connector: connector, reader: reader}
}

func (e *testEnv) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	recorder := httptest.NewRecorder()
	e.router.ServeHTTP(recorder, req)
	return recorder
}

// addCluster registers a cluster directly in the repository and returns its ID.
func (e *testEnv) addCluster(t *testing.T, name string) string {
	t.Helper()
	created, err := e.repo.CreateCluster(context.Background(), domain.Cluster{
		Name: name, URL: "http://es:9200", Env: domain.EnvDev, Password: "hunter2",
	})
	if err != nil {
		t.Fatal(err)
	}
	return created.ID
}

var errBoom = errors.New("boom")
