package tsdb

import (
	"context"
	"errors"

	"github.com/searchboard/searchboard/internal/domain"
)

// Remote is the Phase 2 engine: Prometheus Remote Write + PromQL HTTP client
// (VictoriaMetrics). TODO: implement.
type Remote struct{ writeURL, queryURL string }

func NewRemote(writeURL, queryURL string) (*Remote, error) {
	if writeURL == "" || queryURL == "" {
		return nil, errors.New("tsdb.remote requires write_url and query_url")
	}
	return &Remote{writeURL: writeURL, queryURL: queryURL}, nil
}

func (r *Remote) WriteBatch(ctx context.Context, points []domain.MetricPoint) error {
	return errors.New("tsdb.Remote.WriteBatch: not implemented")
}

func (r *Remote) QueryRange(ctx context.Context, q domain.RangeQuery) ([]domain.TimeSeries, error) {
	return nil, errors.New("tsdb.Remote.QueryRange: not implemented")
}

func (r *Remote) RetentionGC(ctx context.Context) error { return nil } // handled by VM
func (r *Remote) Close() error                          { return nil }
