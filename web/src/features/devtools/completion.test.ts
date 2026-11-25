import { describe, expect, it } from "vitest"
import type { Schema } from "@/lib/types"
import { complete, scanJSON, targetFields } from "./completion"

const schema: Schema = {
  truncated: false,
  indices: [
    {
      name: "logs-2026",
      aliases: ["logs"],
      fields: [
        { path: "@timestamp", type: "date" },
        { path: "message", type: "text" },
        { path: "message.keyword", type: "keyword" },
        { path: "user.name", type: "keyword" },
      ],
    },
    { name: "metrics", aliases: [], fields: [{ path: "cpu", type: "float" }, { path: "host", type: "keyword" }] },
  ],
}

/** Completes at the end of `text` (the cursor is where the | marker would be). */
function at(text: string, s: Schema | undefined = schema) {
  const lines = text.split("\n")
  return complete(text, lines.length, lines[lines.length - 1].length + 1, s)
}

const labels = (c: { items: { label: string }[] }) => c.items.map((i) => i.label)

describe("method completion", () => {
  it("suggests methods on an empty line and filters by prefix", () => {
    expect(labels(at(""))).toEqual(["GET", "POST", "PUT", "DELETE", "HEAD", "PATCH"])
    expect(labels(at("PO"))).toEqual(["POST"])
    expect(labels(at("po"))).toEqual(["POST"])
    expect(labels(at("GET _cat\n\nP"))).toEqual(["POST", "PUT", "PATCH"])
  })

  it("replaces the typed letters", () => {
    expect(at("PO")).toMatchObject({ from: 1, to: 3 })
  })
})

describe("path completion", () => {
  it("offers indices, aliases and root endpoints for the first segment", () => {
    const result = at("GET ")
    expect(labels(result)).toEqual(expect.arrayContaining(["logs-2026", "logs", "metrics", "_search", "_cat"]))
    expect(result.items.find((i) => i.label === "logs")).toMatchObject({ kind: "alias", detail: "alias of logs-2026" })
    expect(result.items.find((i) => i.label === "metrics")?.kind).toBe("index")
    // indices come before aliases before endpoints
    expect(labels(result).indexOf("logs-2026")).toBeLessThan(labels(result).indexOf("_search"))
  })

  it("filters by what was typed and replaces only that segment", () => {
    const result = at("GET lo")
    expect(labels(result)).toEqual(["logs-2026", "logs"])
    expect(result).toMatchObject({ from: 5, to: 7 })
    expect(at("POST /me")).toMatchObject({ from: 7, to: 9 })
    expect(labels(at("POST /me"))).toEqual(["metrics"])
  })

  it("offers endpoints after an index", () => {
    const result = at("POST logs-2026/_sea")
    expect(labels(result)).toEqual(["_search"])
    expect(result).toMatchObject({ from: 16, to: 20 })
    expect(labels(at("POST logs-2026/_se"))).toEqual(["_search", "_settings", "_segments"]) // "_se" is a shared prefix
    expect(labels(at("PUT logs/"))).toEqual(expect.arrayContaining(["_doc", "_mapping", "_settings"]))
  })

  it("offers the children of API roots", () => {
    expect(labels(at("GET _cat/in"))).toEqual(["indices"])
    expect(labels(at("GET _cluster/"))).toEqual(expect.arrayContaining(["health", "settings", "allocation/explain"]))
  })

  it("works without a schema and stays quiet after a query string", () => {
    expect(labels(at("GET _sea", undefined))).toEqual(["_search"])
    expect(at("GET logs/_search?si").items).toEqual([])
  })

  it("stays quiet for unknown deeper segments", () => {
    expect(at("GET _cat/indices/x").items).toEqual([])
    expect(at("GET a/b/c").items).toEqual([])
  })
})

