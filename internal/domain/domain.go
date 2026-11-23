// Package domain holds pure domain models with no infrastructure dependencies.
package domain

import "time"

// Environment labels a registered cluster. The UI uses it for guard rails,
// for example the red frame around production clusters.
type Environment string

const (
	EnvProd    Environment = "prod"
	EnvStaging Environment = "staging"
	EnvDev     Environment = "dev"
)

// Valid reports whether e is a known environment.
func (e Environment) Valid() bool {
	return e == EnvProd || e == EnvStaging || e == EnvDev
}

// Cluster is a registered Elasticsearch / OpenSearch cluster.
type Cluster struct {
	ID                 string
	Name               string
	URL                string
	Username           string
	Password           string // never serialized to API responses
	Env                Environment
	InsecureSkipVerify bool
	CreatedAt          time.Time
}

type MetricPoint struct {
	ClusterID string
	Name      string
	Labels    map[string]string
	Timestamp time.Time
	Value     float64
}

type RangeQuery struct {
	ClusterID string
	Name      string
	Labels    map[string]string
	Start     time.Time
	End       time.Time
	Step      time.Duration
}

type Sample struct {
	Timestamp time.Time
	Value     float64
}

type TimeSeries struct {
	Name    string
	Labels  map[string]string
	Samples []Sample
}

type LogEvent struct {
	ClusterID string
	NodeID    string
	Timestamp time.Time
	Level     string
	Message   string // multi-line stack traces already merged by the agent
	Template  string // filled by the Drain clustering stage
}

type MigrationStatus string

const (
	MigrationPending MigrationStatus = "pending"
	MigrationRunning MigrationStatus = "running"
	MigrationPaused  MigrationStatus = "paused"
	MigrationDone    MigrationStatus = "done"
	MigrationFailed  MigrationStatus = "failed"
)

type MigrationTask struct {
	ID        string
	SourceID  string
	TargetID  string
	Indices   []string
	Status    MigrationStatus
	CreatedAt time.Time
}

// HistoryEntry is one request executed from the DevTools console.
type HistoryEntry struct {
	ID         int64
	ClusterID  string
	Method     string
	Path       string
	Request    string // the console text: "METHOD /path" and the body, if any
	Status     int    // HTTP status the cluster answered with
	Risk       string // "safe", "write" or "dangerous"
	DurationMS int64
	CreatedAt  time.Time
}
