import { describe, expect, it } from "vitest"
import { bodyForSending, formatRequest, parseRequests, requestAtLine } from "./console"

const sample = `# cluster state
GET _cluster/health

POST logs-*/_search?size=1
{
  "query": { "match_all": {} }
}


put /logs/_doc/1
{"a": 1}
DELETE logs/_doc/1`

describe("parseRequests", () => {
  const requests = parseRequests(sample)

  it("finds every request with normalized methods", () => {
    expect(requests.map((r) => `${r.method} ${r.path}`)).toEqual([
      "GET _cluster/health",
      "POST logs-*/_search?size=1",
      "PUT /logs/_doc/1",
      "DELETE logs/_doc/1",
    ])
  })

  it("collects bodies and trims blank lines around them", () => {
    expect(requests[0].body).toBe("")
    expect(JSON.parse(requests[1].body)).toEqual({ query: { match_all: {} } })
    expect(requests[2].body).toBe('{"a": 1}')
    expect(requests[3].body).toBe("")
  })

  it("records line ranges ending at the last content line", () => {
    expect(requests.map((r) => [r.startLine, r.endLine])).toEqual([
      [2, 2],
      [4, 7],
      [10, 11],
      [12, 12],
    ])
  })

  it("ignores comments, even between a request and the next one", () => {
    const parsed = parseRequests("GET a\n# note\nGET b")
    expect(parsed.map((r) => [r.path, r.body])).toEqual([
      ["a", ""],
      ["b", ""],
    ])
  })

  it("keeps a method word inside a body line from starting a request", () => {
    const parsed = parseRequests('POST x\n{\n  "q": "GET me"\n}')
    expect(parsed).toHaveLength(1)
    expect(parsed[0].body).toContain('"GET me"')
  })

  it("handles Windows line endings and empty input", () => {
    expect(parseRequests("GET a\r\n{}\r\n")[0].body).toBe("{}")
    expect(parseRequests("")).toEqual([])
    expect(parseRequests("   \n\n")).toEqual([])
    expect(parseRequests("not a request")).toEqual([])
  })

  it("does not treat a bare method as a request", () => {
    expect(parseRequests("GET")).toEqual([])
    expect(parseRequests("GET   ")).toEqual([])
  })
})

describe("requestAtLine", () => {
  const requests = parseRequests(sample)

  it("picks the request that contains or precedes the line", () => {
    expect(requestAtLine(requests, 2)?.path).toBe("_cluster/health")
    expect(requestAtLine(requests, 3)?.path).toBe("_cluster/health") // blank line after a request
    expect(requestAtLine(requests, 6)?.path).toBe("logs-*/_search?size=1")
    expect(requestAtLine(requests, 12)?.path).toBe("logs/_doc/1")
  })

  it("returns null before the first request", () => {
    expect(requestAtLine(requests, 1)).toBeNull()
    expect(requestAtLine([], 5)).toBeNull()
  })
})

describe("bodyForSending", () => {
  const make = (path: string, body: string) => ({ method: "POST" as const, path, body, startLine: 1, endLine: 1 })

  it("adds the trailing newline that bulk endpoints require", () => {
    expect(bodyForSending(make("_bulk", '{"index":{}}\n{"a":1}'))).toBe('{"index":{}}\n{"a":1}\n')
    expect(bodyForSending(make("/logs/_bulk?refresh=true", "{}"))).toBe("{}\n")
    expect(bodyForSending(make("_msearch", "{}\n{}"))).toBe("{}\n{}\n")
  })

  it("leaves other bodies alone", () => {
    expect(bodyForSending(make("logs/_search", '{"q":1}'))).toBe('{"q":1}')
    expect(bodyForSending(make("my_bulk_index/_doc", "{}"))).toBe("{}")
    expect(bodyForSending(make("_bulk", ""))).toBe("")
  })
})

describe("formatRequest", () => {
  it("round-trips through the parser", () => {
    const text = formatRequest("POST", "logs/_search", '{"size":1}')
    expect(parseRequests(text)).toMatchObject([{ method: "POST", path: "logs/_search", body: '{"size":1}' }])
    expect(formatRequest("GET", "_cat", "")).toBe("GET _cat")
  })
})
