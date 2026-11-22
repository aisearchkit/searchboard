import type { HealthStatus } from "@/lib/types"
import { cn } from "@/lib/utils"

const STYLES: Record<HealthStatus, string> = {
  green: "bg-emerald-500/15 text-emerald-400 border-emerald-500/40",
  yellow: "bg-amber-500/15 text-amber-400 border-amber-500/40",
  red: "bg-red-500/15 text-red-400 border-red-500/40",
}

export function HealthBadge({ status }: { status: HealthStatus }) {
  return (
    <span className={cn("inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium uppercase", STYLES[status])}>
      {status}
    </span>
  )
}

const DOT_STYLES: Record<HealthStatus, string> = {
  green: "bg-emerald-500",
  yellow: "bg-amber-500",
  red: "bg-red-500",
}

export function HealthDot({ status }: { status: HealthStatus }) {
  return <span aria-label={`health ${status}`} className={cn("inline-block size-2 shrink-0 rounded-full", DOT_STYLES[status])} />
}
