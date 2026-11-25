// Completion for the console editor. Pure functions: the Monaco adapter only
// converts positions and kinds.

import type { Schema } from "@/lib/types"
import { METHODS, parseRequests, requestAtLine } from "./console"

export type SuggestionKind = "method" | "endpoint" | "index" | "alias" | "keyword" | "field"

export interface Suggestion {
  label: string
  insertText: string
  kind: SuggestionKind
  detail?: string
  /** Lower sorts first. */
  rank: number
}

export interface Completion {
  items: Suggestion[]
  /** 1-based column where the text to replace starts. */
  from: number
  /** 1-based column where it ends (the cursor). */
  to: number
}

const MAX_ITEMS = 300

const ROOT_ENDPOINTS = [
  "_search", "_count", "_msearch", "_mget", "_bulk", "_reindex", "_analyze", "_field_caps", "_validate",
  "_mapping", "_settings", "_aliases", "_alias", "_refresh", "_flush", "_stats", "_resolve",
  "_cat", "_cluster", "_nodes", "_snapshot", "_tasks", "_ingest", "_template", "_index_template",
  "_component_template", "_data_stream", "_ilm", "_slm", "_security",
]

const ROOT_CHILDREN: Record<string, string[]> = {
  _cat: ["indices", "shards", "nodes", "health", "allocation", "aliases", "count", "master", "plugins", "segments", "tasks", "templates", "thread_pool", "recovery", "repositories", "snapshots", "pending_tasks", "fielddata"],
  _cluster: ["health", "state", "stats", "settings", "allocation/explain", "pending_tasks", "reroute"],
  _nodes: ["stats", "info", "hot_threads", "usage"],
  _snapshot: ["_all"],
  _tasks: ["_cancel"],
  _ingest: ["pipeline"],
  _ilm: ["policy", "status", "explain"],
}

const INDEX_ENDPOINTS = [
  "_search", "_count", "_doc", "_mapping", "_settings", "_bulk", "_refresh", "_flush", "_forcemerge", "_stats",
  "_alias", "_aliases", "_open", "_close", "_delete_by_query", "_update_by_query", "_field_caps", "_analyze",
  "_validate/query", "_explain", "_segments", "_recovery", "_clone", "_shrink", "_split", "_rollover",
  "_termvectors", "_mget",
]

/** Query DSL, search body and aggregation names. */
const DSL_KEYWORDS = [
  "query", "aggs", "aggregations", "size", "from", "sort", "_source", "highlight", "track_total_hits", "collapse",
  "search_after", "post_filter", "rescore", "script_fields", "stored_fields", "docvalue_fields", "runtime_mappings",
  "timeout", "min_score", "fields", "knn",
  "match_all", "match_none", "match", "match_phrase", "match_phrase_prefix", "multi_match", "term", "terms", "range",
  "exists", "bool", "must", "filter", "should", "must_not", "minimum_should_match", "wildcard", "prefix", "regexp",
  "fuzzy", "ids", "nested", "query_string", "simple_query_string", "function_score", "constant_score", "boost",
  "terms_set", "geo_distance", "geo_bounding_box",
  "date_histogram", "histogram", "avg", "sum", "min", "max", "stats", "extended_stats", "cardinality", "percentiles",
  "value_count", "top_hits", "composite", "filters", "significant_terms", "calendar_interval", "fixed_interval",
  "field", "order", "format", "missing", "script",
]

/** Containers whose keys are field names. */
const FIELD_KEY_CONTAINERS = new Set([
  "match", "match_phrase", "match_phrase_prefix", "term", "terms", "range", "prefix", "wildcard", "regexp", "fuzzy",
  "fields", "highlight", "sort",
])

/** Properties whose string value is a field name. */
const FIELD_VALUE_KEYS = new Set(["field", "_source", "fields", "stored_fields", "docvalue_fields"])

/** Properties whose array elements are field names. */
const FIELD_ARRAY_KEYS = new Set(["_source", "fields", "stored_fields", "docvalue_fields", "sort", "includes", "excludes"])

