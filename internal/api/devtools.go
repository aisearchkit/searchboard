package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/internal/devtools"
	"github.com/searchboard/searchboard/internal/domain"
	"github.com/searchboard/searchboard/internal/esclient"
)

const (
	proxyTimeout      = 60 * time.Second
	maxProxyPathLen   = 4096
	defaultSchemaTTL  = 30 * time.Second
	defaultHistoryMax = 100
)

// allowedProxyMethods are the methods the console may send.
var allowedProxyMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true,
	http.MethodPut: true, http.MethodDelete: true, http.MethodPatch: true,
}

// HistoryRepository persists the DevTools console history.
type HistoryRepository interface {
	AddHistory(ctx context.Context, e domain.HistoryEntry) error
	ListHistory(ctx context.Context, clusterID, query string, limit int) ([]domain.HistoryEntry, error)
	ClearHistory(ctx context.Context, clusterID string) (int64, error)
}

type proxyRequest struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   string `json:"body"`
	// Confirm acknowledges the risk reported by an earlier 428 answer.
	Confirm bool `json:"confirm"`
}

type proxyResponse struct {
	Status      int    `json:"status"` // the status the cluster answered with
	ContentType string `json:"content_type"`
	Body        string `json:"body"`
	DurationMS  int64  `json:"duration_ms"`
	Risk        string `json:"risk"`
}

// confirmationBody is the 428 answer that asks the user to confirm a risky request.
type confirmationBody struct {
	Error  string `json:"error"`
	Risk   string `json:"risk"`
	Reason string `json:"reason"`
}

// proxy forwards a console request to the cluster.
//
// Risky requests are held back until the client repeats them with confirm=true:
// dangerous ones always, writes only on production clusters. The check happens
// here, not only in the UI, so no client can skip it by accident.
func (s *server) proxy(ctx context.Context, c *framework.Context) {
	var in proxyRequest
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		writeError(c, http.StatusBadRequest, "invalid JSON body")
		return
	}
	in.Method = strings.ToUpper(strings.TrimSpace(in.Method))
	in.Path = strings.TrimSpace(in.Path)
	if !allowedProxyMethods[in.Method] {
		writeError(c, http.StatusBadRequest, "method must be one of GET, HEAD, POST, PUT, DELETE, PATCH")
		return
	}
	if in.Path == "" || len(in.Path) > maxProxyPathLen {
		writeError(c, http.StatusBadRequest, "path is required and must be shorter than 4096 characters")
		return
	}

	cluster, err := s.clusters.GetCluster(ctx, c.Param("id"))
	if err != nil {
		writeFailure(c, s.log, err)
		return
	}

	risk := devtools.Classify(in.Method, in.Path)
	if needsConfirmation(risk, cluster.Env) && !in.Confirm {
		c.JSON(http.StatusPreconditionRequired, confirmationBody{
			Error:  "this request needs confirmation",
			Risk:   string(risk.Level),
			Reason: risk.Reason,
		})
		return
	}

	client, err := s.connector.Connect(cluster)
	if err != nil {
		writeFailure(c, s.log, err)
		return
	}

	var body []byte
	if in.Body != "" {
		body = []byte(in.Body)
	}
	callCtx, cancel := context.WithTimeout(ctx, proxyTimeout)
	defer cancel()
	started := time.Now()
	raw, err := client.Passthrough(callCtx, in.Method, in.Path, body)
	elapsed := time.Since(started)

	switch {
	case errors.Is(err, esclient.ErrInvalidPath):
		writeError(c, http.StatusBadRequest, "invalid request path")
		return
	case errors.Is(err, esclient.ErrResponseTooLarge):
		writeError(c, http.StatusUnprocessableEntity, "the response is larger than 16 MiB; narrow the request with size or _source")
		return
	case err != nil:
		writeFailure(c, s.log, err)
		return
	}

	s.recordHistory(ctx, cluster.ID, in, raw.Status, risk.Level, elapsed)
	c.JSON(http.StatusOK, proxyResponse{
		Status:      raw.Status,
		ContentType: raw.ContentType,
		Body:        string(raw.Body),
		DurationMS:  elapsed.Milliseconds(),
		Risk:        string(risk.Level),
	})
}

