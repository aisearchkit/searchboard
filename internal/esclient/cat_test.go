package esclient

import (
	"context"
	"strings"
	"testing"

	"github.com/searchboard/searchboard/internal/domain"
)

const esNodes = `[
  {"id":"a1B2","name":"node-1","ip":"10.0.0.1","node.role":"dimr","master":"*","heap.percent":"41","cpu":"7","disk.used_percent":"63.20"},
  {"id":"c3D4","name":"node-2","ip":"10.0.0.2","node.role":"di","master":"-","heap.percent":"55","cpu":"12","disk.used_percent":null}
]`

const openSearchNodes = `[
  {"id":"x9","name":"os-1","ip":"10.1.0.1","node.role":"dimr","cluster_manager":"*","heap.percent":"30","cpu":"3","disk.used_percent":"20"}
]`

func TestNodesElasticsearch(t *testing.T) {
	fake := newFakeCluster("", "7.17.0", map[string]string{"/_cat/nodes": esNodes})
	client := fake.start(t, Config{})

	nodes, err := client.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}

	first := nodes[0]
	if first.ID != "a1B2" || first.Name != "node-1" || first.Roles != "dimr" || !first.IsMaster {
		t.Errorf("unexpected first node: %+v", first)
	}
	if first.HeapPct == nil || *first.HeapPct != 41 || first.DiskPct == nil || *first.DiskPct != 63.2 {
		t.Errorf("unexpected first node metrics: %+v", first)
	}

	second := nodes[1]
	if second.IsMaster {
		t.Error("second node must not be master")
	}
	if second.DiskPct != nil {
		t.Errorf("missing disk metric must be nil, got %v", *second.DiskPct)
	}

	columns := fake.query("/_cat/nodes").Get("h")
	if !strings.Contains(columns, "master") || strings.Contains(columns, "cluster_manager") {
		t.Errorf("columns = %q, want the legacy master column", columns)
	}
	if got := fake.query("/_cat/nodes").Get("format"); got != "json" {
		t.Errorf("format = %q, want json", got)
	}
}

func TestNodesOpenSearchUsesClusterManagerColumn(t *testing.T) {
	fake := newFakeCluster("opensearch", "2.11.1", map[string]string{"/_cat/nodes": openSearchNodes})
	client := fake.start(t, Config{})

	nodes, err := client.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || !nodes[0].IsMaster {
		t.Fatalf("unexpected nodes: %+v", nodes)
	}
	if columns := fake.query("/_cat/nodes").Get("h"); !strings.Contains(columns, "cluster_manager") {
		t.Errorf("columns = %q, want the cluster_manager column", columns)
	}
}

func TestNodeIDFallsBackToName(t *testing.T) {
	fake := newFakeCluster("", "5.6.16", map[string]string{
		"/_cat/nodes": `[{"name":"legacy","ip":"10.0.0.9","node.role":"mdi","master":"*"}]`,
	})
	nodes, err := fake.start(t, Config{}).Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if nodes[0].ID != "legacy" {
		t.Fatalf("ID = %q, want the node name as fallback", nodes[0].ID)
	}
}

const shardRows = `[
  {"index":"logs","shard":"0","prirep":"p","state":"STARTED","docs":"120","store":"2048","id":"a1B2","node":"node-1","unassigned.reason":null},
  {"index":"logs","shard":"1","prirep":"r","state":"RELOCATING","docs":"10","store":"512","id":"a1B2 -> c3D4","node":"node-1 -> 10.0.0.2 c3D4 node-2","unassigned.reason":null},
  {"index":"logs","shard":"2","prirep":"r","state":"UNASSIGNED","docs":null,"store":null,"id":null,"node":null,"unassigned.reason":"NODE_LEFT"},
  {"index":"logs","shard":"3","prirep":"p","state":"initializing","docs":null,"store":null,"id":"c3D4","node":"node-2","unassigned.reason":null}
]`

func TestShards(t *testing.T) {
	fake := newFakeCluster("", "7.17.0", map[string]string{"/_cat/shards": shardRows})
	shards, err := fake.start(t, Config{}).Shards(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(shards) != 4 {
		t.Fatalf("got %d shards, want 4", len(shards))
	}

	started := shards[0]
	if !started.Primary || started.State != domain.ShardStarted || started.NodeID != "a1B2" || started.NodeName != "node-1" {
		t.Errorf("unexpected started shard: %+v", started)
	}
	if started.Docs == nil || *started.Docs != 120 || started.StoreBytes == nil || *started.StoreBytes != 2048 {
		t.Errorf("unexpected started shard sizes: %+v", started)
	}

	relocating := shards[1]
	if relocating.Primary || relocating.State != domain.ShardRelocating {
		t.Errorf("unexpected relocating shard: %+v", relocating)
	}
	if relocating.NodeID != "a1B2" || relocating.NodeName != "node-1" ||
		relocating.RelocatingToID != "c3D4" || relocating.RelocatingToName != "node-2" {
		t.Errorf("relocation endpoints wrong: %+v", relocating)
	}

	unassigned := shards[2]
	if unassigned.State != domain.ShardUnassigned || unassigned.NodeID != "" || unassigned.NodeName != "" {
		t.Errorf("unexpected unassigned shard: %+v", unassigned)
	}
	if unassigned.UnassignedReason != "NODE_LEFT" || unassigned.Docs != nil || unassigned.StoreBytes != nil {
		t.Errorf("unexpected unassigned details: %+v", unassigned)
	}

	if shards[3].State != domain.ShardInitializing {
		t.Errorf("state must be normalized to upper case, got %q", shards[3].State)
	}

	if got := fake.query("/_cat/shards").Get("bytes"); got != "b" {
		t.Errorf("bytes = %q, want b", got)
	}
}

func TestShardsRejectsInvalidShardNumber(t *testing.T) {
	fake := newFakeCluster("", "7.17.0", map[string]string{
		"/_cat/shards": `[{"index":"logs","shard":"oops","prirep":"p","state":"STARTED"}]`,
	})
	if _, err := fake.start(t, Config{}).Shards(context.Background()); err == nil {
		t.Fatal("expected an error for a malformed shard number")
	}
}
