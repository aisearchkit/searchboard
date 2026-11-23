package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/searchboard/searchboard/internal/domain"
)

// historyKeepPerCluster bounds the history of one cluster; older entries are
// pruned on insert. It is a variable only so tests can shrink it.
var historyKeepPerCluster = 1000

const (
	maxHistoryRequestLen = 64 << 10
	defaultHistoryLimit  = 100
	maxHistoryLimit      = 500
)

// AddHistory records a console request. The request text is cut at 64 KiB, and
// only the newest 1000 entries of the cluster are kept.
func (s *Store) AddHistory(ctx context.Context, e domain.HistoryEntry) error {
	if len(e.Request) > maxHistoryRequestLen {
		e.Request = e.Request[:maxHistoryRequestLen] + "\n... (truncated)"
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: add history: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO devtools_history (cluster_id, request, method, path, status, risk, duration_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ClusterID, e.Request, e.Method, e.Path, e.Status, e.Risk, e.DurationMS, e.CreatedAt,
	); err != nil {
		return fmt.Errorf("store: add history: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM devtools_history WHERE cluster_id = ? AND id NOT IN
		   (SELECT id FROM devtools_history WHERE cluster_id = ? ORDER BY id DESC LIMIT ?)`,
		e.ClusterID, e.ClusterID, historyKeepPerCluster,
	); err != nil {
		return fmt.Errorf("store: prune history: %w", err)
	}
	return tx.Commit()
}

// ListHistory returns the newest entries of a cluster first. A non-empty query
// keeps entries whose request text contains it, ignoring case. limit is clamped
// to 1..500 and defaults to 100.
func (s *Store) ListHistory(ctx context.Context, clusterID, query string, limit int) ([]domain.HistoryEntry, error) {
	switch {
	case limit <= 0:
		limit = defaultHistoryLimit
	case limit > maxHistoryLimit:
		limit = maxHistoryLimit
	}

	statement := `SELECT id, cluster_id, method, path, request, status, risk, duration_ms, created_at
	              FROM devtools_history WHERE cluster_id = ?`
	args := []any{clusterID}
	if query != "" {
		statement += ` AND request LIKE ? ESCAPE '\'`
		args = append(args, "%"+escapeLike(query)+"%")
	}
	statement += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list history: %w", err)
	}
	defer rows.Close()

	entries := []domain.HistoryEntry{}
	for rows.Next() {
		var e domain.HistoryEntry
		if err := rows.Scan(&e.ID, &e.ClusterID, &e.Method, &e.Path, &e.Request, &e.Status, &e.Risk, &e.DurationMS, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan history: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list history: %w", err)
	}
	return entries, nil
}

// ClearHistory deletes every entry of a cluster and returns how many were removed.
func (s *Store) ClearHistory(ctx context.Context, clusterID string) (int64, error) {
	result, err := s.DB.ExecContext(ctx, "DELETE FROM devtools_history WHERE cluster_id = ?", clusterID)
	if err != nil {
		return 0, fmt.Errorf("store: clear history: %w", err)
	}
	return result.RowsAffected()
}

// escapeLike escapes the LIKE wildcards so user text is matched literally.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
