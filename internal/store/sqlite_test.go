package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/searchboard/searchboard/internal/domain"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data", "sb.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestOpenAppliesMigrationsOnce(t *testing.T) {
	s, path := openTemp(t)

	var version int
	if err := s.DB.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != len(migrations) {
		t.Fatalf("user_version = %d, err = %v, want %d", version, err, len(migrations))
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopening an up-to-date database must succeed: %v", err)
	}
	_ = reopened.Close()
}

func TestOpenCreatesPrivateFile(t *testing.T) {
	_, path := openTemp(t)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Fatalf("database file mode = %v, want owner-only access", mode)
	}
}

func TestClusterLifecycle(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)

	created, err := s.CreateCluster(ctx, domain.Cluster{
		Name: "prod-logs", URL: "https://es.example.com:9200",
		Username: "elastic", Password: "secret", Env: domain.EnvProd, InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() {
		t.Fatalf("ID and CreatedAt must be assigned: %+v", created)
	}

	got, err := s.GetCluster(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "prod-logs" || got.Password != "secret" || got.Env != domain.EnvProd || !got.InsecureSkipVerify {
		t.Fatalf("round trip lost data: %+v", got)
	}

	list, err := s.ListClusters(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListClusters = %v, %v", list, err)
	}

	if err := s.DeleteCluster(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCluster(ctx, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetCluster after delete = %v, want ErrNotFound", err)
	}
	if err := s.DeleteCluster(ctx, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
}

func TestCreateClusterRejectsDuplicateName(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)

	cluster := domain.Cluster{Name: "dup", URL: "http://es:9200", Env: domain.EnvDev}
	if _, err := s.CreateCluster(ctx, cluster); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCluster(ctx, cluster); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate create = %v, want ErrConflict", err)
	}
}

func TestListClustersIsEmptyNotNil(t *testing.T) {
	s, _ := openTemp(t)
	list, err := s.ListClusters(context.Background())
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("ListClusters = %#v, %v; want empty non-nil slice", list, err)
	}
}
