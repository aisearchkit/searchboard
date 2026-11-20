package esclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/searchboard/searchboard/internal/domain"
)

// relocationArrow separates source and target in _cat/shards output for a
// relocating shard, for example "node-1 -> 10.0.0.2 Xk3 node-2".
const relocationArrow = " -> "

// catRow is one JSON object of a _cat response with every cell as a string.
// Missing cells and JSON nulls become empty strings.
type catRow map[string]string

// cat calls GET _cat/<target> asking for exactly the given columns. Elasticsearch
// echoes the requested column names as JSON keys, so callers read cells by the
// same names they requested.
func (c *Client) cat(ctx context.Context, target string, columns []string, extra url.Values) ([]catRow, error) {
	query := url.Values{
		"format": {"json"},
		"h":      {strings.Join(columns, ",")},
	}
	for key, values := range extra {
		query[key] = values
	}

	var raw []map[string]any
	if err := c.doJSON(ctx, http.MethodGet, "_cat/"+target, query, nil, &raw); err != nil {
		return nil, err
	}

	rows := make([]catRow, len(raw))
	for i, object := range raw {
		row := make(catRow, len(object))
		for key, value := range object {
			row[key] = cellString(value)
		}
		rows[i] = row
	}
	return rows, nil
}

func cellString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprint(v)
	}
}

// Nodes lists cluster nodes via _cat/nodes. The elected-master column is named
// "cluster_manager" on OpenSearch 2+ and "master" everywhere else.
func (c *Client) Nodes(ctx context.Context) ([]domain.Node, error) {
	version, err := c.Version(ctx)
	if err != nil {
		return nil, err
	}

	masterColumn := "master"
	if version.UsesClusterManagerTerm() {
		masterColumn = "cluster_manager"
	}

	rows, err := c.cat(ctx, "nodes", []string{
		"id", "name", "ip", "node.role", masterColumn,
		"heap.percent", "cpu", "disk.used_percent",
	}, nil)
	if err != nil {
		return nil, err
	}

	nodes := make([]domain.Node, 0, len(rows))
	for _, row := range rows {
		node := domain.Node{
			ID:       row["id"],
			Name:     row["name"],
			IP:       row["ip"],
			Roles:    row["node.role"],
			IsMaster: row[masterColumn] == "*",
			HeapPct:  parseFloat(row["heap.percent"]),
			CPUPct:   parseFloat(row["cpu"]),
			DiskPct:  parseFloat(row["disk.used_percent"]),
		}
		if node.ID == "" {
			node.ID = node.Name
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// Shards lists every shard copy via _cat/shards with sizes in bytes.
func (c *Client) Shards(ctx context.Context) ([]domain.Shard, error) {
	rows, err := c.cat(ctx, "shards", []string{
		"index", "shard", "prirep", "state", "docs", "store",
		"id", "node", "unassigned.reason",
	}, url.Values{"bytes": {"b"}})
	if err != nil {
		return nil, err
	}

	shards := make([]domain.Shard, 0, len(rows))
	for _, row := range rows {
		shard, err := parseShard(row)
		if err != nil {
			return nil, err
		}
		shards = append(shards, shard)
	}
	return shards, nil
}

func parseShard(row catRow) (domain.Shard, error) {
	number, err := strconv.Atoi(row["shard"])
	if err != nil {
		return domain.Shard{}, fmt.Errorf("esclient: invalid shard number %q for index %q", row["shard"], row["index"])
	}

	nodeName, relocatingToName := splitRelocation(row["node"], true)
	nodeID, relocatingToID := splitRelocation(row["id"], false)
	if nodeID == "" {
		nodeID = nodeName
	}
	if relocatingToID == "" {
		relocatingToID = relocatingToName
	}

	return domain.Shard{
		Index:            row["index"],
		Number:           number,
		Primary:          row["prirep"] == "p",
		State:            domain.ShardState(strings.ToUpper(row["state"])),
		Docs:             parseInt(row["docs"]),
		StoreBytes:       parseInt(row["store"]),
		NodeID:           nodeID,
		NodeName:         nodeName,
		RelocatingToID:   relocatingToID,
		RelocatingToName: relocatingToName,
		UnassignedReason: row["unassigned.reason"],
	}, nil
}

// splitRelocation splits a _cat/shards node cell into its source and target.
// Cells of non-relocating shards contain only the source.
//
// The "node" column holds "<source> -> <ip> <id> <name>" while relocating, so
// the target name is the last field. The "id" column holds "<source> -> <id>".
func splitRelocation(cell string, targetIsTriplet bool) (source, target string) {
	left, right, found := strings.Cut(cell, relocationArrow)
	if !found {
		return strings.TrimSpace(cell), ""
	}
	source = strings.TrimSpace(left)
	fields := strings.Fields(right)
	if len(fields) == 0 {
		return source, ""
	}
	if targetIsTriplet {
		return source, fields[len(fields)-1]
	}
	return source, fields[0]
}

func parseFloat(cell string) *float64 {
	if cell == "" {
		return nil
	}
	value, err := strconv.ParseFloat(cell, 64)
	if err != nil {
		return nil
	}
	return &value
}

func parseInt(cell string) *int64 {
	if cell == "" {
		return nil
	}
	value, err := strconv.ParseInt(cell, 10, 64)
	if err != nil {
		return nil
	}
	return &value
}
