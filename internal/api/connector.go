package api

import (
	"sync"

	"github.com/searchboard/searchboard/internal/domain"
	"github.com/searchboard/searchboard/internal/esclient"
)

// Connector hands out cluster clients. Implementations may cache them.
type Connector interface {
	// Connect returns a reader for the cluster described by c.
	Connect(c domain.Cluster) (esclient.ClusterReader, error)
	// Forget drops anything cached for the cluster ID, for example after deletion.
	Forget(id string)
}

// ClientCache is the production Connector. It reuses one esclient.Client per
// cluster so HTTP connections and the detected version are shared across
// requests, and rebuilds the client when the stored connection settings change.
type ClientCache struct {
	mu      sync.Mutex
	clients map[string]cachedClient
}

type cachedClient struct {
	config esclient.Config
	client *esclient.Client
}

// NewClientCache returns an empty cache.
func NewClientCache() *ClientCache {
	return &ClientCache{clients: map[string]cachedClient{}}
}

// Connect implements Connector.
func (c *ClientCache) Connect(cluster domain.Cluster) (esclient.ClusterReader, error) {
	config := esclient.Config{
		BaseURL:            cluster.URL,
		Username:           cluster.Username,
		Password:           cluster.Password,
		InsecureSkipVerify: cluster.InsecureSkipVerify,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if cached, ok := c.clients[cluster.ID]; ok && cached.config == config {
		return cached.client, nil
	}
	client, err := esclient.New(config)
	if err != nil {
		return nil, err
	}
	c.clients[cluster.ID] = cachedClient{config: config, client: client}
	return client, nil
}

// Forget implements Connector.
func (c *ClientCache) Forget(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.clients, id)
}
