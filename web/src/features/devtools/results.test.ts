import { describe, expect, it } from "vitest"
import { chartFrom, flatten, prettyJSON, tableFrom, toCSV } from "./results"

const searchResponse = {
  hits: {
    total: { value: 2 },
    hits: [
      { _index: "logs", _id: "1", _score: 1.5, _source: { message: "boom", user: { name: "ada", tags: ["a", "b"] } } },
      { _index: "logs", _id: "2", _score: null, _source: { message: "ok", level: "info" } },
    ],
  },
}

describe("flatten", () => {
  it("uses dotted keys and keeps arrays as JSON text", () => {
    expect(flatten({ a: { b: 1, c: [1, 2] }, d: null })).toEqual({ "a.b": 1, "a.c": "[1,2]", d: null })
  })
})

describe("tableFrom", () => {
  it("turns search hits into rows with flattened sources", () => {
    const table = tableFrom(searchResponse)!
    expect(table.columns).toEqual(["_index", "_id", "_score", "message", "user.name", "user.tags", "level"])
    expect(table.rows[0]).toMatchObject({ _index: "logs", _id: "1", _score: 1.5, message: "boom", "user.name": "ada", "user.tags": '["a","b"]' })
    expect(table.rows[1]).not.toHaveProperty("_score") // a null score is dropped
  })

  it("turns an array of objects (such as _cat output) into rows", () => {
    const table = tableFrom([
      { index: "a", "docs.count": "5" },
      { index: "b", health: "green" },
    ])!
    expect(table.columns).toEqual(["index", "docs.count", "health"])
    expect(table.rows).toHaveLength(2)
  })

  it("turns aggregation buckets into rows", () => {
    const table = tableFrom({
      hits: { hits: [] },
      aggregations: {
        by_level: {
          buckets: [
            { key: "error", doc_count: 3, avg_time: { value: 1.5 } },
            { key: "info", doc_count: 9, avg_time: { value: 0.5 } },
          ],
        },
      },
    })!
    expect(table.columns).toEqual(["key", "doc_count", "avg_time.value"])
    expect(table.rows[1]).toMatchObject({ key: "info", doc_count: 9, "avg_time.value": 0.5 })
  })

  it("supports keyed buckets and date histograms", () => {
    const keyed = tableFrom({ aggregations: { f: { buckets: { errors: { doc_count: 2 }, warns: { doc_count: 1 } } } } })!
    expect(keyed.rows.map((r) => r.key)).toEqual(["errors", "warns"])

    const dates = tableFrom({ aggregations: { h: { buckets: [{ key: 1700000000000, key_as_string: "2023-11-14", doc_count: 4 }] } } })!
    expect(dates.rows[0]).toMatchObject({ key: 1700000000000, key_as_string: "2023-11-14", doc_count: 4 })
  })

  it("returns null when nothing is tabular", () => {
    for (const value of [null, 5, "text", {}, [], [1, 2], { acknowledged: true }, { hits: { hits: [] } }]) {
      expect(tableFrom(value)).toBeNull()
    }
  })

  it("caps the number of columns", () => {
    const wide = Object.fromEntries(Array.from({ length: 500 }, (_, i) => [`f${i}`, i]))
    expect(tableFrom([wide])!.columns).toHaveLength(200)
  })
})

describe("chartFrom", () => {
  it("builds bars from the first bucket aggregation", () => {
    const chart = chartFrom({
      aggregations: {
        per_day: {
          buckets: [
            { key: 1, key_as_string: "2026-01-01", doc_count: 4 },
            { key: 2, doc_count: 7 },
            { key: 3 }, // no doc_count: skipped
          ],
        },
      },
    })!
    expect(chart.title).toBe("per_day")
    expect(chart.bars).toEqual([
      { label: "2026-01-01", value: 4 },
      { label: "2", value: 7 },
    ])
  })

  it("returns null without bucket aggregations", () => {
    expect(chartFrom({ aggregations: { avg_x: { value: 1 } } })).toBeNull()
    expect(chartFrom(searchResponse)).toBeNull()
    expect(chartFrom([1, 2])).toBeNull()
    expect(chartFrom({ aggregations: { a: { buckets: [] } } })).toBeNull()
  })
})

describe("toCSV", () => {
  it("quotes cells and doubles quotes (RFC 4180)", () => {
    const csv = toCSV({
      columns: ["a", "b"],
      rows: [
        { a: 'say "hi"', b: "line1\nline2" },
        { a: "x,y", b: 5 },
        { a: null, b: true },
      ],
    })
    expect(csv).toBe('a,b\r\n"say ""hi""","line1\nline2"\r\n"x,y",5\r\n,true\r\n')
  })

  it("neutralizes formulas so a spreadsheet cannot run them", () => {
    const csv = toCSV({ columns: ["v"], rows: [{ v: "=HYPERLINK(\"http://evil\")" }, { v: "+1" }, { v: "-cmd" }, { v: "@SUM(A1)" }, { v: -5 }, { v: "plain" }] })
    const cells = csv.trim().split("\r\n").slice(1)
    expect(cells[0]).toBe(`"'=HYPERLINK(""http://evil"")"`)
    expect(cells.slice(1, 4)).toEqual(["'+1", "'-cmd", "'@SUM(A1)"])
    expect(cells[4]).toBe("-5") // a real number is not text and stays numeric
    expect(cells[5]).toBe("plain")
  })

  it("serializes nested values as JSON", () => {
    expect(toCSV({ columns: ["o"], rows: [{ o: { a: 1 } }] })).toBe('o\r\n"{""a"":1}"\r\n')
  })
})

describe("prettyJSON", () => {
  it("pretty-prints valid JSON", () => {
    expect(prettyJSON('{"a":[1,2]}')).toMatchObject({ isJSON: true, value: { a: [1, 2] } })
    expect(prettyJSON('{"a":1}').text).toBe('{\n  "a": 1\n}')
  })

  it("returns other text unchanged", () => {
    expect(prettyJSON("green open logs 1 1")).toEqual({ text: "green open logs 1 1", isJSON: false })
    expect(prettyJSON("")).toEqual({ text: "", isJSON: false })
  })

  it("does not parse huge bodies", () => {
    const huge = '{"a":"' + "x".repeat(100) + '"}'
    expect(prettyJSON(huge, 50)).toEqual({ text: huge, isJSON: false })
  })
})