describe("body completion", () => {
  const request = (body: string) => `POST logs-2026/_search\n${body}`

  it("suggests keywords and fields at a key position", () => {
    const result = at(request('{\n  "'))
    expect(labels(result)).toEqual(expect.arrayContaining(["query", "aggs", "size", "message", "user.name"]))
    // at the top level the DSL keywords come before the fields
    expect(result.items.findIndex((i) => i.label === "query")).toBeLessThan(result.items.findIndex((i) => i.label === "message"))
  })

  it("puts fields first inside containers that take field names", () => {
    const result = at(request('{ "query": { "match": { "'))
    expect(result.items[0]).toMatchObject({ kind: "field" })
    expect(labels(result)).toContain("query")
  })

  it("filters by the typed text and replaces only it", () => {
    const result = at(request('{ "query": { "term": { "user.n'))
    expect(labels(result)).toEqual(["user.name"])
    expect(result.to - result.from).toBe("user.n".length)
  })

  it("suggests fields for the value of field-like properties", () => {
    const aggs = at(request('{ "aggs": { "by": { "terms": { "field": "'))
    expect(aggs.items.every((i) => i.kind === "field")).toBe(true)
    expect(labels(aggs)).toContain("message.keyword")

    expect(labels(at(request('{ "_source": ["mess')))).toEqual(["message", "message.keyword"])
    expect(labels(at(request('{ "sort": ["@')))).toEqual(["@timestamp"])
  })

  it("offers nothing for other values", () => {
    expect(at(request('{ "query": { "match": { "message": "')).items).toEqual([])
    expect(at(request('{ "query": { "terms": { "user.name": ["a", "')).items).toEqual([])
  })

  it("offers nothing outside a string", () => {
    expect(at(request('{ ')).items).toEqual([])
    expect(at(request('{ "query": ')).items).toEqual([])
  })

  it("restricts fields to the indices the path targets", () => {
    const metrics = at('POST metrics/_search\n{ "')
    expect(labels(metrics)).toEqual(expect.arrayContaining(["cpu", "host"]))
    expect(labels(metrics)).not.toContain("message")

    const wildcard = at('POST logs-*/_search\n{ "query": { "term": { "')
    expect(labels(wildcard)).toContain("user.name")
    expect(labels(wildcard)).not.toContain("cpu")

    const viaAlias = at('POST logs/_search\n{ "_source": ["')
    expect(labels(viaAlias)).toContain("@timestamp")
  })

  it("uses every index when the path has none, and none when the index is unknown", () => {
    expect(labels(at('POST _search\n{ "query": { "term": { "'))).toEqual(expect.arrayContaining(["cpu", "message"]))
    const unknown = at('POST nope/_search\n{ "query": { "term": { "')
    expect(unknown.items.filter((i) => i.kind === "field")).toEqual([])
  })

  it("finds the request even after a blank line, and keeps requests apart", () => {
    const text = 'GET _cat/indices\n\nPOST metrics/_search\n{ "query": { "term": { "'
    const result = at(text)
    expect(labels(result)).toEqual(expect.arrayContaining(["cpu"]))
    expect(labels(result)).not.toContain("message")
  })
})

describe("comments and prose", () => {
  it("never completes inside comments", () => {
    expect(at("# fix the quer").items).toEqual([])
    expect(at("  # POS").items).toEqual([])
  })
})

describe("scanJSON", () => {
  it("tracks strings, escapes and nesting", () => {
    expect(scanJSON('{ "a": "x\\"y').inString).toBe(true)
    expect(scanJSON('{ "a": "x\\"y"').inString).toBe(false)
    expect(scanJSON('{ "a": [ { "b": ').frames.map((f) => [f.bracket, f.key])).toEqual([
      ["{", null],
      ["[", "a"],
      ["{", null],
    ])
    expect(scanJSON('{ "a": { } }').frames).toEqual([])
    expect(scanJSON('{ "k": ').currentKey).toBe("k")
  })

  it("is not confused by unbalanced closing brackets", () => {
    expect(() => scanJSON("}}]]")).not.toThrow()
  })
})

describe("targetFields", () => {
  it("deduplicates fields across matching indices", () => {
    const merged = targetFields(schema, "_search")
    expect(merged.filter((f) => f.path === "host")).toHaveLength(1)
    expect(targetFields(undefined, "x")).toEqual([])
    expect(targetFields(schema, "/logs-2026,metrics/_search").map((f) => f.path)).toEqual(
      expect.arrayContaining(["message", "cpu"]),
    )
  })

  it("treats regular expression characters in an index name literally", () => {
    expect(targetFields(schema, "logs.2026/_search")).toEqual([])
  })
})
