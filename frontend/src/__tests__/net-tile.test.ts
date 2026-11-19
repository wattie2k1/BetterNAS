import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
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
  it('zeigt Raten, Verlauf und Schnittstellen mit Geschwindigkeit', () => {
    const current = netRates({ at: 0, net: [eth(0, 0), eth(0, 0, { name: 'eno2', up: false, speedMbps: 0 })] }, { at: 1000, net: [eth(5_000_000, 1_000_000), eth(0, 0, { name: 'eno2', up: false, speedMbps: 0 })] })
    const w = mount(NetTile, { props: { current, rx: [0, 5e6], tx: [0, 1e6] } })
    expect(w.find('[data-test="net-rx"]').text()).toBe('5,0 MB/s')
    expect(w.find('[data-test="net-tx"]').text()).toBe('1,0 MB/s')
    expect(w.find('[data-test="net-history"] svg').exists()).toBe(true)
    expect(w.find('[data-test="net-ifaces"]').text()).toContain('1 GBit/s')
    expect(w.find('[data-test="net-ifaces"]').text()).not.toContain('eno2') // getrennt und ohne Verkehr
  })
  it('zeigt vor der zweiten Messung Platzhalter', () => {
    const w = mount(NetTile, { props: { current: null, rx: [], tx: [] } })
    expect(w.find('[data-test="net-rx"]').text()).toBe('–')
    expect(w.find('[data-test="net-history"] svg').exists()).toBe(false)
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
