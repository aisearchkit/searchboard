package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/searchboard/searchboard/internal/domain"
)

func entry(cluster, request string) domain.HistoryEntry {
	return domain.HistoryEntry{ClusterID: cluster, Method: "GET", Path: "_search", Request: request, Status: 200, Risk: "safe", DurationMS: 12}
}

func TestHistoryAddListNewestFirst(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)

	for _, request := range []string{"GET a", "GET b", "GET c"} {
		if err := s.AddHistory(ctx, entry("c1", request)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddHistory(ctx, entry("c2", "GET other-cluster")); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListHistory(ctx, "c1", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Request != "GET c" || got[2].Request != "GET a" {
		t.Fatalf("history = %+v", got)
	}
	if got[0].Method != "GET" || got[0].Status != 200 || got[0].Risk != "safe" || got[0].DurationMS != 12 || got[0].CreatedAt.IsZero() {
		t.Fatalf("fields were not stored: %+v", got[0])
	}
}

func TestHistorySearchMatchesLiterally(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	for _, request := range []string{"GET logs/_search", "POST metrics/_search", `PUT docs/_doc/1 {"a":"100%"}`, "GET under_score"} {
		if err := s.AddHistory(ctx, entry("c1", request)); err != nil {
			t.Fatal(err)
		}
	}

	count := func(query string) int {
		got, err := s.ListHistory(ctx, "c1", query, 10)
		if err != nil {
			t.Fatal(err)
		}
		return len(got)
	}
	if got := count("LOGS"); got != 1 {
		t.Errorf("case-insensitive search found %d", got)
	}
	if got := count("_search"); got != 2 {
		t.Errorf("search found %d", got)
	}
	if got := count("%"); got != 1 {
		t.Errorf("a percent sign must match literally, found %d", got)
	}
	if got := count("_"); got != 4 {
		t.Errorf("an underscore must match literally (4 entries contain one), found %d", got)
	}
	if got := count("no-such-text"); got != 0 {
		t.Errorf("found %d", got)
	}
	if got := count(`'; DROP TABLE devtools_history; --`); got != 0 {
		t.Errorf("injection attempt matched %d", got)
	}
	if _, err := s.ListHistory(ctx, "c1", "", 1); err != nil {
		t.Fatalf("the table must still exist: %v", err)
	}
}

func TestHistoryLimitAndPruning(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)

	original := historyKeepPerCluster
	historyKeepPerCluster = 20
	defer func() { historyKeepPerCluster = original }()

	for i := 0; i < historyKeepPerCluster+5; i++ {
		if err := s.AddHistory(ctx, entry("busy", fmt.Sprintf("GET %d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddHistory(ctx, entry("quiet", "GET only")); err != nil {
		t.Fatal(err)
	}

	var busy int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM devtools_history WHERE cluster_id = 'busy'").Scan(&busy); err != nil || busy != historyKeepPerCluster {
		t.Fatalf("busy cluster keeps %d entries (err %v), want %d", busy, err, historyKeepPerCluster)
	}
	newest, _ := s.ListHistory(ctx, "busy", "", 1)
	if newest[0].Request != fmt.Sprintf("GET %d", historyKeepPerCluster+4) {
		t.Fatalf("the newest entry must survive pruning: %q", newest[0].Request)
	}
	if got, _ := s.ListHistory(ctx, "quiet", "", 10); len(got) != 1 {
		t.Fatalf("pruning must not touch other clusters: %d", len(got))
	}

	if got, _ := s.ListHistory(ctx, "busy", "", 100000); len(got) != historyKeepPerCluster {
		t.Errorf("a huge limit must be clamped and never return more than exists, got %d", len(got))
	}
	if got, _ := s.ListHistory(ctx, "busy", "", 5); len(got) != 5 {
		t.Errorf("limit 5 returned %d", len(got))
	}
}

func TestHistoryTruncatesHugeRequests(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	huge := make([]byte, maxHistoryRequestLen*2)
	for i := range huge {
		huge[i] = 'x'
	}
	if err := s.AddHistory(ctx, entry("c1", string(huge))); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ListHistory(ctx, "c1", "", 1)
	if len(got[0].Request) > maxHistoryRequestLen+32 || !strings.HasSuffix(got[0].Request, "(truncated)") {
		t.Fatalf("stored %d bytes, tail %q", len(got[0].Request), got[0].Request[len(got[0].Request)-13:])
	}
}

func TestClearHistory(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	_ = s.AddHistory(ctx, entry("c1", "GET a"))
	_ = s.AddHistory(ctx, entry("c1", "GET b"))
	_ = s.AddHistory(ctx, entry("c2", "GET keep"))

	removed, err := s.ClearHistory(ctx, "c1")
	if err != nil || removed != 2 {
		t.Fatalf("removed %d, err %v", removed, err)
	}
	if got, _ := s.ListHistory(ctx, "c2", "", 10); len(got) != 1 {
		t.Fatal("clearing one cluster must not touch another")
	}
}

func TestDeletingAClusterDeletesItsHistory(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	cluster, err := s.CreateCluster(ctx, domain.Cluster{Name: "demo", URL: "http://es:9200", Env: domain.EnvDev})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.AddHistory(ctx, entry(cluster.ID, "PUT secret/_doc/1 {\"password\":\"hunter2\"}"))

	if err := s.DeleteCluster(ctx, cluster.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListHistory(ctx, cluster.ID, "", 10); len(got) != 0 {
		t.Fatalf("history of a deleted cluster survived: %+v", got)
	}
}

func TestMigrationFromVersionOneKeepsExistingHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(migrations[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO devtools_history (cluster_id, request) VALUES ('c1', 'GET legacy')`); err != nil {
		t.Fatal(err)
	}
	_ = old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("upgrading a version 1 database failed: %v", err)
	}
	defer s.Close()

	got, err := s.ListHistory(context.Background(), "c1", "", 10)
	if err != nil || len(got) != 1 || got[0].Request != "GET legacy" || got[0].Risk != "safe" || got[0].Status != 0 {
		t.Fatalf("upgraded history = %+v, %v", got, err)
	}
}
