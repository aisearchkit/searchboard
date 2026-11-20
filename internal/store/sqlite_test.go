package store

import (
	"path/filepath"
	"testing"
)

func TestOpenMigrates(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "sb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.DB.Exec(`INSERT INTO clusters (id, name, url) VALUES ('c1', 'prod', 'http://es:9200')`); err != nil {
		t.Fatal(err)
	}
	var env string
	if err := s.DB.QueryRow(`SELECT env FROM clusters WHERE id = 'c1'`).Scan(&env); err != nil || env != "dev" {
		t.Fatalf("env=%q err=%v", env, err)
	}
}