interface Frame {
  bracket: "{" | "["
  /** The property this bracket is the value of, if any. */
  key: string | null
}

interface JSONState {
  inString: boolean
  /** Offset of the opening quote, when inString. */
  stringStart: number
  frames: Frame[]
  /** Last significant character before the string being typed. */
  previous: string
  /** The property name a value is being typed for (after a colon). */
  currentKey: string | null
}

/** Scans JSON-like text up to the cursor. It tolerates incomplete input. */
export function scanJSON(source: string): JSONState {
  const frames: Frame[] = []
  let inString = false
  let stringStart = -1
  let escaped = false
  let previous = ""
  let lastString: string | null = null
  let currentKey: string | null = null
  let buffer = ""

  for (let i = 0; i < source.length; i++) {
    const ch = source[i]
    if (inString) {
      if (escaped) escaped = false
      else if (ch === "\\") escaped = true
      else if (ch === '"') {
        inString = false
        lastString = buffer
        previous = '"'
      } else buffer += ch
      continue
    }
    if (ch === '"') {
      inString = true
      stringStart = i
      buffer = ""
      continue
    }
    if (/\s/.test(ch)) continue
    if (ch === ":") currentKey = lastString
    if (ch === "{" || ch === "[") frames.push({ bracket: ch, key: previous === ":" ? currentKey : null })
    if ((ch === "}" || ch === "]") && frames.length > 0) frames.pop()
    if (ch === "," || ch === "{" || ch === "[") currentKey = null
    previous = ch
  }
  return { inString, stringStart, frames, previous, currentKey }
}

/** Endpoints, indices and keywords match by prefix. */
function matches(label: string, typed: string): boolean {
  return label.toLowerCase().startsWith(typed.toLowerCase())
}

/** Fields match anywhere, so "name" finds "user.name". */
function matchesField(path: string, typed: string): boolean {
  return path.toLowerCase().includes(typed.toLowerCase())
}

