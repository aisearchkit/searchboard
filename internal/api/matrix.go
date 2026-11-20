package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

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

// readerFor loads the cluster named in the URL and opens a client for it.
// On failure it has already written the HTTP error response.
func (s *server) readerFor(w http.ResponseWriter, r *http.Request) (domain.Cluster, esclient.ClusterReader, context.Context, context.CancelFunc, bool) {
	cluster, err := s.clusters.GetCluster(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeFailure(w, s.log, err)
		return domain.Cluster{}, nil, nil, nil, false
	}
	reader, err := s.connector.Connect(cluster)
	if err != nil {
		writeFailure(w, s.log, err)
		return domain.Cluster{}, nil, nil, nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), clusterCallTimeout)
	return cluster, reader, ctx, cancel, true
}

func (s *server) getMatrix(w http.ResponseWriter, r *http.Request) {
	cluster, reader, ctx, cancel, ok := s.readerFor(w, r)
	if !ok {
		return
	}
	defer cancel()

	snapshot, err := matrix.Load(ctx, reader)
	if err != nil {
		writeFailure(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, matrixResponse{
		Cluster:     toClusterResponse(cluster),
		Snapshot:    snapshot,
		GeneratedAt: time.Now().UTC(),
	})
}

func (s *server) getAllocationExplain(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	index := query.Get("index")
	shard, err := strconv.Atoi(query.Get("shard"))
	if index == "" || err != nil || shard < 0 {
		writeError(w, http.StatusBadRequest, "index and a non-negative shard number are required")
		return
	}
	primary, err := strconv.ParseBool(query.Get("primary"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "primary must be true or false")
		return
	}

	_, reader, ctx, cancel, ok := s.readerFor(w, r)
	if !ok {
		return
	}
	defer cancel()

	explain, err := reader.AllocationExplain(ctx, domain.AllocationExplainRequest{Index: index, Shard: shard, Primary: primary})
	if err != nil {
		// The cluster answers HTTP 400 when the shard is assigned: nothing to explain.
		if esclient.HasStatus(err, http.StatusBadRequest) {
			writeError(w, http.StatusUnprocessableEntity, "the cluster has nothing to explain for this shard; it is probably assigned")
			return
		}
		writeFailure(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, explain)
}
