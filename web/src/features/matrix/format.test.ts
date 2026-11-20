import { describe, expect, it } from "vitest"
import type { IndexRow, ShardRef } from "@/lib/types"
import { describeShard, filterIndices, formatBytes } from "./format"

describe("formatBytes", () => {
  it("handles missing values", () => {
    expect(formatBytes(undefined)).toBe("-")
  })

  it("scales through binary units", () => {
    expect(formatBytes(0)).toBe("0 B")
    expect(formatBytes(512)).toBe("512 B")
    expect(formatBytes(1536)).toBe("1.5 KB")
    expect(formatBytes(5 * 1024 ** 3)).toBe("5.0 GB")
    expect(formatBytes(250 * 1024 ** 2)).toBe("250 MB")
  })
})

describe("describeShard", () => {
  it("summarizes a relocating replica", () => {
    const ref: ShardRef = {
      shard: 2,
      primary: false,
      state: "RELOCATING",
      docs: 1200,
      store_bytes: 2048,
      relocating_to: "node-2",
    }
    expect(describeShard("logs", ref)).toBe("logs [2] replica | relocating | moving to node-2 | 1,200 docs | 2.0 KB")
  })

  it("includes the unassigned reason", () => {
    const ref: ShardRef = { shard: 0, primary: true, state: "UNASSIGNED", unassigned_reason: "NODE_LEFT" }
    expect(describeShard("logs", ref)).toContain("reason: NODE_LEFT")
  })
})

describe("filterIndices", () => {
  const row = (name: string, health: IndexRow["health"]): IndexRow => ({
    name,
    health,
    cells: {},
    unassigned: [],
  })
  const indices = [row("logs-2026", "green"), row("Audit", "yellow"), row("metrics", "red")]

  it("returns everything for an empty filter", () => {
    expect(filterIndices(indices, { text: "", problemsOnly: false })).toHaveLength(3)
  })

  it("matches text case-insensitively", () => {
    expect(filterIndices(indices, { text: " AUD ", problemsOnly: false }).map((i) => i.name)).toEqual(["Audit"])
  })

  it("hides healthy indices when asked", () => {
    expect(filterIndices(indices, { text: "", problemsOnly: true }).map((i) => i.name)).toEqual(["Audit", "metrics"])
  })

  it("combines both filters", () => {
    expect(filterIndices(indices, { text: "logs", problemsOnly: true })).toEqual([])
  })
})