function globToRegExp(pattern: string): RegExp {
  const escaped = pattern.replace(/[.+?^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*")
  return new RegExp(`^${escaped}$`)
}

/** Fields of the indices and aliases that a request path targets. */
export function targetFields(schema: Schema | undefined, path: string): { path: string; type: string }[] {
  if (!schema) return []
  const clean = path.split("?")[0].replace(/^\//, "")
  const first = clean.split("/")[0]

  let indices = schema.indices
  if (first !== "" && !first.startsWith("_")) {
    const patterns = first.split(",").filter(Boolean).map(globToRegExp)
    indices = indices.filter((index) => patterns.some((p) => p.test(index.name) || index.aliases.some((a) => p.test(a))))
  }

  const fields = new Map<string, string>()
  for (const index of indices) {
    for (const field of index.fields) {
      if (!fields.has(field.path)) fields.set(field.path, field.type)
    }
  }
  return [...fields].map(([fieldPath, type]) => ({ path: fieldPath, type }))
}

function methodItems(typed: string): Suggestion[] {
  return METHODS.filter((m) => m.startsWith(typed.toUpperCase())).map((m) => ({ label: m, insertText: m, kind: "method", rank: 0 }))
}

function pathItems(token: string, schema: Schema | undefined): { items: Suggestion[]; segment: string } {
  const clean = token.replace(/^\//, "")
  const parts = clean.split("/")
  const segment = parts[parts.length - 1]
  const completed = parts.slice(0, -1)
  if (token.includes("?")) return { items: [], segment: "" }

  const items: Suggestion[] = []
  if (completed.length === 0) {
    for (const index of schema?.indices ?? []) {
      if (matches(index.name, segment)) items.push({ label: index.name, insertText: index.name, kind: "index", rank: 0 })
      for (const alias of index.aliases) {
        if (matches(alias, segment)) items.push({ label: alias, insertText: alias, kind: "alias", detail: `alias of ${index.name}`, rank: 1 })
      }
    }
    for (const endpoint of ROOT_ENDPOINTS) {
      if (matches(endpoint, segment)) items.push({ label: endpoint, insertText: endpoint, kind: "endpoint", rank: 2 })
    }
  } else if (completed.length === 1 && completed[0].startsWith("_")) {
    for (const child of ROOT_CHILDREN[completed[0]] ?? []) {
      if (matches(child, segment)) items.push({ label: child, insertText: child, kind: "endpoint", rank: 0 })
    }
  } else if (completed.length === 1) {
    for (const endpoint of INDEX_ENDPOINTS) {
      if (matches(endpoint, segment)) items.push({ label: endpoint, insertText: endpoint, kind: "endpoint", rank: 0 })
    }
  }
  return { items, segment }
}

function bodyItems(source: string, requestPath: string, schema: Schema | undefined): { items: Suggestion[]; typed: string } {
  const state = scanJSON(source)
  if (!state.inString) return { items: [], typed: "" }
  const typed = source.slice(state.stringStart + 1)
  const top = state.frames[state.frames.length - 1]

  const fieldItems = (rank: number): Suggestion[] =>
    targetFields(schema, requestPath)
      .filter((f) => matchesField(f.path, typed))
      .map((f) => ({ label: f.path, insertText: f.path, kind: "field" as const, detail: f.type, rank }))
  const keywordItems = (rank: number): Suggestion[] =>
    DSL_KEYWORDS.filter((k) => matches(k, typed)).map((k) => ({ label: k, insertText: k, kind: "keyword" as const, rank }))

  if (state.previous === ":") {
    return { items: state.currentKey !== null && FIELD_VALUE_KEYS.has(state.currentKey) ? fieldItems(0) : [], typed }
  }
  if (top?.bracket === "{" && (state.previous === "{" || state.previous === ",")) {
    const fieldsFirst = top.key !== null && FIELD_KEY_CONTAINERS.has(top.key)
    return { items: [...fieldItems(fieldsFirst ? 0 : 1), ...keywordItems(fieldsFirst ? 1 : 0)], typed }
  }
  if (top?.bracket === "[" && (state.previous === "[" || state.previous === ",")) {
    return { items: top.key !== null && FIELD_ARRAY_KEYS.has(top.key) ? fieldItems(0) : [], typed }
  }
  return { items: [], typed }
}

/**
 * Computes completions for the cursor at (line, column), both 1-based.
 * schema may be undefined while it is loading.
 */
export function complete(text: string, line: number, column: number, schema?: Schema): Completion {
  const lines = text.split(/\r?\n/)
  const before = (lines[line - 1] ?? "").slice(0, Math.max(column - 1, 0))
  const empty: Completion = { items: [], from: column, to: column }

  if (before.trimStart().startsWith("#")) return empty

  // A new request line: only letters typed so far.
  if (/^[A-Za-z]*$/.test(before)) {
    return { items: methodItems(before), from: column - before.length, to: column }
  }

  // The path of a request line.
  const requestLine = /^(?:GET|POST|PUT|DELETE|HEAD|PATCH)\s+(\S*)$/i.exec(before)
  if (requestLine) {
    const { items, segment } = pathItems(requestLine[1], schema)
    return { items: cap(items), from: column - segment.length, to: column }
  }

  // The body of a request: a JSON string being typed.
  const request = requestAtLine(parseRequests(text), line)
  if (request && line > request.startLine) {
    const bodyBefore = [...lines.slice(request.startLine, line - 1), before].join("\n")
    const { items, typed } = bodyItems(bodyBefore, request.path, schema)
    return { items: cap(items), from: column - typed.length, to: column }
  }
  return empty
}

/** Orders by rank (the sort is stable, so equal ranks keep their order) and limits the list. */
function cap(items: Suggestion[]): Suggestion[] {
  return [...items].sort((a, b) => a.rank - b.rank).slice(0, MAX_ITEMS)
}
