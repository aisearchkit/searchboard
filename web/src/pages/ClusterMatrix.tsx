import { useState } from "react"
import { Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { AddClusterForm } from "@/features/matrix/AddClusterForm"
import { ExplainPanel } from "@/features/matrix/ExplainPanel"
import { filterIndices } from "@/features/matrix/format"
import { HealthBadge } from "@/features/matrix/HealthBadge"
import { MatrixTable } from "@/features/matrix/MatrixTable"
import { useClusters, useDeleteCluster, useMatrix, type ShardSelection } from "@/features/matrix/queries"
import { ShardLegend } from "@/features/matrix/ShardChip"
import type { ShardRef } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useWorkspace } from "@/stores/workspace"

type Selection = ShardSelection & { ref: ShardRef }

export default function ClusterMatrix() {
  const { clusterId, setCluster } = useWorkspace()
  const clusters = useClusters()
  const deleteCluster = useDeleteCluster()

  const [adding, setAdding] = useState(false)
  const [text, setText] = useState("")
  const [problemsOnly, setProblemsOnly] = useState(false)
  const [selection, setSelection] = useState<Selection | null>(null)

  // Fall back to the first cluster when the remembered one no longer exists.
  const active = clusters.data?.find((c) => c.id === clusterId) ?? clusters.data?.[0] ?? null
  const matrix = useMatrix(active?.id ?? null)

  if (clusters.isPending) {
    return <p className="text-sm text-muted-foreground">Loading clusters...</p>
  }
  if (clusters.isError) {
    return <p className="text-sm text-red-400">Could not load clusters: {clusters.error.message}</p>
  }

  if (clusters.data.length === 0 || adding) {
    return (
      <div className="flex flex-col gap-4">
        <h1 className="text-xl font-semibold">Cluster Matrix</h1>
        {clusters.data.length === 0 && (
          <p className="text-sm text-muted-foreground">No clusters yet. Connect an Elasticsearch or OpenSearch cluster to begin.</p>
        )}
        <AddClusterForm
          onCreated={(id) => {
            setCluster(id)
            setAdding(false)
          }}
          onCancel={clusters.data.length > 0 ? () => setAdding(false) : undefined}
        />
      </div>
    )
  }

  const removeActive = () => {
    if (!active) return
    if (!window.confirm(`Remove "${active.name}" from Searchboard? The cluster itself is not touched.`)) return
    deleteCluster.mutate(active.id, {
      onSuccess: () => {
        setCluster(null)
        setSelection(null)
        toast.success(`Removed ${active.name}`)
      },
      onError: (error) => toast.error(error.message),
    })
  }

  const snapshot = matrix.data
  const indices = snapshot ? filterIndices(snapshot.indices, { text, problemsOnly }) : []
  const isProd = active?.env === "prod"

  return (
    <div className={cn("flex flex-col gap-4", isProd && "rounded-lg border-2 border-red-500/70 p-4")}>
      <header className="flex flex-wrap items-center gap-3">
        <h1 className="text-xl font-semibold">Cluster Matrix</h1>
        <select
          aria-label="Cluster"
          className="h-9 rounded-md border border-border bg-transparent px-2 text-sm"
          value={active?.id ?? ""}
          onChange={(e) => {
            setCluster(e.target.value)
            setSelection(null)
          }}
        >
          {clusters.data.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
        </select>
        {isProd && <span className="rounded bg-red-600 px-2 py-0.5 text-xs font-bold text-white">PROD</span>}
        <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
          <Plus className="size-4" /> Add cluster
        </Button>
        <Button variant="ghost" size="sm" onClick={removeActive} disabled={deleteCluster.isPending}>
          <Trash2 className="size-4" /> Remove
        </Button>
      </header>

      {matrix.isPending && <p className="text-sm text-muted-foreground">Loading matrix...</p>}
      {matrix.isError && (
        <p className="text-sm text-red-400" role="alert">
          Could not read the cluster: {matrix.error.message}
        </p>
      )}

      {snapshot && (
        <>
          <section className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
            <HealthBadge status={snapshot.health.status} />
            <span className="font-medium">{snapshot.health.cluster_name}</span>
            <span className="text-muted-foreground">{snapshot.version}</span>
            <span>{snapshot.health.number_of_nodes} nodes</span>
            <span>{snapshot.health.active_shards} active shards</span>
            <span className={cn(snapshot.health.unassigned_shards > 0 && "text-red-400")}>
              {snapshot.health.unassigned_shards} unassigned
            </span>
            <span>{snapshot.health.relocating_shards} relocating</span>
            <span>{snapshot.health.initializing_shards} initializing</span>
            <span className="text-xs text-muted-foreground">
              Updated {new Date(snapshot.generated_at).toLocaleTimeString()}
            </span>
          </section>

          <div className="flex flex-wrap items-center gap-3">
            <Input
              className="max-w-xs"
              placeholder="Filter indices"
              aria-label="Filter indices"
              value={text}
              onChange={(e) => setText(e.target.value)}
            />
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={problemsOnly} onChange={(e) => setProblemsOnly(e.target.checked)} />
              Problems only
            </label>
          </div>

          <ShardLegend />

          <MatrixTable snapshot={snapshot} indices={indices} selected={selection} onSelect={setSelection} />

          {selection && active && (
            <ExplainPanel clusterId={active.id} selection={selection} onClose={() => setSelection(null)} />
          )}
        </>
      )}
    </div>
  )
}
