package esclient

import (
	"context"

	"github.com/searchboard/searchboard/internal/domain"
)

// ClusterReader is the read-only, version-independent view of a cluster that the
// rest of the server depends on. *Client implements it.
type ClusterReader interface {
	Version(ctx context.Context) (Version, error)
	Health(ctx context.Context) (domain.ClusterHealth, error)
	Nodes(ctx context.Context) ([]domain.Node, error)
	Shards(ctx context.Context) ([]domain.Shard, error)
	AllocationExplain(ctx context.Context, req domain.AllocationExplainRequest) (domain.AllocationExplain, error)
}

var _ ClusterReader = (*Client)(nil)
