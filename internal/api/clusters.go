package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/searchboard/searchboard/internal/domain"
)

const (
	maxBodyBytes        = 1 << 20
	maxClusterNameLen   = 64
	connectionTestLimit = 10 * time.Second
)

// ClusterRepository persists registered clusters.
type ClusterRepository interface {
	ListClusters(ctx context.Context) ([]domain.Cluster, error)
	GetCluster(ctx context.Context, id string) (domain.Cluster, error)
	CreateCluster(ctx context.Context, c domain.Cluster) (domain.Cluster, error)
	DeleteCluster(ctx context.Context, id string) error
}

// clusterResponse is the public view of a cluster. It deliberately has no
// password field.
type clusterResponse struct {
	ID                 string             `json:"id"`
	Name               string             `json:"name"`
	URL                string             `json:"url"`
	Username           string             `json:"username"`
	Env                domain.Environment `json:"env"`
	InsecureSkipVerify bool               `json:"insecure_skip_verify"`
	CreatedAt          time.Time          `json:"created_at"`
}

func toClusterResponse(c domain.Cluster) clusterResponse {
	return clusterResponse{
		ID: c.ID, Name: c.Name, URL: c.URL, Username: c.Username,
		Env: c.Env, InsecureSkipVerify: c.InsecureSkipVerify, CreatedAt: c.CreatedAt,
	}
}

type createClusterRequest struct {
	Name               string             `json:"name"`
	URL                string             `json:"url"`
	Username           string             `json:"username"`
	Password           string             `json:"password"`
	Env                domain.Environment `json:"env"`
	InsecureSkipVerify bool               `json:"insecure_skip_verify"`
}

// validate normalizes the request in place and reports the first problem.
func (r *createClusterRequest) validate() error {
	r.Name = strings.TrimSpace(r.Name)
	r.URL = strings.TrimRight(strings.TrimSpace(r.URL), "/")
	if r.Env == "" {
		r.Env = domain.EnvDev
	}

	switch {
	case r.Name == "":
		return errors.New("name is required")
	case len(r.Name) > maxClusterNameLen:
		return fmt.Errorf("name must be at most %d characters", maxClusterNameLen)
	case !r.Env.Valid():
		return errors.New("env must be one of: prod, staging, dev")
	}

	parsed, err := url.Parse(r.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("url must be an absolute http or https URL")
	}
	return nil
}

func (s *server) listClusters(w http.ResponseWriter, r *http.Request) {
	clusters, err := s.clusters.ListClusters(r.Context())
	if err != nil {
		writeFailure(w, s.log, err)
		return
	}
	response := make([]clusterResponse, len(clusters))
	for i, cluster := range clusters {
		response[i] = toClusterResponse(cluster)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *server) createCluster(w http.ResponseWriter, r *http.Request) {
	var request createClusterRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := request.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	candidate := domain.Cluster{
		Name: request.Name, URL: request.URL, Username: request.Username,
		Password: request.Password, Env: request.Env, InsecureSkipVerify: request.InsecureSkipVerify,
	}

	// Verify connectivity and credentials before storing anything.
	reader, err := s.connector.Connect(candidate)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), connectionTestLimit)
	defer cancel()
	if _, err := reader.Version(ctx); err != nil {
		s.log.Warn("cluster connection test failed", "name", candidate.Name, "err", err)
		writeError(w, http.StatusUnprocessableEntity, "could not connect to the cluster; check the URL, credentials and TLS settings")
		return
	}

	created, err := s.clusters.CreateCluster(r.Context(), candidate)
	if err != nil {
		writeFailure(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, toClusterResponse(created))
}

func (s *server) deleteCluster(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.clusters.DeleteCluster(r.Context(), id); err != nil {
		writeFailure(w, s.log, err)
		return
	}
	s.connector.Forget(id)
	w.WriteHeader(http.StatusNoContent)
}
