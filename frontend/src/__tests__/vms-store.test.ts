import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useVmsStore } from '@/stores/vms'

const res = (body: unknown, status = 200) => Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))

function route(map: Record<string, unknown>) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const v = map[`${init?.method ?? 'GET'} ${url}`]
    return v === undefined ? res({ error: { code: 'not_found', message: 'x' } }, 404) : res(v, v === null ? 204 : 200)
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

const vm = { name: 'debian', uuid: 'u', state: 'running', vcpus: 2, memoryMiB: 4096, autostart: false, disks: [] }

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('VM-Store', () => {
  it('lädt Status und Liste', async () => {
    route({ 'GET /api/v1/vms/status': { installed: true, active: true, kvm: true }, 'GET /api/v1/vms': [vm] })
    const s = useVmsStore()
    await s.refresh()
    expect(s.vms).toHaveLength(1)
    expect(s.error).toBe('')
  })

  it('lädt bei inaktivem libvirt keine Liste', async () => {
    const fn = route({ 'GET /api/v1/vms/status': { installed: true, active: false, kvm: true } })
    const s = useVmsStore()
    await s.refresh()
    expect(s.vms).toEqual([])
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('aktualisiert nur bei vm.changed', async () => {
    const fn = route({ 'GET /api/v1/vms/status': { installed: true, active: true, kvm: true }, 'GET /api/v1/vms': [vm] })
    const s = useVmsStore()
    s.handleEvent({ type: 'docker.changed' })
    expect(fn).not.toHaveBeenCalled()
    s.handleEvent({ type: 'vm.changed' })
    await vi.waitFor(() => expect(s.vms).toHaveLength(1))
  })

  it('sendet Aktionen und lädt neu', async () => {
    const fn = route({
      'GET /api/v1/vms/status': { installed: true, active: true, kvm: true },
      'GET /api/v1/vms': [vm],
      'POST /api/v1/vms/debian/shutdown': null,
      'DELETE /api/v1/vms/debian?disks=1': null,
    })
    const s = useVmsStore()
    await s.act('debian', 'shutdown')
    await s.remove('debian', true)
    const calls = fn.mock.calls.map((c) => `${c[1]?.method ?? 'GET'} ${c[0]}`)
    expect(calls).toContain('POST /api/v1/vms/debian/shutdown')
    expect(calls).toContain('DELETE /api/v1/vms/debian?disks=1')
    expect(calls.filter((c) => c === 'GET /api/v1/vms')).toHaveLength(2)
  })

  it('pollt, solange die Seite offen ist', async () => {
    vi.useFakeTimers()
    const fn = route({ 'GET /api/v1/vms/status': { installed: true, active: true, kvm: true }, 'GET /api/v1/vms': [] })
    const s = useVmsStore()
    s.startPolling()
    await vi.advanceTimersByTimeAsync(10000)
    s.stopPolling()
    const n = fn.mock.calls.length
    await vi.advanceTimersByTimeAsync(10000)
    expect(n).toBeGreaterThan(0)
    expect(fn.mock.calls.length).toBe(n)
  })
})
