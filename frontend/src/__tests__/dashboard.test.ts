import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import DashboardView from '@/views/DashboardView.vue'
import { useAuthStore } from '@/stores/auth'

const info = (total: number, idle: number) => ({
  hostname: 'nas', kernel: 'k', os: 'o', uptimeSeconds: 10, cpuCount: 4, cpuModel: 'Testprozessor',
  cpuTotalTicks: total, cpuIdleTicks: idle, load: [0.5, 0.25, 0.1], memTotalBytes: 2e9, memAvailableBytes: 1e9, version: '0.4.0',
})

function stubFetch(samples: ReturnType<typeof info>[]) {
  let n = 0
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation(async (url: string) => {
      const u = String(url)
      const body = u.includes('/system/info') ? samples[Math.min(n++, samples.length - 1)] : u.includes('/docker/status') ? { installed: true, active: true, version: '27' } : []
      return new Response(JSON.stringify(body), { status: 200 })
    }),
  )
}

function mountDash() {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().user = 'anna'
  return mount(DashboardView, { global: { plugins: [pinia, PrimeVue, ToastService], stubs: { RouterLink: true } } })
}
const edit = (w: ReturnType<typeof mountDash>) => w.find('[data-test="edit-toggle"]').trigger('click')
const ids = (w: ReturnType<typeof mountDash>) => w.findAll('article.widget').map((a) => a.attributes('data-test')!.replace('tile-', ''))

beforeEach(() => localStorage.clear())
afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('Dashboard: CPU-Kachel', () => {
  it('berechnet die Auslastung aus zwei Messungen und zeigt Last und Modell', async () => {
    vi.useFakeTimers()
    stubFetch([info(1000, 800), info(2000, 1300)])
    const w = mountDash()
    await vi.advanceTimersByTimeAsync(0)
    expect(w.find('[data-test="cpu-percent"]').text()).toBe('–')
    await vi.advanceTimersByTimeAsync(1600)
    expect(w.find('[data-test="cpu-percent"]').text()).toBe('50 %')
    expect(w.find('[data-test="cpu-load"]').text()).toContain('0.50 · 0.25 · 0.10')
    expect(w.find('[data-test="tile-cpu"]').text()).toContain('Testprozessor')
    w.unmount()
  })

  it('bleibt bei stehenden Zählern bei der letzten Messung', async () => {
    vi.useFakeTimers()
    stubFetch([info(1000, 800), info(2000, 1300), info(2000, 1300)])
    const w = mountDash()
    await vi.advanceTimersByTimeAsync(1600)
    await vi.advanceTimersByTimeAsync(5000)
    expect(w.find('[data-test="cpu-percent"]').text()).toBe('50 %')
    w.unmount()
  })
})

describe('Dashboard: Meldungen-Kachel', () => {
  const alert = (key: string, level: string) => ({ key, level, title: `Titel ${key}`, body: 'b', since: '2026-11-17T10:00:00Z' })
  function stubAlerts(list: unknown[]) {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string) => {
        const u = String(url)
        const body = u.includes('/system/info') ? info(1, 1) : u.includes('/alerts') ? list : u.includes('/docker/status') ? { installed: true, active: true, version: '27' } : []
        return new Response(JSON.stringify(body), { status: 200 })
      }),
    )
  }

  it('zeigt die Anzahl aktiver Meldungen, rot bei kritischen', async () => {
    stubAlerts([alert('smart:d1', 'crit'), alert('temp:d2', 'warn')])
    const w = mountDash()
    await flushPromises()
    const tile = w.find('[data-test="tile-alerts"]')
    expect(tile.text()).toContain('2 Meldungen')
    expect(tile.text()).toContain('Titel smart:d1')
    expect(tile.classes()).toContain('crit')
    w.unmount()
  })

  it('erscheint nicht, wenn nichts anliegt', async () => {
    stubAlerts([])
    const w = mountDash()
    await flushPromises()
    expect(w.find('[data-test="tile-alerts"]').exists()).toBe(false)
    w.unmount()
  })
})

