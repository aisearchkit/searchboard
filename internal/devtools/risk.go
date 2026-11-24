// Package devtools holds the rules behind the DevTools console that do not
// depend on HTTP: how risky a request to a cluster is.
package devtools

import (
	"net/http"
	"strings"
)

// Level says how much care a request needs.
type Level string

const (
	// Safe requests only read data.
	Safe Level = "safe"
	// Write requests change data or settings. They need confirmation on production clusters.
	Write Level = "write"
	// Dangerous requests can destroy data or disrupt the cluster. They always need confirmation.
	Dangerous Level = "dangerous"
)

// Risk is the verdict for one request.
type Risk struct {
	Level  Level
	Reason string
}

// readOnlyEndpoints are endpoints that use POST (or GET with a body) but only read.
var readOnlyEndpoints = map[string]bool{
	"_search": true, "_count": true, "_msearch": true, "_mget": true,
	"_termvectors": true, "_mtermvectors": true, "_explain": true, "_validate": true,
	"_analyze": true, "_field_caps": true, "_search_shards": true, "_rank_eval": true,
	"_render": true, "_sql": true, "_eql": true, "_query": true, "_graph": true,
	"_resolve": true, "_knn_search": true,
}

// dangerousEndpoints are segments that make any method dangerous.
var dangerousEndpoints = map[string]string{
	"_delete_by_query": "deletes every document that matches the query",
	"_update_by_query": "rewrites every document that matches the query",
	"_reindex":         "copies a whole index and can overwrite the destination",
	"_close":           "makes the index unavailable",
	"_forcemerge":      "is a heavy operation that can stall indexing and searching",
	"_restore":         "overwrites indices from a snapshot",
	"_shutdown":        "stops cluster nodes",
	"_cancel":          "cancels running tasks",
}

// dangerousRoots are top-level endpoints where every change is dangerous.
var dangerousRoots = map[string]string{
	"_security":           "changes users, roles or API keys",
	"_snapshot":           "changes snapshot repositories or snapshots",
	"_data_stream":        "changes or deletes data streams and their backing indices",
	"_index_template":     "changes how new indices are created",
	"_template":           "changes how new indices are created",
	"_component_template": "changes how new indices are created",
	"_ilm":                "changes index lifecycle policies",
	"_slm":                "changes snapshot lifecycle policies",
}

// Classify rates a request. path may carry a query string and a leading slash.
// The rules are deliberately conservative: anything unknown that is not a plain
// read counts as a write.
func Classify(method, path string) Risk {
	method = strings.ToUpper(method)
	segments := segmentsOf(path)

	if method == http.MethodGet || method == http.MethodHead {
		return Risk{Level: Safe}
	}

	for _, segment := range segments {
		if reason, ok := dangerousEndpoints[segment]; ok {
			return Risk{Level: Dangerous, Reason: segment + " " + reason}
		}
	}
	if len(segments) > 0 {
		if reason, ok := dangerousRoots[segments[0]]; ok {
			return Risk{Level: Dangerous, Reason: segments[0] + " " + reason}
		}
	}
	if len(segments) >= 2 && segments[0] == "_cluster" && (segments[1] == "settings" || segments[1] == "reroute" || segments[1] == "voting_config_exclusions") {
		return Risk{Level: Dangerous, Reason: "_cluster/" + segments[1] + " changes cluster-wide behavior"}
	}
	if len(segments) >= 1 && segments[0] == "_nodes" && contains(segments, "shutdown") {
		return Risk{Level: Dangerous, Reason: "stops cluster nodes"}
	}

	if method == http.MethodDelete && deletesWholeIndices(segments) {
		return Risk{Level: Dangerous, Reason: "deletes entire indices and everything in them"}
	}

	if (method == http.MethodPost || method == http.MethodPut) && isReadOnlyEndpoint(segments) {
		return Risk{Level: Safe}
	}
	return Risk{Level: Write, Reason: "changes data or settings"}
}

// segmentsOf returns the lower-cased path segments without query string.
func segmentsOf(path string) []string {
	path, _, _ = strings.Cut(path, "?")
	path = strings.Trim(strings.ToLower(path), "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

// deletesWholeIndices reports whether DELETE targets index names themselves:
// "DELETE logs", "DELETE logs-*,metrics", "DELETE _all". Deleting a single
// document or an alias has more segments and is only a write.
func deletesWholeIndices(segments []string) bool {
	return len(segments) == 1 && !strings.HasPrefix(segments[0], "_") || len(segments) == 1 && segments[0] == "_all"
}

func isReadOnlyEndpoint(segments []string) bool {
	for _, segment := range segments {
		if readOnlyEndpoints[segment] {
			return true
		}
	}
	// A painless script execution is read-only: POST _scripts/painless/_execute.
	return len(segments) >= 3 && segments[0] == "_scripts" && segments[len(segments)-1] == "_execute"
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
