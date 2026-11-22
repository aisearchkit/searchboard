import type { ShardRef, ShardState } from "@/lib/types"
import { cn } from "@/lib/utils"
import { describeShard } from "./format"

interface StateStyle {
  primary: string
  replica: string
}

// Primaries are filled, replicas are outlined; the hue encodes the shard state.
const STATE_STYLES: Record<ShardState, StateStyle> = {
  STARTED: {
    primary: "border-emerald-600 bg-emerald-600 text-white",
    replica: "border-emerald-500 text-emerald-400",
  },
  INITIALIZING: {
    primary: "border-sky-600 bg-sky-600 text-white",
    replica: "border-sky-500 text-sky-400",
  },
  RELOCATING: {
    primary: "border-amber-600 bg-amber-600 text-white",
    replica: "border-amber-500 text-amber-400",
  },
  UNASSIGNED: {
    primary: "border-red-600 bg-red-600 text-white",
    replica: "border-red-500 text-red-400",
  },
}

interface ShardChipProps {
  index: string
  shard: ShardRef
  selected: boolean
  onSelect: () => void
}

export function ShardChip({ index, shard, selected, onSelect }: ShardChipProps) {
  const style = STATE_STYLES[shard.state] ?? STATE_STYLES.STARTED
  const description = describeShard(index, shard)

  return (
    <button
      type="button"
      title={description}
      aria-label={description}
      onClick={onSelect}
      className={cn(
        "inline-flex h-6 min-w-6 items-center justify-center rounded border px-1 text-xs font-medium",
        shard.primary ? style.primary : style.replica,
        selected && "ring-2 ring-foreground",
      )}
    >
      {shard.shard}
      {shard.state === "RELOCATING" && <span aria-hidden="true">{">"}</span>}
    </button>
  )
}

export function ShardLegend() {
  const items: { label: string; className: string }[] = [
    { label: "Started", className: STATE_STYLES.STARTED.primary },
    { label: "Initializing", className: STATE_STYLES.INITIALIZING.primary },
    { label: "Relocating", className: STATE_STYLES.RELOCATING.primary },
    { label: "Unassigned", className: STATE_STYLES.UNASSIGNED.primary },
  ]
  return (
    <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
      {items.map((item) => (
        <span key={item.label} className="inline-flex items-center gap-1">
          <span className={cn("inline-block size-3 rounded border", item.className)} />
          {item.label}
        </span>
      ))}
      <span>Filled = primary, outlined = replica</span>
    </div>
  )
}
