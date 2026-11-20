import type { IndexRow, MatrixSnapshot, ShardRef } from "@/lib/types"
import { HealthDot } from "./HealthBadge"
import { NodeHeader } from "./NodeHeader"
import { ShardChip } from "./ShardChip"
import type { ShardSelection } from "./queries"

interface MatrixTableProps {
  snapshot: MatrixSnapshot
  indices: IndexRow[]
  selected: ShardSelection | null
  onSelect: (selection: ShardSelection & { ref: ShardRef }) => void
}

export function MatrixTable({ snapshot, indices, selected, onSelect }: MatrixTableProps) {
  if (indices.length === 0) {
    return <p className="py-8 text-center text-sm text-muted-foreground">No indices match the current filter.</p>
  }

  const isSelected = (index: string, ref: ShardRef) =>
    selected?.index === index && selected.shard === ref.shard && selected.primary === ref.primary

  const renderChips = (row: IndexRow, refs: ShardRef[]) =>
    refs.map((ref) => (
      <ShardChip
        key={`${ref.shard}-${ref.primary}-${ref.state}`}
        index={row.name}
        shard={ref}
        selected={isSelected(row.name, ref)}
        onSelect={() => onSelect({ index: row.name, shard: ref.shard, primary: ref.primary, ref })}
      />
    ))

  return (
    <div className="overflow-auto rounded-md border border-border">
      <table className="border-separate border-spacing-0 text-sm">
        <thead>
          <tr>
            <th className="sticky left-0 top-0 z-20 min-w-56 border-b border-r border-border bg-card p-2 text-left font-medium">
              Index ({indices.length})
            </th>
            {snapshot.nodes.map((node) => (
              <th key={node.id} className="sticky top-0 z-10 border-b border-border bg-card p-2 align-top">
                <NodeHeader node={node} />
              </th>
            ))}
            <th className="sticky top-0 z-10 border-b border-l border-border bg-card p-2 text-left font-medium">
              Unassigned
            </th>
          </tr>
        </thead>
        <tbody>
          {indices.map((row) => (
            <tr key={row.name} className="hover:bg-muted/40">
              <th
                scope="row"
                className="sticky left-0 z-10 border-b border-r border-border bg-card p-2 text-left font-normal"
              >
                <span className="flex items-center gap-2">
                  <HealthDot status={row.health} />
                  <span className="max-w-64 truncate" title={row.name}>
                    {row.name}
                  </span>
                </span>
              </th>
              {snapshot.nodes.map((node) => (
                <td key={node.id} className="border-b border-border p-2 align-top">
                  <div className="flex flex-wrap gap-1">{renderChips(row, row.cells[node.id] ?? [])}</div>
                </td>
              ))}
              <td className="border-b border-l border-border p-2 align-top">
                <div className="flex flex-wrap gap-1">{renderChips(row, row.unassigned)}</div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
