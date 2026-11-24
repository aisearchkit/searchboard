/** Error carrying the HTTP status and the server's message. */
export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
    /** The parsed JSON error body, for answers that carry more than a message (such as 428). */
    public readonly body?: unknown,
  ) {
    super(message)
    this.name = "ApiError"
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    ...init,
    headers: init?.body ? { "Content-Type": "application/json", ...init.headers } : init?.headers,
  })

  if (!response.ok) {
    let message = `${response.status} ${response.statusText}`
    let body: unknown
    try {
      body = await response.json()
      const error = (body as { error?: string } | null)?.error
      if (error) message = error
    } catch {
      // Keep the status line when the body is not JSON.
    }
    throw new ApiError(response.status, message, body)
  }

  if (response.status === 204) return undefined as T
  return (await response.json()) as T
}