// needsConfirmation is the confirmation policy: dangerous requests always need
// it, writes only on production clusters.
func needsConfirmation(risk devtools.Risk, env domain.Environment) bool {
	switch risk.Level {
	case devtools.Dangerous:
		return true
	case devtools.Write:
		return env == domain.EnvProd
	}
	return false
}

// recordHistory stores what was executed. A failure to record is logged and
// never fails the request the user already ran.
func (s *server) recordHistory(ctx context.Context, clusterID string, in proxyRequest, status int, level devtools.Level, elapsed time.Duration) {
	text := in.Method + " /" + strings.TrimPrefix(in.Path, "/")
	if in.Body != "" {
		text += "\n" + in.Body
	}
	err := s.history.AddHistory(ctx, domain.HistoryEntry{
		ClusterID:  clusterID,
		Method:     in.Method,
		Path:       in.Path,
		Request:    text,
		Status:     status,
		Risk:       string(level),
		DurationMS: elapsed.Milliseconds(),
	})
	if err != nil {
		s.log.Warn("record devtools history", "err", err)
	}
}

type historyResponse struct {
	ID         int64     `json:"id"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Request    string    `json:"request"`
	Status     int       `json:"status"`
	Risk       string    `json:"risk"`
	DurationMS int64     `json:"duration_ms"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *server) listHistory(ctx context.Context, c *framework.Context) {
	cluster, err := s.clusters.GetCluster(ctx, c.Param("id"))
	if err != nil {
		writeFailure(c, s.log, err)
		return
	}
	limit := defaultHistoryMax
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(c, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
	}

	entries, err := s.history.ListHistory(ctx, cluster.ID, c.Query("q"), limit)
	if err != nil {
		writeFailure(c, s.log, err)
		return
	}
	response := make([]historyResponse, len(entries))
	for i, e := range entries {
		response[i] = historyResponse{
			ID: e.ID, Method: e.Method, Path: e.Path, Request: e.Request, Status: e.Status,
			Risk: e.Risk, DurationMS: e.DurationMS, CreatedAt: e.CreatedAt,
		}
	}
	c.JSON(http.StatusOK, response)
}

func (s *server) clearHistory(ctx context.Context, c *framework.Context) {
	cluster, err := s.clusters.GetCluster(ctx, c.Param("id"))
	if err != nil {
		writeFailure(c, s.log, err)
		return
	}
	if _, err := s.history.ClearHistory(ctx, cluster.ID); err != nil {
		writeFailure(c, s.log, err)
		return
	}
	c.NoContent(http.StatusNoContent)
}

// getSchema returns the indices and fields of a cluster for editor completion.
// The answer is cached briefly: editors ask often and large clusters are slow.
func (s *server) getSchema(ctx context.Context, c *framework.Context) {
	cluster, client, callCtx, cancel, ok := s.readerFor(ctx, c)
	if !ok {
		return
	}
	defer cancel()

	if cached, found := s.schemas.get(cluster.ID); found {
		c.JSON(http.StatusOK, cached)
		return
	}
	schema, err := client.Schema(callCtx)
	if err != nil {
		writeFailure(c, s.log, err)
		return
	}
	s.schemas.put(cluster.ID, schema)
	c.JSON(http.StatusOK, schema)
}

// schemaCache keeps one schema per cluster for a short time.
type schemaCache struct {
	ttl time.Duration

	mu      sync.Mutex
	entries map[string]schemaEntry
}

type schemaEntry struct {
	schema  domain.Schema
	expires time.Time
}

func newSchemaCache(ttl time.Duration) *schemaCache {
	if ttl <= 0 {
		ttl = defaultSchemaTTL
	}
	return &schemaCache{ttl: ttl, entries: map[string]schemaEntry{}}
}

func (c *schemaCache) get(clusterID string) (domain.Schema, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[clusterID]
	if !ok || time.Now().After(entry.expires) {
		delete(c.entries, clusterID)
		return domain.Schema{}, false
	}
	return entry.schema, true
}

func (c *schemaCache) put(clusterID string, schema domain.Schema) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[clusterID] = schemaEntry{schema: schema, expires: time.Now().Add(c.ttl)}
}

func (c *schemaCache) forget(clusterID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, clusterID)
}
