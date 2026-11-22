import type { ClusterNode } from "@/lib/types"
import { cn } from "@/lib/utils"

function UsageBar({ label, pct }: { label: string; pct: number | undefined }) {
  if (pct === undefined) {
    return <div className="text-[10px] text-muted-foreground">{label} n/a</div>
  }
  const color = pct >= 85 ? "bg-red-500" : pct >= 70 ? "bg-amber-500" : "bg-emerald-500"
  return (
    <div className="flex items-center gap-1 text-[10px] text-muted-foreground" title={`${label} ${pct}%`}>
      <span className="w-7 text-left">{label}</span>
      <span className="h-1.5 w-16 overflow-hidden rounded bg-muted">
        <span className={cn("block h-full", color)} style={{ width: `${Math.min(pct, 100)}%` }} />
      </span>
      <span className="w-8 text-right">{Math.round(pct)}%</span>
    </div>
  )
}

export function NodeHeader({ node }: { node: ClusterNode }) {
  return (
    <div className="flex flex-col gap-0.5 text-left">
      <div className="flex items-center gap-1 text-sm font-medium">
        {node.is_master && (
          <span title="Elected master" aria-label="Elected master">
            *
          </span>
        )}
        <span className="max-w-40 truncate" title={node.name}>
          {node.name || node.id}
        </span>
      </div>
      <div className="text-[10px] text-muted-foreground">
        {node.ip} {node.roles && `| ${node.roles}`}
      </div>
      <UsageBar label="heap" pct={node.heap_pct} />
      <UsageBar label="cpu" pct={node.cpu_pct} />
      <UsageBar label="disk" pct={node.disk_pct} />
    </div>
  )
}
