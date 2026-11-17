import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import ReplaceDiskDialog from '@/components/storage/ReplaceDiskDialog.vue'
import SnapraidPanel from '@/components/storage/SnapraidPanel.vue'
import PoolsView from '@/views/storage/PoolsView.vue'
import { useStorageStore } from '@/stores/storage'
import type { PoolView, SnapraidStatus } from '@/api/storage'
import { disk, snapraidDefaults } from './fixtures'

afterEach(() => vi.unstubAllGlobals())

const pool: PoolView = {
  name: 'media', branches: ['a1', 'a2'], createPolicy: 'epmfs', minFree: '20G', path: '/srv/pools/media',
  sizeBytes: 8e12, freeBytes: 4e12, mounted: true, snapraid: { parity: ['p1'], ...snapraidDefaults },
}
const disks = [
  disk({ id: 'a1', device: '', model: 'Alt-HDD', serial: 'A1', role: 'data', pool: 'media', state: 'missing', sizeBytes: 4e12 }),
  disk({ id: 'a2', device: '/dev/sdb', model: 'Gut-HDD', serial: 'A2', role: 'data', pool: 'media', sizeBytes: 4e12 }),
  disk({ id: 'n1', device: '/dev/sdc', model: 'Neu-HDD', serial: 'N1', role: 'data', sizeBytes: 4e12 }),
  disk({ id: 'n2', device: '/dev/sdd', model: 'Klein-HDD', serial: 'N2', role: 'data', sizeBytes: 2e12 }),
  disk({ id: 'n3', device: '/dev/sde', model: 'Parität-HDD', serial: 'N3', role: 'parity', sizeBytes: 4e12 }),
  disk({ id: 'n4', device: '/dev/sdf', model: 'Leer-HDD', serial: 'N4', role: 'unassigned', sizeBytes: 4e12 }),
]
const plugins = () => [createPinia(), PrimeVue, ToastService]

const mountDialog = (over: Record<string, unknown> = {}) =>
  mount(ReplaceDiskDialog, { props: { visible: true, pool, disks, ...over }, global: { plugins: plugins() } })

const ids = (w: ReturnType<typeof mountDialog>, kind: 'old' | 'new') => w.findAll(`[data-test="${kind}-option"]`).map((e) => e.attributes('data-id'))

describe('ReplaceDiskDialog', () => {
  it('wählt die fehlende Platte vor und bietet als Ersatz nur passende Datenplatten an', async () => {
    const w = mountDialog()
    expect(ids(w, 'old')).toEqual(['a1', 'a2'])
    expect((w.find('[data-test="old-option"][data-id="a1"] input').element as HTMLInputElement).checked).toBe(true)
    await w.find('[data-test="replace-next"]').trigger('click')
    // n1: passt · n2: zu klein · n3: Parität · n4: nicht formatiert · a2: bereits im Pool
    expect(ids(w, 'new')).toEqual(['n1'])
    expect(w.find('[data-test="no-candidates"]').exists()).toBe(false)
  })

  it('sperrt „Weiter“ ohne Auswahl und erklärt, wenn keine Ersatzplatte passt', async () => {
    const w = mountDialog({ disks: disks.filter((d) => d.id !== 'n1') })
    await w.find('[data-test="replace-next"]').trigger('click')
    expect(ids(w, 'new')).toEqual([])
    expect(w.find('[data-test="no-candidates"]').text()).toContain('Datenträger')
    expect(w.find('[data-test="replace-next"]').attributes('disabled')).toBeDefined()
  })

  it('verlangt ohne fehlende Platte eine bewusste Auswahl', () => {
    const w = mountDialog({ disks: disks.map((d) => (d.id === 'a1' ? { ...d, state: 'online' as const } : d)) })
    expect(w.find('[data-test="replace-next"]').attributes('disabled')).toBeDefined()
  })

  it('sendet erst nach der Bestätigung', async () => {
    const w = mountDialog()
    await w.find('[data-test="replace-next"]').trigger('click')
    await w.find('[data-test="new-option"][data-id="n1"] input').setValue(true)
    await w.find('[data-test="replace-next"]').trigger('click')
    expect(w.text()).toContain('Alt-HDD')
    expect(w.text()).toContain('Neu-HDD')
    expect(w.text()).toContain('viele Stunden')
    const submit = () => w.find('[data-test="replace-submit"]')
    expect(submit().attributes('disabled')).toBeDefined()
    await w.find('input#replace-confirm').setValue(true)
    expect(submit().attributes('disabled')).toBeUndefined()
    await submit().trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([{ oldDisk: 'a1', newDisk: 'n1' }])
  })
})

