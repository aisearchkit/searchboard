package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/framework/ut"
	"github.com/searchboard/searchboard/internal/domain"
	"github.com/searchboard/searchboard/internal/esclient"
)

// memoryHistory is an in-memory HistoryRepository.
type memoryHistory struct {
	mu      sync.Mutex
	entries []domain.HistoryEntry
	addErr  error
}

func (m *memoryHistory) AddHistory(_ context.Context, e domain.HistoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.addErr != nil {
		return m.addErr
	}
	e.ID = int64(len(m.entries) + 1)
	m.entries = append(m.entries, e)
	return nil
}

func (m *memoryHistory) ListHistory(_ context.Context, clusterID, query string, limit int) ([]domain.HistoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []domain.HistoryEntry{}
	for i := len(m.entries) - 1; i >= 0 && len(out) < limit; i-- {
		e := m.entries[i]
		if e.ClusterID == clusterID && (query == "" || strings.Contains(strings.ToLower(e.Request), strings.ToLower(query))) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *memoryHistory) ClearHistory(_ context.Context, clusterID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.entries[:0]
	var removed int64
	for _, e := range m.entries {
		if e.ClusterID == clusterID {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	m.entries = kept
	return removed, nil
}

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

// proxiedCall records one request that reached the fake cluster.
type proxiedCall struct {
	Method, Path, Body string
}

// fakeReader is a canned esclient.Cluster.
type fakeReader struct {
	versionErr error
	healthErr  error
	explain    domain.AllocationExplain
	explainErr error

	schema       domain.Schema
	schemaErr    error
	schemaCalls  int
	proxyAnswer  esclient.RawResponse
	proxyErr     error
	proxiedCalls []proxiedCall
}

func (f *fakeReader) Passthrough(_ context.Context, method, path string, body []byte) (esclient.RawResponse, error) {
	f.proxiedCalls = append(f.proxiedCalls, proxiedCall{Method: method, Path: path, Body: string(body)})
	if f.proxyErr != nil {
		return esclient.RawResponse{}, f.proxyErr
	}
	if f.proxyAnswer.Status == 0 {
		return esclient.RawResponse{Status: 200, ContentType: "application/json", Body: []byte(`{"acknowledged":true}`)}, nil
	}
	return f.proxyAnswer, nil
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

func (f *fakeReader) Schema(context.Context) (domain.Schema, error) {
	f.schemaCalls++
	return f.schema, f.schemaErr
}

func (f *fakeReader) AllocationExplain(context.Context, domain.AllocationExplainRequest) (domain.AllocationExplain, error) {
	return f.explain, f.explainErr
}

// fakeConnector returns the same reader for every cluster and records usage.
type fakeConnector struct {
	reader         *fakeReader
	connected      []domain.Cluster
	forgotten      []string
	panicOnConnect bool
}

func (f *fakeConnector) Connect(c domain.Cluster) (esclient.Cluster, error) {
	if f.panicOnConnect {
		panic("connector exploded")
	}
	f.connected = append(f.connected, c)
	return f.reader, nil
}

func (f *fakeConnector) Forget(id string) { f.forgotten = append(f.forgotten, id) }

// testEnv wires the real server against fakes.
type testEnv struct {
	server    *framework.Server
	repo      *memoryRepo
	history   *memoryHistory
	connector *fakeConnector
	reader    *fakeReader
}

func newTestEnv() *testEnv {
	reader := &fakeReader{}
	connector := &fakeConnector{reader: reader}
	repo := newMemoryRepo()
	history := &memoryHistory{}
	server := New(Deps{
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clusters:  repo,
		History:   history,
		Connector: connector,
		SchemaTTL: 150 * time.Millisecond,
	})
	return &testEnv{server: server, repo: repo, history: history, connector: connector, reader: reader}
}

// do sends a request through routing and middleware without a network.
func (e *testEnv) do(t *testing.T, method, path, body string) *ut.Result {
	t.Helper()
	var payload []byte
	headers := []ut.Header{}
	if body != "" {
		payload = []byte(body)
		headers = append(headers, ut.Header{Key: "Content-Type", Value: "application/json"})
	}
	return ut.PerformRequest(e.server, method, path, payload, headers...)
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