describe('Dashboard: Kacheln sortieren', () => {
  const dragTo = async (w: ReturnType<typeof mountDash>, from: string, to: string) => {
    const dataTransfer = { setData: vi.fn(), effectAllowed: '' }
    await w.find(`[data-test="tile-${from}"]`).trigger('dragstart', { dataTransfer })
    await w.find(`[data-test="tile-${to}"]`).trigger('drop')
  }

  it('zeigt die Standardreihenfolge ohne leere SnapRAID-Kachel', async () => {
    stubFetch([info(1, 1)])
    const w = mountDash()
    await flushPromises()
    expect(ids(w)).toEqual(['storage', 'cpu', 'memory', 'disks', 'docker', 'system'])
    expect(w.find('[data-test="tiles-reset"]').exists()).toBe(false)
    w.unmount()
  })

  it('erlaubt das Verschieben nur im Bearbeiten-Modus', async () => {
    stubFetch([info(1, 1)])
    const w = mountDash()
    await flushPromises()
    expect(w.find('[data-test="tile-cpu"]').attributes('draggable')).toBe('false')
    expect(w.find('[data-test="tile-cpu"] .handle').exists()).toBe(false)
    await dragTo(w, 'system', 'storage') // ohne Modus wirkungslos
    expect(ids(w)).toEqual(['storage', 'cpu', 'memory', 'disks', 'docker', 'system'])
    expect(localStorage.getItem('bn-dashboard-tiles:anna')).toBeNull()

    await edit(w)
    expect(w.find('[data-test="edit-toggle"]').text()).toContain('Fertig')
    expect(w.find('[data-test="tile-cpu"]').attributes('draggable')).toBe('true')
    expect(w.find('[data-test="tile-cpu"] .handle').exists()).toBe(true)
    await dragTo(w, 'system', 'storage')
    expect(ids(w)[0]).toBe('system')

    await edit(w) // Fertig
    expect(w.find('[data-test="tile-cpu"]').attributes('draggable')).toBe('false')
    expect(w.find('[data-test="tile-cpu"] .handle').exists()).toBe(false)
    expect(w.find('[data-test="tiles-reset"]').exists()).toBe(false)
    w.unmount()
  })

  it('ordnet per Ziehen um, merkt es sich pro Benutzer und setzt zurück', async () => {
    stubFetch([info(1, 1)])
    const w = mountDash()
    await flushPromises()
    await edit(w)
    await dragTo(w, 'system', 'storage')
    expect(ids(w)).toEqual(['system', 'storage', 'cpu', 'memory', 'disks', 'docker'])
    expect(JSON.parse(localStorage.getItem('bn-dashboard-tiles:anna')!)).toContain('system')
    w.unmount()

    const w2 = mountDash()
    await flushPromises()
    expect(ids(w2)).toEqual(['system', 'storage', 'cpu', 'memory', 'disks', 'docker'])
    await edit(w2)
    await w2.find('[data-test="tiles-reset"]').trigger('click')
    expect(ids(w2)).toEqual(['storage', 'cpu', 'memory', 'disks', 'docker', 'system'])
    expect(localStorage.getItem('bn-dashboard-tiles:anna')).toBeNull()
    w2.unmount()
  })

  it('ordnet mit den Pfeiltasten am Griff um', async () => {
    stubFetch([info(1, 1)])
    const w = mountDash()
    await flushPromises()
    await edit(w)
    await w.find('[data-test="tile-cpu"] .handle').trigger('keydown', { key: 'ArrowLeft' })
    expect(ids(w)).toEqual(['cpu', 'storage', 'memory', 'disks', 'docker', 'system'])
    await w.find('[data-test="tile-cpu"] .handle').trigger('keydown', { key: 'ArrowLeft' }) // schon ganz vorn
    expect(ids(w)).toEqual(['cpu', 'storage', 'memory', 'disks', 'docker', 'system'])
    await w.find('[data-test="tile-cpu"] .handle').trigger('keydown', { key: 'a' })
    expect(ids(w)).toEqual(['cpu', 'storage', 'memory', 'disks', 'docker', 'system'])
    w.unmount()
  })

  it('ignoriert gespeicherte Kacheln, die es nicht mehr gibt', async () => {
    localStorage.setItem('bn-dashboard-tiles:anna', JSON.stringify(['gibtsnicht', 'system']))
    stubFetch([info(1, 1)])
    const w = mountDash()
    await flushPromises()
    expect(ids(w)).toEqual(['system', 'storage', 'cpu', 'memory', 'disks', 'docker'])
    w.unmount()
  })
})

describe('Dashboard: Datenträger-Kachel', () => {
  it('zeigt die Belegung der Daten- und Paritätsplatten', async () => {
    const d = (id: string, role: string, usage?: object) => ({
      id, device: '/dev/' + id, model: 'HDD ' + id, serial: id, sizeBytes: 4e12, role, state: 'online', pool: role === 'data' ? 'media' : undefined, partitions: [], usage,
    })
    const rows = [
      d('a', 'data', { sizeBytes: 4e12, usedBytes: 3e12, freeBytes: 1e12 }),
      d('b', 'parity', { sizeBytes: 4e12, usedBytes: 1e12, freeBytes: 3e12 }),
      d('c', 'data'),
      d('s', 'system', { sizeBytes: 1e11, usedBytes: 5e10, freeBytes: 5e10 }),
    ]
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string) => {
        const u = String(url)
        const body = u.includes('/system/info') ? info(1, 1) : u.endsWith('/disks') ? rows : u.includes('/docker/status') ? { installed: true, active: true, version: '27' } : []
        return new Response(JSON.stringify(body), { status: 200 })
      }),
    )
    const w = mountDash()
    await flushPromises()
    const list = w.find('[data-test="disk-usage"]')
    expect(list.findAll('li').length).toBe(2)
    expect(list.text()).toContain('HDD a')
    expect(list.text()).toContain('Parität')
    expect(list.text()).not.toContain('HDD c')
    expect(list.text()).not.toContain('HDD s')
    w.unmount()
  })
})
