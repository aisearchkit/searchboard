package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/searchboard/searchboard/framework"
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

func (s *server) listClusters(ctx context.Context, c *framework.Context) {
	clusters, err := s.clusters.ListClusters(ctx)
	if err != nil {
		writeFailure(c, s.log, err)
		return
	}
	response := make([]clusterResponse, len(clusters))
	for i, cluster := range clusters {
		response[i] = toClusterResponse(cluster)
	}
	c.JSON(http.StatusOK, response)
}

func (s *server) createCluster(ctx context.Context, c *framework.Context) {
	var request createClusterRequest
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := request.validate(); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	candidate := domain.Cluster{
		Name: request.Name, URL: request.URL, Username: request.Username,
		Password: request.Password, Env: request.Env, InsecureSkipVerify: request.InsecureSkipVerify,
	}

	// Verify connectivity and credentials before storing anything.
	reader, err := s.connector.Connect(candidate)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	testCtx, cancel := context.WithTimeout(ctx, connectionTestLimit)
	defer cancel()
	if _, err := reader.Version(testCtx); err != nil {
		s.log.Warn("cluster connection test failed", "name", candidate.Name, "err", err)
		writeError(c, http.StatusUnprocessableEntity, "could not connect to the cluster; check the URL, credentials and TLS settings")
		return
	}

	created, err := s.clusters.CreateCluster(ctx, candidate)
	if err != nil {
		writeFailure(c, s.log, err)
		return
	}
	c.JSON(http.StatusCreated, toClusterResponse(created))
}

func (s *server) deleteCluster(ctx context.Context, c *framework.Context) {
	id := c.Param("id")
	if err := s.clusters.DeleteCluster(ctx, id); err != nil {
		writeFailure(c, s.log, err)
		return
	}
	s.connector.Forget(id)
	s.schemas.forget(id)
	c.NoContent(http.StatusNoContent)
}