const status = (over: Partial<SnapraidStatus> = {}): SnapraidStatus => ({ enabled: true, running: false, ...over })

function stubStatus(st: SnapraidStatus) {
  vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => new Response(JSON.stringify(st), { status: 200 })))
}

describe('SnapraidPanel: Plattenersatz', () => {
  it('hebt „Platte ersetzen“ hervor, wenn eine Platte des Pools fehlt', async () => {
    stubStatus(status())
    const p = createPinia()
    setActivePinia(p)
    useStorageStore().disks = disks
    const w = mount(SnapraidPanel, { props: { pool }, global: { plugins: [p, PrimeVue, ToastService] } })
    await flushPromises()
    const btn = w.find('[data-test="snapraid-replace"]')
    expect(btn.attributes('data-missing')).toBe('true')
    await btn.trigger('click')
    expect(w.emitted('replace')).toBeTruthy()
  })

  it('zeigt eine offene Wiederherstellung mit Wiederholen/Abbrechen und sperrt Sync und Prüfung', async () => {
    stubStatus(status({ recovery: { disk: 'old1', state: 'open', since: '2026-11-17T10:00:00Z' }, lastFix: { action: 'fix', result: 'errors', message: '2 Dateien sind nicht wiederherstellbar.', at: '2026-11-17T11:00:00Z' } }))
    const w = mount(SnapraidPanel, { props: { pool }, global: { plugins: plugins() } })
    await flushPromises()
    const banner = w.find('[data-test="snapraid-recovery"]')
    expect(banner.text()).toContain('old1')
    expect(banner.text()).toContain('2 Dateien sind nicht wiederherstellbar')
    expect(w.find('[data-test="snapraid-sync"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="snapraid-scrub"]').attributes('disabled')).toBeDefined()
    await w.find('[data-test="recovery-retry"]').trigger('click')
    await w.find('[data-test="recovery-cancel"]').trigger('click')
    expect(w.emitted('retry')).toBeTruthy()
    expect(w.emitted('cancelRecovery')).toBeTruthy()
  })

  it('zeigt ohne offene Wiederherstellung kein Banner', async () => {
    stubStatus(status())
    const w = mount(SnapraidPanel, { props: { pool }, global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="snapraid-recovery"]').exists()).toBe(false)
  })
})

describe('PoolsView: Plattenersatz', () => {
  function mountPools() {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string) => {
        const body = url.endsWith('/status') ? status({ recovery: undefined }) : url.includes('/disks') ? disks : [pool]
        return new Response(JSON.stringify(body), { status: 200 })
      }),
    )
    const p = createPinia()
    setActivePinia(p)
    const store = useStorageStore()
    const replace = vi.spyOn(store, 'replaceDisk').mockResolvedValue(undefined)
    const retry = vi.spyOn(store, 'retryRecovery').mockResolvedValue(undefined)
    const cancel = vi.spyOn(store, 'cancelRecovery').mockResolvedValue(undefined)
    const w = mount(PoolsView, { global: { plugins: [p, PrimeVue, ToastService] } })
    return { w, replace, retry, cancel }
  }

  it('führt den Assistenten bis zum Auftrag', async () => {
    const { w, replace } = mountPools()
    await flushPromises()
    await w.find('[data-test="snapraid-replace"]').trigger('click')
    await w.find('[data-test="replace-next"]').trigger('click')
    await w.find('[data-test="new-option"][data-id="n1"] input').setValue(true)
    await w.find('[data-test="replace-next"]').trigger('click')
    await w.find('input#replace-confirm').setValue(true)
    await w.find('[data-test="replace-submit"]').trigger('click')
    await flushPromises()
    expect(replace).toHaveBeenCalledWith('media', 'a1', 'n1')
  })
})
