import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import DashboardView from '@/views/DashboardView.vue'
import NetTile from '@/components/NetTile.vue'
import { useAuthStore } from '@/stores/auth'
import { formatRate, ifaceLoad, netRates } from '@/utils/netRate'

const eth = (rx: number, tx: number, extra = {}) => ({ name: 'eno1', rxBytes: rx, txBytes: tx, up: true, speedMbps: 1000, ...extra })

describe('netRates', () => {
  it('berechnet Byte/s aus zwei Zählerständen', () => {
    const r = netRates({ at: 0, net: [eth(0, 0)] }, { at: 2000, net: [eth(4_000_000, 1_000_000)] })!
    expect(r.rx).toBe(2_000_000)
    expect(r.tx).toBe(500_000)
  })
  it('liefert ohne Vorwert nichts und 0 bei zurückgesetztem Zähler oder neuer Karte', () => {
    expect(netRates(null, { at: 1000, net: [eth(1, 1)] })).toBeNull()
    expect(netRates({ at: 0, net: [eth(9000, 9000)] }, { at: 1000, net: [eth(10, 10)] })!.rx).toBe(0)
    expect(netRates({ at: 0, net: [] }, { at: 1000, net: [eth(10, 10)] })!.tx).toBe(0)
  })
  it('rechnet die Auslastung gegen die Leitungsgeschwindigkeit', () => {
    expect(ifaceLoad({ rx: 62_500_000, tx: 0, speedMbps: 1000 })).toBeCloseTo(50)
    expect(ifaceLoad({ rx: 1, tx: 1, speedMbps: 0 })).toBeNull()
    expect(ifaceLoad({ rx: 1e12, tx: 0, speedMbps: 100 })).toBe(100)
  })
  it('formatiert Raten', () => {
    expect(formatRate(512)).toBe('512 B/s')
    expect(formatRate(2_500_000)).toBe('2,5 MB/s')
  })
})

describe('NetTile', () => {
  it('zeigt Raten und Verlauf ohne Schnittstellenliste', () => {
    const current = netRates({ at: 0, net: [eth(0, 0), eth(0, 0, { name: 'eno2', up: false, speedMbps: 0 })] }, { at: 1000, net: [eth(5_000_000, 1_000_000), eth(0, 0, { name: 'eno2', up: false, speedMbps: 0 })] })
    const w = mount(NetTile, { props: { current, rx: [0, 5e6], tx: [0, 1e6] } })
    expect(w.find('[data-test="net-rx"]').text()).toBe('5,0 MB/s')
    expect(w.find('[data-test="net-tx"]').text()).toBe('1,0 MB/s')
    expect(w.find('[data-test="net-history"] svg').exists()).toBe(true)
    expect(w.find('[data-test="net-ifaces"]').exists()).toBe(false)
    expect(w.text()).not.toContain('eno1')
  })
  it('zeigt vor der zweiten Messung Platzhalter', () => {
    const w = mount(NetTile, { props: { current: null, rx: [], tx: [] } })
    expect(w.find('[data-test="net-rx"]').text()).toBe('–')
    expect(w.find('[data-test="net-history"] svg').exists()).toBe(false)
  })
})

describe('NetTile: Datenmengen', () => {
  afterEach(() => vi.unstubAllGlobals())
  it('zeigt Tage, Monate und Jahre mit kB/MB/GB/TB', async () => {
    const traffic = {
      days: [{ key: '2026-11-20', rx: 2_000_000_000, tx: 500_000 }, { key: '2026-11-21', rx: 3_500_000_000_000, tx: 1500 }],
      months: [{ key: '2026-11', rx: 5e9, tx: 1e6 }],
      years: [{ key: '2026', rx: 5e9, tx: 1e6 }],
    }
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => new Response(JSON.stringify(traffic), { status: 200 })))
    const w = mount(NetTile, { props: { current: null, rx: [], tx: [] } })
    await flushPromises()
    const rows = w.find('[data-test="net-traffic-rows"]').text()
    expect(rows).toContain('21.11.2026')
    expect(rows).toContain('3,5 TB')
    expect(rows).toContain('1,5 kB')
    expect(rows.indexOf('21.11.2026')).toBeLessThan(rows.indexOf('20.11.2026')) // neueste zuerst
    await w.find('[data-test="net-period-months"]').trigger('click')
    expect(w.find('[data-test="net-traffic-rows"]').text()).toContain('11/2026')
    await w.find('[data-test="net-period-years"]').trigger('click')
    expect(w.find('[data-test="net-traffic-rows"]').text()).toContain('2026')
  })
  it('meldet eine leere Statistik', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => new Response('[]', { status: 200 })))
    const w = mount(NetTile, { props: { current: null, rx: [], tx: [] } })
    await flushPromises()
    expect(w.find('[data-test="net-traffic-empty"]').exists()).toBe(true)
  })
})

describe('Dashboard: Netzwerk-Kachel', () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })
  const sample = (net?: unknown[]) => ({
    hostname: 'nas', kernel: 'k', os: 'o', uptimeSeconds: 10, cpuCount: 4, cpuModel: 'x', cpuTotalTicks: 1, cpuIdleTicks: 1,
    load: [0, 0, 0], memTotalBytes: 2e9, memAvailableBytes: 1e9, version: '0.4.0', net,
  })
  function mountWith(samples: unknown[]) {
    let n = 0
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async (url: string) => {
      const body = String(url).includes('/system/info') ? samples[Math.min(n++, samples.length - 1)] : String(url).includes('/docker/status') ? { installed: true, active: true, version: '27' } : []
      return new Response(JSON.stringify(body), { status: 200 })
    }))
    const pinia = createPinia()
    setActivePinia(pinia)
    useAuthStore().user = 'anna'
    return mount(DashboardView, { global: { plugins: [pinia, PrimeVue, ToastService], stubs: { RouterLink: true } } })
  }

  it('erscheint nur, wenn der Server Netzwerkdaten liefert', async () => {
    vi.useFakeTimers()
    const w = mountWith([sample()])
    await vi.advanceTimersByTimeAsync(10)
    expect(w.find('[data-test="tile-network"]').exists()).toBe(false)
    w.unmount()
  })

  it('berechnet die Rate aus zwei Abfragen', async () => {
    vi.useFakeTimers()
    const w = mountWith([sample([eth(0, 0)]), sample([eth(3_000_000, 600_000)])])
    await vi.advanceTimersByTimeAsync(10)
    expect(w.find('[data-test="net-rx"]').text()).toBe('–')
    await vi.advanceTimersByTimeAsync(1600) // zweite Messung nach 1,5 s
    expect(w.find('[data-test="net-rx"]').text()).toBe('2,0 MB/s')
    expect(w.find('[data-test="net-tx"]').text()).toBe('400,0 kB/s')
    w.unmount()
  })
})
