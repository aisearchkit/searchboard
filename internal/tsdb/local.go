package tsdb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/searchboard/searchboard/internal/domain"
)

// Local is the Phase 1 engine: append-only, day-partitioned directories
// (data/metrics/{cluster_id}/{YYYY-MM-DD}/) so retention is os.RemoveAll.
// TODO: WAL + Gorilla XOR blocks, or embed prometheus/tsdb.
type Local struct {
	dir           string
	retentionDays int
}

func NewLocal(dir string, retentionDays int) (*Local, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Local{dir: dir, retentionDays: retentionDays}, nil
}

func (l *Local) WriteBatch(ctx context.Context, points []domain.MetricPoint) error {
	return errors.New("tsdb.Local.WriteBatch: not implemented")
}

func (l *Local) QueryRange(ctx context.Context, q domain.RangeQuery) ([]domain.TimeSeries, error) {
	return nil, errors.New("tsdb.Local.QueryRange: not implemented")
}

// RetentionGC removes whole day-directories older than the retention window.
func (l *Local) RetentionGC(ctx context.Context) error {
	cutoff := time.Now().AddDate(0, 0, -l.retentionDays)
	clusters, err := os.ReadDir(l.dir)
	if err != nil {
		return err
	}
	for _, c := range clusters {
		if !c.IsDir() {
			continue
		}
		days, err := os.ReadDir(filepath.Join(l.dir, c.Name()))
		if err != nil {
			continue
		}
		for _, d := range days {
			t, err := time.Parse("2006-01-02", d.Name())
			if err == nil && t.Before(cutoff) {
				_ = os.RemoveAll(filepath.Join(l.dir, c.Name(), d.Name()))
			}
		}
	}
	return nil
}

func (l *Local) Close() error { return nil }
