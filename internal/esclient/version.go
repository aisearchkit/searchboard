package esclient

import (
	"fmt"
	"strconv"
	"strings"
)

// Flavor distinguishes the Elasticsearch and OpenSearch code lines, which share
// an API origin but diverged in version numbering and some endpoints.
type Flavor string

const (
	FlavorElasticsearch Flavor = "elasticsearch"
	FlavorOpenSearch    Flavor = "opensearch"
)

// Version is a parsed server version together with its flavor.
type Version struct {
	Flavor Flavor
	Major  int
	Minor  int
	Patch  int
	Raw    string
}

// ParseVersion parses strings such as "7.10.2" or "8.0.0-rc1".
// Pre-release and build suffixes are ignored.
func ParseVersion(flavor Flavor, raw string) (Version, error) {
	core, _, _ := strings.Cut(strings.TrimSpace(raw), "-")
	parts := strings.Split(core, ".")
	if len(parts) < 2 {
		return Version{}, fmt.Errorf("esclient: unrecognized version %q", raw)
	}

	numbers := make([]int, 3)
	for i := 0; i < len(parts) && i < len(numbers); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return Version{}, fmt.Errorf("esclient: unrecognized version %q", raw)
		}
		numbers[i] = n
	}

	return Version{
		Flavor: flavor,
		Major:  numbers[0],
		Minor:  numbers[1],
		Patch:  numbers[2],
		Raw:    raw,
	}, nil
}

func (v Version) String() string {
	return fmt.Sprintf("%s %s", v.Flavor, v.Raw)
}

// AtLeast reports whether the version is >= major.minor within its own flavor.
func (v Version) AtLeast(major, minor int) bool {
	return v.Major > major || (v.Major == major && v.Minor >= minor)
}

// SupportsPIT reports whether Point-in-Time search is available
// (Elasticsearch 7.10+, OpenSearch 2.4+).
func (v Version) SupportsPIT() bool {
	if v.Flavor == FlavorOpenSearch {
		return v.AtLeast(2, 4)
	}
	return v.AtLeast(7, 10)
}

// SupportsSearchAfter reports whether the migration reader may rely on
// search_after. The API exists earlier in Elasticsearch, but 6.8 is the first
// release we treat as reliable for large exports.
func (v Version) SupportsSearchAfter() bool {
	if v.Flavor == FlavorOpenSearch {
		return true
	}
	return v.AtLeast(6, 8)
}

// RemovedMappingTypes reports whether mapping types are gone entirely
// (Elasticsearch 8+, OpenSearch 2+).
func (v Version) RemovedMappingTypes() bool {
	if v.Flavor == FlavorOpenSearch {
		return v.AtLeast(2, 0)
	}
	return v.AtLeast(8, 0)
}

// UsesClusterManagerTerm reports whether the API uses "cluster_manager" instead
// of the deprecated "master" terminology (OpenSearch 2+).
func (v Version) UsesClusterManagerTerm() bool {
	return v.Flavor == FlavorOpenSearch && v.AtLeast(2, 0)
}
