import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import FormatDialog from '@/components/storage/FormatDialog.vue'
import PoolDialog from '@/components/storage/PoolDialog.vue'
import SnapraidDialog from '@/components/storage/SnapraidDialog.vue'
import SnapraidPanel from '@/components/storage/SnapraidPanel.vue'
import DisksView from '@/views/storage/DisksView.vue'
import DashboardView from '@/views/DashboardView.vue'
import { useStorageStore } from '@/stores/storage'
import { useJobsStore } from '@/stores/jobs'
import type { PoolView, SnapraidStatus } from '@/api/storage'
import { disks, parityDisks, pools, preview, snapraidDefaults } from './fixtures'

const plugins = () => [createPinia(), PrimeVue, ToastService]

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

const snapPool: PoolView = { ...pools[0], snapraid: { parity: ['p1'], ...snapraidDefaults } }

describe('FormatDialog: Rolle', () => {
  it('sendet role parity, wenn „Paritätsplatte“ gewählt ist', async () => {
    const w = mount(FormatDialog, { props: { visible: true, preview }, global: { plugins: plugins() } })
    await w.find('input#role-parity').setValue(true)
    await w.find('input#confirm-serial').setValue('WD-WCC7K1234567')
    await w.find('button[data-test="format-submit"]').trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([{ fsType: 'ext4', role: 'parity', confirmSerial: 'WD-WCC7K1234567', confirmToken: 'tok' }])
  })
})

describe('DisksView: Paritätsplatten', () => {
  it('bietet „Freigeben“ für unbenutzte Paritätsplatten, nicht für benutzte', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string) =>
        new Response(JSON.stringify(url.includes('/disks') ? [...disks, ...parityDisks] : pools), { status: 200 }),
      ),
    )
    const w = mount(DisksView, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="disk-p1"]').text()).toContain('Freigeben')
    expect(w.find('[data-test="disk-p4"]').text()).not.toContain('Freigeben')
  })
})

describe('PoolDialog: SnapRAID', () => {
  it('sperrt das Entfernen vorhandener Platten, solange SnapRAID aktiv ist', () => {
    const w = mount(PoolDialog, { props: { visible: true, disks, pools: [snapPool], pool: snapPool }, global: { plugins: plugins() } })
    const boxes = w.findAll('[data-test="branch-option"] input')
    const own = w.find('[data-test="branch-option"][data-id="d1"] input')
    expect(own.attributes('disabled')).toBeDefined()
    expect(boxes.length).toBeGreaterThan(1)
    expect(w.text()).toContain('SnapRAID zuerst deaktivieren')
  })
})

describe('SnapraidDialog', () => {
  const mountDialog = (props = {}) =>
    mount(SnapraidDialog, {
      props: { visible: true, pool: pools[0], disks: [...disks, ...parityDisks], ...props },
      global: { plugins: plugins() },
    })

  it('bietet nur freie Paritätsplatten an und übernimmt die Standardwerte', async () => {
    const w = mountDialog()
    const ids = w.findAll('[data-test="parity-option"]').map((o) => o.attributes('data-id'))
    expect(ids).toEqual(['p1', 'p2', 'p3'])
    expect((w.find('input#sr-sync').element as HTMLInputElement).value).toBe('*-*-* 03:00:00')
    expect((w.find('input#sr-scrub').element as HTMLInputElement).value).toBe('Sun *-*-* 05:00:00')
    expect((w.find('input#sr-percent').element as HTMLInputElement).value).toBe('8')
    expect((w.find('input#sr-days').element as HTMLInputElement).value).toBe('10')
    expect((w.find('input#sr-threshold').element as HTMLInputElement).value).toBe('500')
    const submit = () => w.find('button[data-test="snapraid-submit"]')
    expect(submit().attributes('disabled')).toBeDefined()
    await w.find('[data-test="parity-option"][data-id="p1"] input').setValue(true)
    expect(submit().attributes('disabled')).toBeUndefined()
    await submit().trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([{ parity: ['p1'], ...snapraidDefaults }])
  })

  it('erlaubt höchstens zwei Paritätsplatten', async () => {
    const w = mountDialog()
    for (const id of ['p1', 'p3']) await w.find(`[data-test="parity-option"][data-id="${id}"] input`).setValue(true)
    expect(w.find('[data-test="parity-option"][data-id="p2"] input').attributes('disabled')).toBeDefined()
  })

  it('warnt, wenn eine Paritätsplatte kleiner als die größte Datenplatte ist, und sperrt das Speichern', async () => {
    const w = mountDialog()
    await w.find('[data-test="parity-option"][data-id="p2"] input').setValue(true)
    expect(w.find('[data-test="parity-warning"]').exists()).toBe(true)
    expect(w.find('button[data-test="snapraid-submit"]').attributes('disabled')).toBeDefined()
  })

  it('füllt beim Ändern die vorhandenen Einstellungen ein', () => {
    const w = mountDialog({ pool: { ...snapPool, snapraid: { parity: ['p1'], ...snapraidDefaults, deleteThreshold: 42 } } })
    expect((w.find('input#sr-threshold').element as HTMLInputElement).value).toBe('42')
    expect((w.find('[data-test="parity-option"][data-id="p1"] input').element as HTMLInputElement).checked).toBe(true)
  })
})

