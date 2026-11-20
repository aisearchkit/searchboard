// Package tsdb defines the MetricsStore abstraction. Business code never
// depends on a concrete engine; switch via config (tsdb.mode).
package tsdb

import (
	"context"
	"fmt"

	"github.com/searchboard/searchboard/internal/config"
	"github.com/searchboard/searchboard/internal/domain"
)

type MetricsStore interface {
	WriteBatch(ctx context.Context, points []domain.MetricPoint) error
	QueryRange(ctx context.Context, q domain.RangeQuery) ([]domain.TimeSeries, error)
	RetentionGC(ctx context.Context) error
	Close() error
}

// New builds the store selected by cfg.Mode.
func New(cfg config.TSDBConfig) (MetricsStore, error) {
	switch cfg.Mode {
	case "", "local":
		return NewLocal(cfg.Local.Dir, cfg.Local.RetentionDays)
	case "remote":
		return NewRemote(cfg.Remote.WriteURL, cfg.Remote.QueryURL)
	default:
		return nil, fmt.Errorf("unknown tsdb.mode %q", cfg.Mode)
	}
}
