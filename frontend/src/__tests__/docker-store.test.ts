import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { STACK_NAME_RE } from '@/api/docker'
import { useDockerStore } from '@/stores/docker'
import { useJobsStore, type Job } from '@/stores/jobs'

const res = (body: unknown, status = 200) => Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))

function route(map: Record<string, unknown | (() => Promise<Response>)>) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    const v = map[key]
    if (v === undefined) return res({ error: { code: 'not_found', message: 'x' } }, 404)
    return typeof v === 'function' ? (v as () => Promise<Response>)() : res(v)
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => vi.unstubAllGlobals())

describe('Docker-Store: Speicherorte', () => {
  it('lädt bei nicht eingerichtetem Docker weder Stacks noch Container', async () => {
    const fn = route({ 'GET /api/v1/docker/status': { installed: true, active: false, configured: false, version: '' } })
    const s = useDockerStore()
    await s.refresh()
    expect(s.configured).toBe(false)
    expect(s.stacks).toEqual([])
    expect(fn.mock.calls.map((c) => c[0])).toEqual(['/api/v1/docker/status'])
    expect(s.error).toBe('')
  })

  it('speichert die Speicherorte und gibt sie an die Vorlagen weiter', async () => {
    const { roots, setRoots } = await import('@/utils/appTemplates')
    const before = { ...roots }
    const paths = { configured: true, appdata: '/srv/pools/a/cfg', dockerdata: '/srv/pools/b/dd', warnings: [], staleStacks: 0 }
    route({
      'PUT /api/v1/docker/paths': paths,
      'GET /api/v1/docker/status': { installed: true, active: true, configured: true, version: '26' },
      'GET /api/v1/stacks': [],
      'GET /api/v1/containers': [],
    })
    const s = useDockerStore()
    const r = await s.savePaths({ appdata: '/srv/pools/a/cfg', dockerdata: '/srv/pools/b/dd' })
    expect(r.configured).toBe(true)
    expect(s.configured).toBe(true)
    expect(roots.appdata).toBe('/srv/pools/a/cfg')
    expect(roots.dockerdata).toBe('/srv/pools/b/dd')
    setRoots(before)
  })
})

describe('Docker-Store', () => {
  it('lädt Status, Stacks und Container', async () => {
    route({
      'GET /api/v1/docker/status': { installed: true, active: true, version: '26' },
      'GET /api/v1/stacks': [{ name: 'media', running: 1, total: 2 }],
      'GET /api/v1/containers': [{ id: 'a', name: 'web', image: 'nginx', state: 'running', status: 'Up', stack: 'media', ports: [], created: '' }],
    })
    const s = useDockerStore()
    await s.refresh()
    expect(s.stacks).toHaveLength(1)
    expect(s.containers[0].name).toBe('web')
    expect(s.error).toBe('')
  })

  it('lädt bei inaktivem Docker nur die Stacks', async () => {
    const fn = route({
      'GET /api/v1/docker/status': { installed: true, active: false, version: '' },
      'GET /api/v1/stacks': [{ name: 'media', running: 0, total: 0 }],
    })
    const s = useDockerStore()
    await s.refresh()
    expect(s.stacks).toHaveLength(1)
    expect(s.containers).toEqual([])
    expect(fn).not.toHaveBeenCalledWith('/api/v1/containers', expect.anything())
  })

  it('meldet Fehler', async () => {
    route({ 'GET /api/v1/docker/status': () => res({ error: { code: 'internal', message: 'kaputt' } }, 500) })
    const s = useDockerStore()
    await s.refresh()
    expect(s.error).toBe('kaputt')
  })

  it('run wartet auf den Auftrag und lädt danach neu', async () => {
    const fn = route({
      'POST /api/v1/stacks/media/up': () => res({ jobId: 'j1' }, 202),
      'GET /api/v1/docker/status': { installed: true, active: false, version: '' },
      'GET /api/v1/stacks': [],
    })
    const s = useDockerStore()
    const jobs = useJobsStore()
    const p = s.run('media', 'up')
    await vi.waitFor(() => expect(fn).toHaveBeenCalledWith('/api/v1/stacks/media/up', expect.anything()))
    jobs.update({ id: 'j1', kind: 'stack.up', target: 'media', status: 'succeeded', progress: 100, log: '', startedBy: 'a', startedAt: '' } as Job)
    await p
    expect(fn).toHaveBeenCalledWith('/api/v1/stacks', expect.anything())
  })

  it('handleEvent lädt bei docker.changed neu', async () => {
    const fn = route({ 'GET /api/v1/docker/status': { installed: false, active: false, version: '' }, 'GET /api/v1/stacks': [] })
    const s = useDockerStore()
    s.handleEvent({ type: 'other' } as never)
    expect(fn).not.toHaveBeenCalled()
    s.handleEvent({ type: 'docker.changed' } as never)
    await vi.waitFor(() => expect(fn).toHaveBeenCalled())
  })
})

describe('Stackname', () => {
  it('entspricht den Backend-Regeln', () => {
    for (const ok of ['jellyfin', 'a_b-1']) expect(STACK_NAME_RE.test(ok)).toBe(true)
    for (const bad of ['', '../x', 'A', 'a b', '-a', 'a'.repeat(41)]) expect(STACK_NAME_RE.test(bad)).toBe(false)
  })
})
