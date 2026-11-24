// Turns an Elasticsearch answer into a table, a chart and CSV.

export interface Table {
  columns: string[]
  rows: Record<string, unknown>[]
}

export interface Bar {
  label: string
  value: number
}

export interface BarChart {
  title: string
  bars: Bar[]
}

const MAX_COLUMNS = 200
const MAX_BARS = 200

type Json = Record<string, unknown>

function isObject(value: unknown): value is Json {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

/** Flattens nested objects into dotted keys. Arrays are kept as JSON text. */
export function flatten(value: unknown, prefix = "", out: Json = {}): Json {
  if (!isObject(value)) {
    out[prefix || "value"] = Array.isArray(value) ? JSON.stringify(value) : value
    return out
  }
  for (const [key, child] of Object.entries(value)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (isObject(child)) flatten(child, path, out)
    else out[path] = Array.isArray(child) ? JSON.stringify(child) : child
  }
  return out
}

function tableFromRows(rows: Json[]): Table {
  const columns: string[] = []
  const seen = new Set<string>()
  for (const row of rows) {
    for (const key of Object.keys(row)) {
      if (!seen.has(key) && columns.length < MAX_COLUMNS) {
        seen.add(key)
        columns.push(key)
      }
    }
  }
  return { columns, rows }
}

/** Finds the first aggregation that has buckets. */
function firstBucketAggregation(response: Json): { name: string; buckets: Json[] } | null {
  const aggregations = response.aggregations ?? response.aggs
  if (!isObject(aggregations)) return null
  for (const [name, aggregation] of Object.entries(aggregations)) {
    if (isObject(aggregation) && Array.isArray(aggregation.buckets)) {
      // "filters" aggregations may return buckets as an object keyed by name.
      return { name, buckets: aggregation.buckets.filter(isObject) }
    }
    if (isObject(aggregation) && isObject(aggregation.buckets)) {
      const keyed = Object.entries(aggregation.buckets).map(([key, bucket]) => ({ key, ...(isObject(bucket) ? bucket : {}) }))
      return { name, buckets: keyed }
    }
  }
  return null
}

/**
 * Extracts rows from the usual response shapes: search hits, aggregation
 * buckets and arrays of objects (such as _cat output). Returns null when the
 * answer has nothing tabular.
 */
export function tableFrom(value: unknown): Table | null {
  if (Array.isArray(value)) {
    const rows = value.filter(isObject).map((row) => flatten(row))
    return rows.length > 0 ? tableFromRows(rows) : null
  }
  if (!isObject(value)) return null

  const hits = isObject(value.hits) ? value.hits.hits : undefined
  if (Array.isArray(hits) && hits.length > 0) {
    const rows = hits.filter(isObject).map((hit) => {
      const row: Json = { _index: hit._index, _id: hit._id }
      if (hit._score !== undefined && hit._score !== null) row._score = hit._score
      return { ...row, ...flatten(hit._source ?? hit.fields ?? {}) }
    })
    return tableFromRows(rows)
  }

  const aggregation = firstBucketAggregation(value)
  if (aggregation && aggregation.buckets.length > 0) {
    const rows = aggregation.buckets.map((bucket) => {
      const { key_as_string: keyAsString, ...rest } = bucket
      const row = flatten(rest)
      return keyAsString === undefined ? row : { ...row, key_as_string: keyAsString }
    })
    return tableFromRows(rows)
  }
  return null
}

/** Extracts the bars of the first bucket aggregation: one bar per bucket, sized by doc_count. */
export function chartFrom(value: unknown): BarChart | null {
  if (!isObject(value)) return null
  const aggregation = firstBucketAggregation(value)
  if (!aggregation) return null

  const bars: Bar[] = []
  for (const bucket of aggregation.buckets.slice(0, MAX_BARS)) {
    const count = bucket.doc_count
    if (typeof count !== "number") continue
    bars.push({ label: String(bucket.key_as_string ?? bucket.key ?? ""), value: count })
  }
  return bars.length > 0 ? { title: aggregation.name, bars } : null
}

/** Characters that make a spreadsheet interpret a text cell as a formula. */
const FORMULA_PREFIX = /^[=+\-@\t\r]/

function csvCell(value: unknown): string {
  if (value === null || value === undefined) return ""
  let text = typeof value === "string" ? value : typeof value === "object" ? JSON.stringify(value) : String(value)
  // Text that a spreadsheet would run as a formula is neutralized (CSV injection).
  if (typeof value === "string" && FORMULA_PREFIX.test(text)) text = `'${text}`
  return /[",\r\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text
}

/** Serializes a table as RFC 4180 CSV with CRLF line endings. */
export function toCSV(table: Table): string {
  const lines = [table.columns.map(csvCell).join(",")]
  for (const row of table.rows) {
    lines.push(table.columns.map((column) => csvCell(row[column])).join(","))
  }
  return lines.join("\r\n") + "\r\n"
}

/** Pretty-prints JSON text, or returns it unchanged when it is not JSON or very large. */
export function prettyJSON(text: string, maxBytes = 2 * 1024 * 1024): { text: string; isJSON: boolean; value?: unknown } {
  if (text.length > maxBytes) return { text, isJSON: false }
  try {
    const value: unknown = JSON.parse(text)
    return { text: JSON.stringify(value, null, 2), isJSON: true, value }
  } catch {
    return { text, isJSON: false }
  }
}
