// Package domain holds pure domain models with no infrastructure dependencies.
package domain

import "time"

type Cluster struct {
	ID       string
	Name     string
	URL      string
	Username string
	Env      string // prod | staging | dev ; "prod" enables UI guard rails
	Version  string
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
