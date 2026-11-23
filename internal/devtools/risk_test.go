package devtools

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		method, path string
		want         Level
	}{
		// Reads.
		{"GET", "_cluster/health", Safe},
		{"GET", "/logs/_search?q=x", Safe},
		{"HEAD", "logs", Safe},
		{"GET", "_cat/indices?format=json", Safe},
		{"GET", "_snapshot/_all", Safe},
		// Reads that use POST.
		{"POST", "logs/_search", Safe},
		{"POST", "/logs-*/_search?size=0", Safe},
		{"POST", "_msearch", Safe},
		{"POST", "logs/_count", Safe},
		{"POST", "_analyze", Safe},
		{"POST", "logs/_validate/query", Safe},
		{"POST", "logs/_explain/1", Safe},
		{"POST", "_sql?format=json", Safe},
		{"PUT", "_scripts/painless/_execute", Safe},
		{"POST", "_scripts/painless/_execute", Safe},
		// Plain writes.
		{"PUT", "logs/_doc/1", Write},
		{"POST", "logs/_doc", Write},
		{"POST", "_bulk", Write},
		{"POST", "logs/_bulk", Write},
		{"DELETE", "logs/_doc/1", Write},
		{"PUT", "logs/_settings", Write},
		{"PUT", "logs/_mapping", Write},
		{"POST", "logs/_refresh", Write},
		{"PUT", "_ingest/pipeline/p", Write},
		{"PATCH", "logs/_doc/1", Write},
		{"FOO", "anything", Write},
		// Dangerous.
		{"DELETE", "logs", Dangerous},
		{"DELETE", "/logs/", Dangerous},
		{"DELETE", "logs-*", Dangerous},
		{"DELETE", "_all", Dangerous},
		{"DELETE", "*", Dangerous},
		{"DELETE", "logs,metrics", Dangerous},
		{"POST", "logs/_delete_by_query", Dangerous},
		{"POST", "logs/_update_by_query", Dangerous},
		{"POST", "_reindex", Dangerous},
		{"POST", "logs/_close", Dangerous},
		{"POST", "logs/_forcemerge", Dangerous},
		{"POST", "_snapshot/repo/snap/_restore", Dangerous},
		{"DELETE", "_snapshot/repo/snap", Dangerous},
		{"PUT", "_snapshot/repo", Dangerous},
		{"PUT", "_cluster/settings", Dangerous},
		{"POST", "_cluster/reroute", Dangerous},
		{"POST", "_cluster/voting_config_exclusions", Dangerous},
		{"PUT", "_nodes/node-1/shutdown", Dangerous},
		{"POST", "_tasks/abc:1/_cancel", Dangerous},
		{"PUT", "_security/user/bob", Dangerous},
		{"DELETE", "_security/api_key", Dangerous},
		{"DELETE", "_data_stream/logs", Dangerous},
		{"PUT", "_index_template/t", Dangerous},
		{"POST", "_ilm/stop", Dangerous},
		// Case and slashes do not hide anything.
		{"delete", "LOGS", Dangerous},
		{"post", "Logs/_Delete_By_Query", Dangerous},
		{"POST", "//logs//_delete_by_query", Dangerous},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			got := Classify(tt.method, tt.path)
			if got.Level != tt.want {
				t.Fatalf("Classify = %+v, want level %s", got, tt.want)
			}
			if got.Level != Safe && got.Reason == "" {
				t.Fatal("a risky request must come with a reason the UI can show")
			}
		})
	}
}

func TestQueryStringNeverChangesTheVerdict(t *testing.T) {
	// An attacker-controlled query string must not turn a dangerous path into a safe one.
	if got := Classify("DELETE", "logs?x=/_search"); got.Level != Dangerous {
		t.Fatalf("level = %s", got.Level)
	}
	if got := Classify("POST", "logs?q=_search"); got.Level != Write {
		t.Fatalf("a query string must not look like a read-only endpoint, got %s", got.Level)
	}
}
