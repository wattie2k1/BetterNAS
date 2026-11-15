import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, apiFetch, setCsrfToken, setUnauthorizedHandler } from '@/api/client'

function mockFetch(status: number, body: unknown) {
  const fn = vi.fn().mockImplementation(async () =>
    new Response(body === undefined ? null : JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  )
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => {
  vi.unstubAllGlobals()
  setCsrfToken('')
})

describe('apiFetch', () => {
  it('sendet das CSRF-Token bei POST, nicht bei GET', async () => {
    setCsrfToken('tok123')
    const fn = mockFetch(200, { ok: true })
    await apiFetch('/api/v1/x', { method: 'POST', body: '{}' })
    expect(new Headers(fn.mock.calls[0][1].headers).get('X-CSRF-Token')).toBe('tok123')
    await apiFetch('/api/v1/x')
    expect(new Headers(fn.mock.calls[1][1].headers).get('X-CSRF-Token')).toBeNull()
  })

  it('wandelt das Fehlerformat in ApiError', async () => {
    mockFetch(409, { error: { code: 'pool_busy', message: 'Pool wird verwendet' } })
    const err = (await apiFetch("/api/v1/pools/a", { method: "PATCH" }).catch((e) => e)) as ApiError
    expect(err).toBeInstanceOf(ApiError)
    expect(err.code).toBe('pool_busy')
    expect(err.status).toBe(409)
    expect(err.message).toBe('Pool wird verwendet')
  })

  it('ruft bei 401 den Abmelde-Handler auf', async () => {
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    mockFetch(401, { error: { code: 'unauthorized', message: 'Bitte anmelden' } })
    await apiFetch('/api/v1/system/info').catch(() => {})
    expect(handler).toHaveBeenCalled()
    setUnauthorizedHandler(() => {})
  })

  it('liefert undefined bei 204', async () => {
    mockFetch(204, undefined)
    expect(await apiFetch('/api/v1/auth/logout', { method: 'POST' })).toBeUndefined()
  })
})
