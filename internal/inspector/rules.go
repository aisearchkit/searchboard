// Package inspector runs deterministic health rules, then (optionally) LLM attribution.
// AI never executes cluster-mutating actions silently; humans confirm.
package inspector

type Severity string

const (
	SevInfo     Severity = "info"
	SevWarning  Severity = "warning"
	SevCritical Severity = "critical"
)

// Snapshot is the compact structured input to rules (and later to the LLM).
type Snapshot struct {
	MaxShardSizeGB   float64
	ShardsPerNode    map[string]int
	HeapGBPerNode    map[string]float64
	MaxMappingFields int
	GCPausePct       float64
}

type Finding struct {
	Rule     string
	Severity Severity
	Detail   string
}

type Rule interface {
	Name() string
	Check(s Snapshot) []Finding
}

// TODO: 30+ rules. Thresholds from the design doc:
// shard > 50GB, shards/node > Heap(GB)*20, mapping fields near 1000, GC pause > 5%.