describe('SnapraidPanel', () => {
  const status = (over: Partial<SnapraidStatus> = {}): SnapraidStatus => ({ enabled: true, running: false, ...over })
  const stubStatus = (list: SnapraidStatus[]) => {
    const f = vi.fn().mockImplementation(async () => {
      const s = list.length > 1 ? list.shift()! : list[0]
      return new Response(JSON.stringify(s), { status: 200 })
    })
    vi.stubGlobal('fetch', f)
    return f
  }
  const mountPanel = (pool = snapPool) => mount(SnapraidPanel, { props: { pool }, global: { plugins: plugins() } })

  it('bietet das Einrichten an, solange SnapRAID aus ist', async () => {
    const w = mountPanel(pools[0])
    await w.find('button[data-test="snapraid-setup"]').trigger('click')
    expect(w.emitted('configure')).toBeTruthy()
  })

  it('zeigt letzten Sync, ungeschützte Änderungen und Prüfung', async () => {
    stubStatus([
      status({
        lastSync: { action: 'sync', result: 'ok', at: '2026-11-16T03:00:00Z' },
        lastScrub: { action: 'scrub', result: 'errors', message: 'Die Prüfung hat Fehler gefunden.', at: '2026-11-16T05:00:00Z' },
        diff: { equal: 10, added: 3, removed: 1, updated: 2, moved: 0, copied: 0 },
      }),
    ])
    const w = mountPanel()
    await flushPromises()
    expect(w.find('[data-test="snapraid-unprotected"]').text()).toContain('6')
    expect(w.text()).toContain('Letzter Sync')
    expect(w.text()).toContain('Fehler gefunden')
  })

  it('zeigt bei Massenlöschung die Warnung und startet mit „Trotzdem synchronisieren“ force', async () => {
    stubStatus([status({ lastSync: { action: 'sync', result: 'blocked', message: 'x', removed: 501, at: '2026-11-16T03:00:00Z' } })])
    const w = mountPanel()
    await flushPromises()
    expect(w.text()).toContain('Sync gestoppt: 501 Dateien gelöscht')
    await w.find('button[data-test="snapraid-force"]').trigger('click')
    expect(w.emitted('run')?.[0]).toEqual(['sync', true])
  })

  it('deaktiviert Aktionen während ein Lauf aktiv ist und fragt dann alle 5 s nach', async () => {
    vi.useFakeTimers()
    const f = stubStatus([status({ running: true }), status({ running: true }), status({ running: false })])
    const w = mountPanel()
    await flushPromises()
    expect(w.find('button[data-test="snapraid-sync"]').attributes('disabled')).toBeDefined()
    expect(f).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(5000)
    expect(f).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(5000)
    expect(f).toHaveBeenCalledTimes(3)
    await flushPromises()
    expect(w.find('button[data-test="snapraid-sync"]').attributes('disabled')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(20000)
    expect(f).toHaveBeenCalledTimes(3) // kein Polling mehr, wenn nichts läuft
  })

  it('meldet die Aktionen an den Elternknoten', async () => {
    stubStatus([status()])
    const w = mountPanel()
    await flushPromises()
    await w.find('button[data-test="snapraid-sync"]').trigger('click')
    await w.find('button[data-test="snapraid-scrub"]').trigger('click')
    await w.find('button[data-test="snapraid-diff"]').trigger('click')
    await w.find('button[data-test="snapraid-edit"]').trigger('click')
    await w.find('button[data-test="snapraid-disable"]').trigger('click')
    expect(w.emitted('run')).toEqual([['sync', false], ['scrub', false], ['diff', false]])
    expect(w.emitted('configure')).toBeTruthy()
    expect(w.emitted('disable')).toBeTruthy()
  })
})

describe('storage store: SnapRAID', () => {
  beforeEach(() => setActivePinia(createPinia()))

  async function finishJob(id: string) {
    useJobsStore().update({ id, kind: 'x', target: 'media', status: 'succeeded', progress: 100, log: '', startedBy: 'a', startedAt: '2026-11-16T00:00:00Z' })
  }

  it('richtet SnapRAID per PUT ein und wartet auf den Auftrag', async () => {
    const f = vi.fn().mockImplementation(async (url: string, init?: RequestInit) =>
      new Response(init?.method === 'PUT' ? '{"jobId":"j1"}' : '[]', { status: init?.method === 'PUT' ? 202 : 200 }),
    )
    vi.stubGlobal('fetch', f)
    const store = useStorageStore()
    const p = store.enableSnapraid('media', { parity: ['p1'], ...snapraidDefaults })
    await flushPromises()
    await finishJob('j1')
    await p
    const call = f.mock.calls.find((c) => c[1]?.method === 'PUT')!
    expect(call[0]).toBe('/api/v1/pools/media/snapraid')
    expect(JSON.parse(call[1].body)).toEqual({ parity: ['p1'], ...snapraidDefaults })
  })

  it('startet Läufe mit force und schaltet SnapRAID ab', async () => {
    const f = vi.fn().mockImplementation(async (url: string, init?: RequestInit) =>
      new Response(init?.method && init.method !== 'GET' ? '{"jobId":"j2"}' : '[]', { status: init?.method && init.method !== 'GET' ? 202 : 200 }),
    )
    vi.stubGlobal('fetch', f)
    const store = useStorageStore()
    let p = store.runSnapraid('media', 'sync', true)
    await flushPromises()
    await finishJob('j2')
    await p
    const run = f.mock.calls.find((c) => c[0] === '/api/v1/pools/media/snapraid/sync')!
    expect(JSON.parse(run[1].body)).toEqual({ force: true })
    p = store.disableSnapraid('media')
    await flushPromises()
    await finishJob('j2')
    await p
    expect(f.mock.calls.some((c) => c[0] === '/api/v1/pools/media/snapraid' && c[1]?.method === 'DELETE')).toBe(true)
  })

  it('sendet beim Übernehmen die Rolle', async () => {
    const f = vi.fn().mockImplementation(async (url: string, init?: RequestInit) =>
      new Response(init?.method === 'POST' ? '{"jobId":"j3"}' : '[]', { status: 200 }),
    )
    vi.stubGlobal('fetch', f)
    const store = useStorageStore()
    const p = store.importDisk('d3', 'parity')
    await flushPromises()
    await finishJob('j3')
    await p
    const call = f.mock.calls.find((c) => String(c[0]).endsWith('/import'))!
    expect(JSON.parse(call[1].body)).toEqual({ role: 'parity' })
  })
})

describe('DashboardView: SnapRAID', () => {
  it('zeigt SnapRAID-Pools mit letztem Sync und ungeschützten Änderungen, sonst nichts', async () => {
    const info = { hostname: 'nas', kernel: 'k', os: 'o', uptimeSeconds: 10, cpuCount: 2, memTotalBytes: 2e9, memAvailableBytes: 1e9, version: '0.2.0' }
    const st = { enabled: true, running: false, lastSync: { action: 'sync', result: 'ok', at: '2026-11-16T03:00:00Z' }, diff: { equal: 1, added: 4, removed: 0, updated: 0, moved: 0, copied: 0 } }
    const route = (withSnap: boolean) =>
      vi.fn().mockImplementation(async (url: string) => {
        const body = url.includes('/snapraid/status') ? st : url.includes('/disks') ? [] : url.includes('/pools') ? (withSnap ? [snapPool] : pools) : info
        return new Response(JSON.stringify(body), { status: 200 })
      })
    vi.stubGlobal('fetch', route(true))
    const w = mount(DashboardView, { global: { plugins: plugins(), stubs: { RouterLink: true } } })
    await flushPromises()
    const tile = w.find('[data-test="snapraid-summary"]')
    expect(tile.exists()).toBe(true)
    expect(tile.text()).toContain('media')
    expect(tile.text()).toContain('4')
    w.unmount()
    vi.stubGlobal('fetch', route(false))
    const w2 = mount(DashboardView, { global: { plugins: plugins(), stubs: { RouterLink: true } } })
    await flushPromises()
    expect(w2.find('[data-test="snapraid-summary"]').exists()).toBe(false)
  })
})
