package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/searchboard/searchboard/internal/domain"
)

const clusterColumns = "id, name, url, username, password, env, insecure_skip_verify, created_at"

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCluster(row rowScanner) (domain.Cluster, error) {
	var (
		c        domain.Cluster
		env      string
		insecure int
	)
	if err := row.Scan(&c.ID, &c.Name, &c.URL, &c.Username, &c.Password, &env, &insecure, &c.CreatedAt); err != nil {
		return domain.Cluster{}, err
	}
	c.Env = domain.Environment(env)
	c.InsecureSkipVerify = insecure != 0
	return c, nil
}

// ListClusters returns all registered clusters ordered by name.
func (s *Store) ListClusters(ctx context.Context) ([]domain.Cluster, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+clusterColumns+" FROM clusters ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("store: list clusters: %w", err)
	}
	defer rows.Close()

	clusters := []domain.Cluster{}
	for rows.Next() {
		cluster, err := scanCluster(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan cluster: %w", err)
		}
		clusters = append(clusters, cluster)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list clusters: %w", err)
	}
	return clusters, nil
}

// GetCluster returns one cluster, or domain.ErrNotFound.
func (s *Store) GetCluster(ctx context.Context, id string) (domain.Cluster, error) {
	row := s.DB.QueryRowContext(ctx, "SELECT "+clusterColumns+" FROM clusters WHERE id = ?", id)
	cluster, err := scanCluster(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Cluster{}, domain.ErrNotFound
		}
		return domain.Cluster{}, fmt.Errorf("store: get cluster: %w", err)
	}
	return cluster, nil
}

// CreateCluster stores a new cluster, assigning its ID and creation time.
// It returns domain.ErrConflict when the name is already taken.
func (s *Store) CreateCluster(ctx context.Context, c domain.Cluster) (domain.Cluster, error) {
	id, err := newID()
	if err != nil {
		return domain.Cluster{}, err
	}
	c.ID = id
	c.CreatedAt = time.Now().UTC().Truncate(time.Second)

	_, err = s.DB.ExecContext(ctx,
		"INSERT INTO clusters ("+clusterColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		c.ID, c.Name, c.URL, c.Username, c.Password, string(c.Env), boolToInt(c.InsecureSkipVerify), c.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return domain.Cluster{}, domain.ErrConflict
		}
		return domain.Cluster{}, fmt.Errorf("store: create cluster: %w", err)
	}
	return c, nil
}

// DeleteCluster removes a cluster and its DevTools history, or returns
// domain.ErrNotFound.
func (s *Store) DeleteCluster(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: delete cluster: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, "DELETE FROM clusters WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("store: delete cluster: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete cluster: %w", err)
	}
	if affected == 0 {
		return domain.ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM devtools_history WHERE cluster_id = ?", id); err != nil {
		return fmt.Errorf("store: delete cluster history: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: delete cluster: %w", err)
	}
	return nil
}

func newID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("store: generate id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
