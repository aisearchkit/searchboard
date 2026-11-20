package matrix

import (
	"context"
	"errors"
	"testing"

	"github.com/searchboard/searchboard/internal/domain"
	"github.com/searchboard/searchboard/internal/esclient"
)

func shard(index string, number int, primary bool, state domain.ShardState, nodeID, nodeName string) domain.Shard {
	return domain.Shard{Index: index, Number: number, Primary: primary, State: state, NodeID: nodeID, NodeName: nodeName}
}

func TestBuildGroupsShardsByIndexAndNode(t *testing.T) {
	nodes := []domain.Node{{ID: "n2", Name: "node-2"}, {ID: "n1", Name: "node-1"}}
	shards := []domain.Shard{
		shard("logs", 1, false, domain.ShardStarted, "n2", "node-2"),
		shard("logs", 0, true, domain.ShardStarted, "n1", "node-1"),
		shard("logs", 0, false, domain.ShardStarted, "n2", "node-2"),
		shard("audit", 0, true, domain.ShardStarted, "n1", "node-1"),
	}

	got := Build(nodes, shards)

	if names := []string{got.Nodes[0].Name, got.Nodes[1].Name}; names[0] != "node-1" || names[1] != "node-2" {
		t.Errorf("nodes not sorted by name: %v", names)
	}
	if got.Indices[0].Name != "audit" || got.Indices[1].Name != "logs" {
		t.Fatalf("indices not sorted: %s, %s", got.Indices[0].Name, got.Indices[1].Name)
	}

	logs := got.Indices[1]
	cell := logs.Cells["n2"]
	if len(cell) != 2 || cell[0].Shard != 0 || cell[1].Shard != 1 {
		t.Errorf("cell n2 not sorted by shard number: %+v", cell)
	}
	if len(logs.Cells["n1"]) != 1 || !logs.Cells["n1"][0].Primary {
		t.Errorf("cell n1 wrong: %+v", logs.Cells["n1"])
	}
	if logs.Health != domain.HealthGreen {
		t.Errorf("health = %s, want green", logs.Health)
	}
}

func TestBuildSeparatesUnassignedShards(t *testing.T) {
	shards := []domain.Shard{
		shard("logs", 0, true, domain.ShardStarted, "n1", "node-1"),
		{Index: "logs", Number: 0, State: domain.ShardUnassigned, UnassignedReason: "NODE_LEFT"},
	}
	got := Build([]domain.Node{{ID: "n1", Name: "node-1"}}, shards)

	row := got.Indices[0]
	if len(row.Unassigned) != 1 || row.Unassigned[0].UnassignedReason != "NODE_LEFT" {
		t.Fatalf("unassigned = %+v", row.Unassigned)
	}
	if len(row.Cells) != 1 {
		t.Errorf("unassigned shard must not appear in a node cell: %+v", row.Cells)
	}
	if row.Health != domain.HealthYellow {
		t.Errorf("missing replica must make the index yellow, got %s", row.Health)
	}
}

func TestIndexHealth(t *testing.T) {
	tests := []struct {
		name   string
		shards []domain.Shard
		want   domain.HealthStatus
	}{
		{"all started", []domain.Shard{
			shard("i", 0, true, domain.ShardStarted, "n1", "a"),
			shard("i", 0, false, domain.ShardStarted, "n2", "b"),
		}, domain.HealthGreen},
		{"unassigned replica", []domain.Shard{
			shard("i", 0, true, domain.ShardStarted, "n1", "a"),
			{Index: "i", Number: 0, State: domain.ShardUnassigned},
		}, domain.HealthYellow},
		{"unassigned primary", []domain.Shard{
			{Index: "i", Number: 0, Primary: true, State: domain.ShardUnassigned},
		}, domain.HealthRed},
		{"initializing primary", []domain.Shard{
			shard("i", 0, true, domain.ShardInitializing, "n1", "a"),
		}, domain.HealthRed},
		{"relocating primary keeps serving", []domain.Shard{
			shard("i", 0, true, domain.ShardRelocating, "n1", "a"),
		}, domain.HealthGreen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Build(nil, tt.shards).Indices[0].Health; got != tt.want {
				t.Fatalf("health = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestBuildKeepsRelocatingShardOnSourceNode(t *testing.T) {
	nodes := []domain.Node{{ID: "n1", Name: "node-1"}, {ID: "n2", Name: "node-2"}}
	moving := shard("logs", 0, true, domain.ShardRelocating, "n1", "node-1")
	moving.RelocatingToID, moving.RelocatingToName = "n2", "node-2"

	got := Build(nodes, []domain.Shard{moving})

	cells := got.Indices[0].Cells
	if len(cells["n1"]) != 1 || cells["n1"][0].RelocatingTo != "node-2" {
		t.Fatalf("relocating shard must stay on its source with a target label: %+v", cells)
	}
	if len(cells["n2"]) != 0 {
		t.Errorf("target cell must be empty: %+v", cells["n2"])
	}
}

func TestBuildAddsPlaceholderForUnknownNode(t *testing.T) {
	shards := []domain.Shard{shard("logs", 0, true, domain.ShardStarted, "gone", "node-gone")}
	got := Build([]domain.Node{{ID: "n1", Name: "node-1"}}, shards)

	if len(got.Nodes) != 2 {
		t.Fatalf("expected a placeholder column, got %+v", got.Nodes)
	}
	var found bool
	for _, node := range got.Nodes {
		found = found || (node.ID == "gone" && node.Name == "node-gone")
	}
	if !found {
		t.Errorf("placeholder for the unknown node is missing: %+v", got.Nodes)
	}
}

func TestBuildEmptyClusterHasNonNilSlices(t *testing.T) {
	got := Build(nil, nil)
	if got.Indices == nil || got.Nodes == nil {
		t.Fatalf("slices must be non-nil so they encode as []: %+v", got)
	}
}

// stubReader is a canned esclient.ClusterReader.
type stubReader struct {
	version esclient.Version
	health  domain.ClusterHealth
	nodes   []domain.Node
	shards  []domain.Shard
	err     error
}

func (s stubReader) Version(context.Context) (esclient.Version, error)    { return s.version, nil }
func (s stubReader) Health(context.Context) (domain.ClusterHealth, error) { return s.health, s.err }
func (s stubReader) Nodes(context.Context) ([]domain.Node, error)         { return s.nodes, nil }
func (s stubReader) Shards(context.Context) ([]domain.Shard, error)       { return s.shards, nil }
func (s stubReader) AllocationExplain(context.Context, domain.AllocationExplainRequest) (domain.AllocationExplain, error) {
	return domain.AllocationExplain{}, nil
}

func TestLoad(t *testing.T) {
	version, _ := esclient.ParseVersion(esclient.FlavorElasticsearch, "7.10.2")
	reader := stubReader{
		version: version,
		health:  domain.ClusterHealth{ClusterName: "prod", Status: domain.HealthGreen},
		nodes:   []domain.Node{{ID: "n1", Name: "node-1"}},
		shards:  []domain.Shard{shard("logs", 0, true, domain.ShardStarted, "n1", "node-1")},
	}

	snapshot, err := Load(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Version != "elasticsearch 7.10.2" || snapshot.Health.ClusterName != "prod" || len(snapshot.Indices) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestLoadFailsWhenAnyCallFails(t *testing.T) {
	boom := errors.New("boom")
	if _, err := Load(context.Background(), stubReader{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("Load error = %v, want it to wrap %v", err, boom)
	}
}
