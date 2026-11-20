// Package esclient hides API differences between Elasticsearch 5.x-9.x and OpenSearch.
package esclient

import (
	"context"
	"strconv"
	"strings"
)

type Flavor string

const (
	FlavorElasticsearch Flavor = "elasticsearch"
	FlavorOpenSearch    Flavor = "opensearch"
)

type Version struct {
	Flavor Flavor
	Major  int
	Minor  int
}

// ParseVersion parses "7.10.2" style strings.
func ParseVersion(flavor Flavor, s string) Version {
	v := Version{Flavor: flavor}
	parts := strings.SplitN(s, ".", 3)
	if len(parts) > 0 {
		v.Major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) > 1 {
		v.Minor, _ = strconv.Atoi(parts[1])
	}
	return v
}

// SupportsPIT reports whether Point-in-Time + search_after is available
// (ES 7.10+, OpenSearch 2.4+).
func (v Version) SupportsPIT() bool {
	if v.Flavor == FlavorOpenSearch {
		return v.Major > 2 || (v.Major == 2 && v.Minor >= 4)
	}
	return v.Major > 7 || (v.Major == 7 && v.Minor >= 10)
}

// SupportsSearchAfter reports ES 6.8+ / OpenSearch (any).
func (v Version) SupportsSearchAfter() bool {
	if v.Flavor == FlavorOpenSearch {
		return true
	}
	return v.Major > 6 || (v.Major == 6 && v.Minor >= 8)
}

// Client is the version-agnostic surface used by the rest of the server.
type Client interface {
	Version(ctx context.Context) (Version, error)
	ClusterHealth(ctx context.Context) ([]byte, error)
	Do(ctx context.Context, method, path string, body []byte) (status int, resp []byte, err error)
}
