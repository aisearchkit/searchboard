package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/internal/domain"
	"github.com/searchboard/searchboard/internal/esclient"
	"github.com/searchboard/searchboard/internal/matrix"
)

// clusterCallTimeout bounds how long one API request may wait on a cluster.
const clusterCallTimeout = 20 * time.Second

type matrixResponse struct {
	Cluster clusterResponse `json:"cluster"`
	matrix.Snapshot
	GeneratedAt time.Time `json:"generated_at"`
}

// readerFor loads the cluster named in the URL and opens a client for it, with
// a timeout for the calls that follow. On failure it has already written the
// HTTP error response.
func (s *server) readerFor(ctx context.Context, c *framework.Context) (domain.Cluster, esclient.ClusterReader, context.Context, context.CancelFunc, bool) {
	cluster, err := s.clusters.GetCluster(ctx, c.Param("id"))
	if err != nil {
		writeFailure(c, s.log, err)
		return domain.Cluster{}, nil, nil, nil, false
	}
	reader, err := s.connector.Connect(cluster)
	if err != nil {
		writeFailure(c, s.log, err)
		return domain.Cluster{}, nil, nil, nil, false
	}
	callCtx, cancel := context.WithTimeout(ctx, clusterCallTimeout)
	return cluster, reader, callCtx, cancel, true
}

func (s *server) getMatrix(ctx context.Context, c *framework.Context) {
	cluster, reader, callCtx, cancel, ok := s.readerFor(ctx, c)
	if !ok {
		return
	}
	defer cancel()

	snapshot, err := matrix.Load(callCtx, reader)
	if err != nil {
		writeFailure(c, s.log, err)
		return
	}
	c.JSON(http.StatusOK, matrixResponse{
		Cluster:     toClusterResponse(cluster),
		Snapshot:    snapshot,
		GeneratedAt: time.Now().UTC(),
	})
}

func (s *server) getAllocationExplain(ctx context.Context, c *framework.Context) {
	index := c.Query("index")
	shard, err := strconv.Atoi(c.Query("shard"))
	if index == "" || err != nil || shard < 0 {
		writeError(c, http.StatusBadRequest, "index and a non-negative shard number are required")
		return
	}
	primary, err := strconv.ParseBool(c.Query("primary"))
	if err != nil {
		writeError(c, http.StatusBadRequest, "primary must be true or false")
		return
	}

	_, reader, callCtx, cancel, ok := s.readerFor(ctx, c)
	if !ok {
		return
	}
	defer cancel()

	explain, err := reader.AllocationExplain(callCtx, domain.AllocationExplainRequest{Index: index, Shard: shard, Primary: primary})
	if err != nil {
		// The cluster answers HTTP 400 when the shard is assigned: nothing to explain.
		if esclient.HasStatus(err, http.StatusBadRequest) {
			writeError(c, http.StatusUnprocessableEntity, "the cluster has nothing to explain for this shard; it is probably assigned")
			return
		}
		writeFailure(c, s.log, err)
		return
	}
	c.JSON(http.StatusOK, explain)
}
