// Package matrix turns flat node and shard listings into the node x index shard
// matrix shown by the Cluster Matrix page.
package matrix

import (
	"sort"

	"github.com/searchboard/searchboard/internal/domain"
)

// ShardRef is one shard copy as displayed inside a matrix cell.
type ShardRef struct {
	Shard            int               `json:"shard"`
	Primary          bool              `json:"primary"`
	State            domain.ShardState `json:"state"`
	Docs             *int64            `json:"docs,omitempty"`
	StoreBytes       *int64            `json:"store_bytes,omitempty"`
	RelocatingTo     string            `json:"relocating_to,omitempty"`
	UnassignedReason string            `json:"unassigned_reason,omitempty"`
}

// IndexRow is one matrix row: the shards of a single index, grouped by node ID.
type IndexRow struct {
	Name       string                `json:"name"`
	Health     domain.HealthStatus   `json:"health"`
	Cells      map[string][]ShardRef `json:"cells"`
	Unassigned []ShardRef            `json:"unassigned"`
}

// Matrix is the full grid. Columns are Nodes, rows are Indices.
type Matrix struct {
	Nodes   []domain.Node `json:"nodes"`
	Indices []IndexRow    `json:"indices"`
}

// Build assembles the matrix. Output ordering is deterministic: nodes by name,
// indices by name, shards by number with the primary first.
//
// A relocating shard stays in its source node's cell. Shards reported on a node
// that is missing from nodes (for example one that just left) get a placeholder
// column so they are never hidden.
func Build(nodes []domain.Node, shards []domain.Shard) Matrix {
	columns := withPlaceholderNodes(nodes, shards)
	sort.Slice(columns, func(i, j int) bool {
		if columns[i].Name != columns[j].Name {
			return columns[i].Name < columns[j].Name
		}
		return columns[i].ID < columns[j].ID
	})

	rows := map[string]*IndexRow{}
	for _, shard := range shards {
		row, ok := rows[shard.Index]
		if !ok {
			row = &IndexRow{
				Name:       shard.Index,
				Cells:      map[string][]ShardRef{},
				Unassigned: []ShardRef{},
			}
			rows[shard.Index] = row
		}

		ref := ShardRef{
			Shard:            shard.Number,
			Primary:          shard.Primary,
			State:            shard.State,
			Docs:             shard.Docs,
			StoreBytes:       shard.StoreBytes,
			RelocatingTo:     shard.RelocatingToName,
			UnassignedReason: shard.UnassignedReason,
		}
		if shard.NodeID == "" {
			row.Unassigned = append(row.Unassigned, ref)
		} else {
			row.Cells[shard.NodeID] = append(row.Cells[shard.NodeID], ref)
		}
	}

	indices := make([]IndexRow, 0, len(rows))
	for _, row := range rows {
		for nodeID := range row.Cells {
			sortShardRefs(row.Cells[nodeID])
		}
		sortShardRefs(row.Unassigned)
		row.Health = indexHealth(shards, row.Name)
		indices = append(indices, *row)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i].Name < indices[j].Name })

	return Matrix{Nodes: columns, Indices: indices}
}

func withPlaceholderNodes(nodes []domain.Node, shards []domain.Shard) []domain.Node {
	columns := make([]domain.Node, len(nodes), len(nodes)+1)
	copy(columns, nodes)

	known := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		known[node.ID] = struct{}{}
	}
	for _, shard := range shards {
		if shard.NodeID == "" {
			continue
		}
		if _, ok := known[shard.NodeID]; ok {
			continue
		}
		known[shard.NodeID] = struct{}{}
		columns = append(columns, domain.Node{ID: shard.NodeID, Name: shard.NodeName})
	}
	return columns
}

func sortShardRefs(refs []ShardRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Shard != refs[j].Shard {
			return refs[i].Shard < refs[j].Shard
		}
		return refs[i].Primary && !refs[j].Primary
	})
}

// indexHealth mirrors Elasticsearch's own rule: red when a primary is not
// started, yellow when only replicas are missing, green otherwise.
func indexHealth(shards []domain.Shard, index string) domain.HealthStatus {
	health := domain.HealthGreen
	for _, shard := range shards {
		if shard.Index != index {
			continue
		}
		// A relocating copy is still serving from its source node.
		active := shard.State == domain.ShardStarted || shard.State == domain.ShardRelocating
		switch {
		case shard.Primary && !active:
			return domain.HealthRed
		case !active:
			health = domain.HealthYellow
		}
	}
	return health
}
