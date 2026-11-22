import type { IndexRow, ShardRef } from "@/lib/types"

const UNITS = ["B", "KB", "MB", "GB", "TB", "PB"]

/** Formats a byte count with binary units, for example 1536 -> "1.5 KB". */
export function formatBytes(bytes: number | undefined): string {
  if (bytes === undefined) return "-"
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024
    unit++
  }
  const digits = unit === 0 || value >= 100 ? 0 : 1
  return `${value.toFixed(digits)} ${UNITS[unit]}`
}

/** Short human description of a shard copy, used for tooltips. */
export function describeShard(index: string, ref: ShardRef): string {
  const role = ref.primary ? "primary" : "replica"
  const parts = [`${index} [${ref.shard}] ${role}`, ref.state.toLowerCase()]
  if (ref.relocating_to) parts.push(`moving to ${ref.relocating_to}`)
  if (ref.docs !== undefined) parts.push(`${ref.docs.toLocaleString("en-US")} docs`)
  if (ref.store_bytes !== undefined) parts.push(formatBytes(ref.store_bytes))
  if (ref.unassigned_reason) parts.push(`reason: ${ref.unassigned_reason}`)
  return parts.join(" | ")
}

export interface IndexFilter {
  text: string
  problemsOnly: boolean
}

/** Keeps indices matching the text (case-insensitive) and, optionally, only unhealthy ones. */
export function filterIndices(indices: IndexRow[], filter: IndexFilter): IndexRow[] {
  const needle = filter.text.trim().toLowerCase()
  return indices.filter((index) => {
    if (filter.problemsOnly && index.health === "green") return false
    return needle === "" || index.name.toLowerCase().includes(needle)
  })
}
