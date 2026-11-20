package domain

// HealthStatus is the cluster-level health color reported by Elasticsearch / OpenSearch.
type HealthStatus string

const (
	HealthGreen  HealthStatus = "green"
	HealthYellow HealthStatus = "yellow"
	HealthRed    HealthStatus = "red"
)

// ClusterHealth is a trimmed view of GET _cluster/health.
type ClusterHealth struct {
	ClusterName         string       `json:"cluster_name"`
	Status              HealthStatus `json:"status"`
	NumberOfNodes       int          `json:"number_of_nodes"`
	NumberOfDataNodes   int          `json:"number_of_data_nodes"`
	ActivePrimaryShards int          `json:"active_primary_shards"`
	ActiveShards        int          `json:"active_shards"`
	RelocatingShards    int          `json:"relocating_shards"`
	InitializingShards  int          `json:"initializing_shards"`
	UnassignedShards    int          `json:"unassigned_shards"`
}

// Node is a cluster node as listed by _cat/nodes. Resource percentages are nil
// when the node did not report them.
type Node struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	IP       string   `json:"ip"`
	Roles    string   `json:"roles"`
	IsMaster bool     `json:"is_master"`
	HeapPct  *float64 `json:"heap_pct,omitempty"`
	CPUPct   *float64 `json:"cpu_pct,omitempty"`
	DiskPct  *float64 `json:"disk_pct,omitempty"`
}

// ShardState is the lifecycle state of a shard copy.
type ShardState string

const (
	ShardStarted      ShardState = "STARTED"
	ShardInitializing ShardState = "INITIALIZING"
	ShardRelocating   ShardState = "RELOCATING"
	ShardUnassigned   ShardState = "UNASSIGNED"
)

// Shard is one copy (primary or replica) of an index shard.
//
// A relocating shard is reported on its source node; RelocatingTo* identify the
// target. An unassigned shard has an empty NodeID and NodeName.
type Shard struct {
	Index            string     `json:"index"`
	Number           int        `json:"number"`
	Primary          bool       `json:"primary"`
	State            ShardState `json:"state"`
	Docs             *int64     `json:"docs,omitempty"`
	StoreBytes       *int64     `json:"store_bytes,omitempty"`
	NodeID           string     `json:"node_id"`
	NodeName         string     `json:"node_name"`
	RelocatingToID   string     `json:"relocating_to_id,omitempty"`
	RelocatingToName string     `json:"relocating_to_name,omitempty"`
	UnassignedReason string     `json:"unassigned_reason,omitempty"`
}

// AllocationExplainRequest identifies the shard copy to explain.
type AllocationExplainRequest struct {
	Index   string `json:"index"`
	Shard   int    `json:"shard"`
	Primary bool   `json:"primary"`
}

// AllocationExplain is a summary of POST _cluster/allocation/explain.
type AllocationExplain struct {
	Index            string                   `json:"index"`
	Shard            int                      `json:"shard"`
	Primary          bool                     `json:"primary"`
	CurrentState     string                   `json:"current_state"`
	UnassignedReason string                   `json:"unassigned_reason,omitempty"`
	CanAllocate      string                   `json:"can_allocate,omitempty"`
	Explanation      string                   `json:"explanation,omitempty"`
	NodeDecisions    []AllocationNodeDecision `json:"node_decisions,omitempty"`
}

// AllocationNodeDecision is the allocation verdict for one node.
type AllocationNodeDecision struct {
	NodeName string              `json:"node_name"`
	Decision string              `json:"decision"`
	Deciders []AllocationDecider `json:"deciders,omitempty"`
}

// AllocationDecider is a single decider that vetoed or throttled allocation.
type AllocationDecider struct {
	Name        string `json:"name"`
	Decision    string `json:"decision"`
	Explanation string `json:"explanation"`
}
