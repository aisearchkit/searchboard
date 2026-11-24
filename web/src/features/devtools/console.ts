// Parser for the console text: requests like
//
//   GET _cat/indices?v
//
//   POST logs-*/_search
//   { "query": { "match_all": {} } }

export const METHODS = ["GET", "POST", "PUT", "DELETE", "HEAD", "PATCH"] as const
export type Method = (typeof METHODS)[number]

export interface ConsoleRequest {
  method: Method
  path: string
  body: string
  /** 1-based line of the request line. */
  startLine: number
  /** 1-based last line that belongs to the request (the last body line). */
  endLine: number
}

const REQUEST_LINE = /^(GET|POST|PUT|DELETE|HEAD|PATCH)\s+(\S.*?)\s*$/i

/** Splits console text into requests. Comment lines (starting with #) are ignored. */
export function parseRequests(text: string): ConsoleRequest[] {
  const lines = text.split(/\r?\n/)
  const requests: ConsoleRequest[] = []
  let current: { method: Method; path: string; start: number; body: string[]; lastContent: number } | null = null

  const finish = () => {
    if (!current) return
    requests.push({
      method: current.method,
      path: current.path,
      body: current.body.join("\n").trim(),
      startLine: current.start,
      endLine: current.lastContent,
    })
    current = null
  }

  lines.forEach((line, index) => {
    const lineNumber = index + 1
    const match = REQUEST_LINE.exec(line)
    if (match) {
      finish()
      current = {
        method: match[1].toUpperCase() as Method,
        path: match[2],
        start: lineNumber,
        body: [],
        lastContent: lineNumber,
      }
      return
    }
    if (!current) return
    if (line.startsWith("#")) return // a comment never belongs to a body
    current.body.push(line)
    if (line.trim() !== "") current.lastContent = lineNumber
  })
  finish()
  return requests
}

/**
 * Returns the request the cursor belongs to: the last request that starts on or
 * before the line. Blank lines after a request still belong to it, like in the
 * Kibana console.
 */
export function requestAtLine(requests: ConsoleRequest[], line: number): ConsoleRequest | null {
  let found: ConsoleRequest | null = null
  for (const request of requests) {
    if (request.startLine <= line) found = request
  }
  return found
}

const NDJSON_ENDPOINT = /(?:^|\/)(?:_bulk|_msearch)(?:\?|$)/

/**
 * Returns the body to send. The bulk-style endpoints require newline-delimited
 * JSON that ends with a newline.
 */
export function bodyForSending(request: ConsoleRequest): string {
  const path = request.path.replace(/^\//, "")
  if (request.body !== "" && NDJSON_ENDPOINT.test(path) && !request.body.endsWith("\n")) {
    return request.body + "\n"
  }
  return request.body
}

/** Formats a request the way it is typed in the editor. */
export function formatRequest(method: string, path: string, body: string): string {
  return body ? `${method} ${path}\n${body}` : `${method} ${path}`
}
