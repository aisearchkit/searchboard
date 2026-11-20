package esclient

import (
	"context"
	"testing"

	"github.com/searchboard/searchboard/internal/domain"
)

func TestHealth(t *testing.T) {
	fake := newFakeCluster("", "7.17.0", map[string]string{
		"/_cluster/health": `{
		  "cluster_name":"prod","status":"yellow","number_of_nodes":3,"number_of_data_nodes":2,
		  "active_primary_shards":10,"active_shards":18,"relocating_shards":1,
		  "initializing_shards":2,"unassigned_shards":4
		}`,
	})
	health, err := fake.start(t, Config{}).Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := domain.ClusterHealth{
		ClusterName: "prod", Status: domain.HealthYellow, NumberOfNodes: 3, NumberOfDataNodes: 2,
		ActivePrimaryShards: 10, ActiveShards: 18, RelocatingShards: 1, InitializingShards: 2, UnassignedShards: 4,
	}
	if health != want {
		t.Fatalf("got %+v, want %+v", health, want)
	}
}

func TestAllocationExplainKeepsOnlyBlockingDeciders(t *testing.T) {
	fake := newFakeCluster("", "7.17.0", map[string]string{
		"/_cluster/allocation/explain": `{
		  "index":"logs","shard":2,"primary":false,"current_state":"unassigned",
		  "unassigned_info":{"reason":"NODE_LEFT"},
		  "can_allocate":"no",
		  "allocate_explanation":"cannot allocate because allocation is not permitted to any of the nodes",
		  "node_allocation_decisions":[{
		    "node_name":"node-1","node_decision":"no",
		    "deciders":[
		      {"decider":"same_shard","decision":"NO","explanation":"a copy of this shard is already allocated to this node"},
		      {"decider":"enable","decision":"YES","explanation":"all allocations are allowed"}
		    ]
		  }]
		}`,
	})
	client := fake.start(t, Config{})

	got, err := client.AllocationExplain(context.Background(), domain.AllocationExplainRequest{Index: "logs", Shard: 2})
	if err != nil {
		t.Fatal(err)
	}

	if got.UnassignedReason != "NODE_LEFT" || got.CanAllocate != "no" || got.CurrentState != "unassigned" {
		t.Errorf("unexpected summary: %+v", got)
	}
	if len(got.NodeDecisions) != 1 || len(got.NodeDecisions[0].Deciders) != 1 {
		t.Fatalf("expected one node with one blocking decider, got %+v", got.NodeDecisions)
	}
	if decider := got.NodeDecisions[0].Deciders[0]; decider.Name != "same_shard" || decider.Decision != "NO" {
		t.Errorf("unexpected decider: %+v", decider)
	}

	const wantBody = `{"index":"logs","shard":2,"primary":false}`
	if body := fake.body("/_cluster/allocation/explain"); body != wantBody {
		t.Errorf("request body = %s, want %s", body, wantBody)
	}
}

func TestAllocationExplainSurfacesClusterError(t *testing.T) {
	fake := newFakeCluster("", "7.17.0", map[string]string{
		"/_cluster/allocation/explain": `!400 {"error":"unable to find any unassigned shards to explain"}`,
	})
	_, err := fake.start(t, Config{}).AllocationExplain(context.Background(), domain.AllocationExplainRequest{Index: "logs"})
	if !HasStatus(err, 400) {
		t.Fatalf("expected an HTTP 400 APIError, got %v", err)
	}
}
