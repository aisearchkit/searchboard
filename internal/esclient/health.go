package esclient

import (
	"context"
	"net/http"

	"github.com/searchboard/searchboard/internal/domain"
)

// Health returns GET _cluster/health.
func (c *Client) Health(ctx context.Context) (domain.ClusterHealth, error) {
	var health domain.ClusterHealth
	if err := c.doJSON(ctx, http.MethodGet, "_cluster/health", nil, nil, &health); err != nil {
		return domain.ClusterHealth{}, err
	}
	return health, nil
}
