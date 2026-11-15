// HTTP-Client für die BetterNAS-API.

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public details?: Record<string, unknown>,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

let csrfToken = ''
let onUnauthorized: () => void = () => {}

export function setCsrfToken(token: string) {
  csrfToken = token
}

export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

const SAFE = new Set(['GET', 'HEAD', 'OPTIONS'])

export async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method ?? 'GET').toUpperCase()
  const headers = new Headers(init.headers)
  if (init.body !== undefined && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  if (!SAFE.has(method) && csrfToken) headers.set('X-CSRF-Token', csrfToken)

  let res: Response
  try {
    res = await fetch(path, { ...init, method, headers, credentials: 'same-origin' })
  } catch {
    throw new ApiError(0, 'network', 'BetterNAS ist nicht erreichbar')
  }
  if (res.status === 204) return undefined as T

  const text = await res.text()
  let data: unknown
  try {
    data = text ? JSON.parse(text) : undefined
  } catch {
    data = undefined
  }
  if (!res.ok) {
    const e = (data as { error?: { code: string; message: string; details?: Record<string, unknown> } })?.error
    const err = new ApiError(res.status, e?.code ?? 'internal', e?.message ?? `Fehler ${res.status}`, e?.details)
    if (res.status === 401 && !path.endsWith('/auth/login')) onUnauthorized()
    throw err
  }
  return data as T
}

export const api = {
  get: <T>(path: string) => apiFetch<T>(path),
  post: <T>(path: string, body?: unknown) =>
    apiFetch<T>(path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) }),
  patch: <T>(path: string, body: unknown) => apiFetch<T>(path, { method: 'PATCH', body: JSON.stringify(body) }),
  del: <T>(path: string) => apiFetch<T>(path, { method: 'DELETE' }),
}
