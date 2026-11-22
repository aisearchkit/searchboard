import { X } from "lucide-react"
import { Button } from "@/components/ui/button"
import type { ShardRef } from "@/lib/types"
import { describeShard } from "./format"
import { useAllocationExplain, type ShardSelection } from "./queries"

interface ExplainPanelProps {
  clusterId: string
  selection: ShardSelection & { ref: ShardRef }
  onClose: () => void
}

export function ExplainPanel({ clusterId, selection, onClose }: ExplainPanelProps) {
  const unassigned = selection.ref.state === "UNASSIGNED"
  const explain = useAllocationExplain(clusterId, selection, unassigned)

  return (
    <aside className="rounded-md border border-border bg-card p-4 text-sm" aria-label="Shard details">
      <div className="flex items-start justify-between gap-2">
        <p className="font-medium">{describeShard(selection.index, selection.ref)}</p>
        <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close shard details">
          <X className="size-4" />
        </Button>
      </div>

      {!unassigned && <p className="mt-2 text-muted-foreground">This shard copy is assigned; there is nothing to explain.</p>}

      {unassigned && explain.isPending && <p className="mt-2 text-muted-foreground">Asking the cluster why...</p>}
      {unassigned && explain.isError && <p className="mt-2 text-red-400">{explain.error.message}</p>}

      {explain.data && (
        <div className="mt-3 flex flex-col gap-3">
          {explain.data.explanation && <p>{explain.data.explanation}</p>}
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
            <dt className="text-muted-foreground">Unassigned reason</dt>
            <dd>{explain.data.unassigned_reason ?? "-"}</dd>
            <dt className="text-muted-foreground">Can allocate</dt>
            <dd>{explain.data.can_allocate ?? "-"}</dd>
          </dl>
          {explain.data.node_decisions?.map((decision) => (
            <div key={decision.node_name} className="rounded border border-border p-2">
              <p className="text-xs font-medium">
                {decision.node_name}: {decision.decision}
              </p>
              <ul className="mt-1 list-disc pl-5 text-xs text-muted-foreground">
                {decision.deciders?.map((decider) => (
                  <li key={decider.name}>
                    <span className="font-medium text-foreground">{decider.name}</span> ({decider.decision}):{" "}
                    {decider.explanation}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
    </aside>
  )
}
