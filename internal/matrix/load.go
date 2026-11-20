package matrix

import (
	"context"
	"errors"
	"sync"

	"github.com/searchboard/searchboard/internal/domain"
	"github.com/searchboard/searchboard/internal/esclient"
)

// Snapshot is everything the Cluster Matrix page needs in one response.
type Snapshot struct {
	Version string               `json:"version"`
	Health  domain.ClusterHealth `json:"health"`
	Matrix
}

// Load fetches health, nodes and shards concurrently and builds a Snapshot.
// If any call fails the combined error is returned and no partial data is used,
// because a half-built matrix would misrepresent the cluster.
func Load(ctx context.Context, reader esclient.ClusterReader) (Snapshot, error) {
	version, err := reader.Version(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	var (
		health domain.ClusterHealth
		nodes  []domain.Node
		shards []domain.Shard

		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	run := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}

	run(func() (err error) { health, err = reader.Health(ctx); return })
	run(func() (err error) { nodes, err = reader.Nodes(ctx); return })
	run(func() (err error) { shards, err = reader.Shards(ctx); return })
	wg.Wait()

	if len(errs) > 0 {
		return Snapshot{}, errors.Join(errs...)
	}
	return Snapshot{
		Version: version.String(),
		Health:  health,
		Matrix:  Build(nodes, shards),
	}, nil
}
