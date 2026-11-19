import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import DiskDetailDialog from '@/components/storage/DiskDetailDialog.vue'
import SelfTestScheduleDialog from '@/components/storage/SelfTestScheduleDialog.vue'
import TrendChart from '@/components/storage/TrendChart.vue'
import UsageBar from '@/components/storage/UsageBar.vue'
import DisksView from '@/views/storage/DisksView.vue'
import { useStorageStore } from '@/stores/storage'
import type { DiskView, HistoryPoint } from '@/api/storage'
import { disk, pools } from './fixtures'

const plugins = () => [createPinia(), PrimeVue, ToastService]
afterEach(() => vi.unstubAllGlobals())

const smart = { passed: true, temperatureC: 41, powerOnHours: 12345, standby: false, reallocatedSectors: 0, pendingSectors: 0 }
const usage = { sizeBytes: 4e12, usedBytes: 3e12, freeBytes: 1e12 }
const point = (i: number, temp: number, sect = 0): HistoryPoint => ({ ts: 1_700_000_000 + i * 3600, temperatureC: temp, reallocatedSectors: sect, pendingSectors: 0, powerOnHours: 100 + i })

describe('UsageBar', () => {
  const bar = (u: number) => mount(UsageBar, { props: { usage: { sizeBytes: 100, usedBytes: u, freeBytes: 100 - u } } })
  it('zeigt Prozent und färbt ab 85 % warnend, ab 95 % rot', () => {
    expect(bar(50).text()).toContain('belegt')
    expect(bar(50).classes()).toContain('ok')
    expect(bar(85).classes()).toContain('warn')
    expect(bar(96).classes()).toContain('danger')
    expect(bar(96).find('[role="progressbar"]').attributes('aria-valuenow')).toBe('96')
  })
  it('rendert ohne Angaben nichts', () => {
    expect(mount(UsageBar, { props: {} }).find('[data-test="usage-bar"]').exists()).toBe(false)
  })
})

describe('TrendChart', () => {
  it('zeichnet eine Linie und warnt ab dem Grenzwert', () => {
    const w = mount(TrendChart, { props: { title: 'Temperatur', unit: '°C', values: [30, 40, 55], times: [1, 2, 3], warnAt: 50 } })
    expect(w.find('path.line').attributes('d')).toMatch(/^M/)
    expect(w.classes()).toContain('warn')
    expect(w.text()).toContain('55 °C')
  })
  it('sagt, dass noch Messpunkte fehlen', () => {
    const w = mount(TrendChart, { props: { title: 'Temperatur', values: [30], times: [1] } })
    expect(w.text()).toContain('zu wenig Messpunkte')
    expect(w.find('svg').exists()).toBe(false)
  })
})

describe('DiskDetailDialog', () => {
  function setup(d: DiskView, extra: (url: string, init?: RequestInit) => Response | undefined = () => undefined) {
    const calls: { url: string; method: string; body?: string }[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
        calls.push({ url, method: init?.method ?? 'GET', body: init?.body as string | undefined })
        const r = extra(url, init)
        if (r) return r
        if (url.includes('/history')) return new Response(JSON.stringify([point(0, 38), point(1, 41, 2), point(2, 44, 5)]), { status: 200 })
        if (url.endsWith('/disks')) return new Response(JSON.stringify([d]), { status: 200 })
        if (url.endsWith('/pools')) return new Response('[]', { status: 200 })
        return new Response(null, { status: 204 })
      }),
    )
    const pinia = createPinia()
    setActivePinia(pinia)
    const store = useStorageStore()
    store.disks = [d]
    const w = mount(DiskDetailDialog, { props: { visible: true, disk: d }, global: { plugins: [pinia, PrimeVue, ToastService] } })
    return { w, calls }
  }

  it('lädt den Verlauf und zeigt beide Diagramme', async () => {
    const { w, calls } = setup(disk({ id: 'd1', role: 'data', smart, usage }))
    await flushPromises()
    expect(calls.some((c) => c.url === '/api/v1/disks/d1/history?days=30')).toBe(true)
    expect(w.findAll('[data-test="trend-chart"]').length).toBe(2)
    expect(w.text()).toContain('44 °C')
    await w.find('[data-test="range-90"]').trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.url === '/api/v1/disks/d1/history?days=90')).toBe(true)
  })

  it('startet Kurz- und Langtest per POST', async () => {
    const { w, calls } = setup(disk({ id: 'd1', role: 'data', smart }))
    await flushPromises()
    await w.find('[data-test="selftest-short"]').trigger('click')
    await flushPromises()
    await w.find('[data-test="selftest-long"]').trigger('click')
    await flushPromises()
    const posts = calls.filter((c) => c.method === 'POST')
    expect(posts.map((c) => [c.url, c.body])).toEqual([
      ['/api/v1/disks/d1/selftest', JSON.stringify({ kind: 'short' })],
      ['/api/v1/disks/d1/selftest', JSON.stringify({ kind: 'long' })],
    ])
  })

  it('zeigt Fortschritt eines laufenden Tests und bricht ihn per DELETE ab', async () => {
    const { w, calls } = setup(disk({ id: 'd1', role: 'data', smart: { ...smart, selfTest: { running: true, remainingPercent: 40 } } }))
    await flushPromises()
    expect(w.find('[data-test="selftest-running"]').text()).toContain('noch etwa 40 %')
    expect(w.find('[data-test="selftest-short"]').exists()).toBe(false)
    await w.find('[data-test="selftest-abort"]').trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.method === 'DELETE' && c.url === '/api/v1/disks/d1/selftest')).toBe(true)
  })

  it('zeigt das Ergebnis des letzten Tests', async () => {
    const { w } = setup(disk({ id: 'd1', role: 'data', smart: { ...smart, selfTest: { running: false, last: { type: 'long', result: 'failed', status: 'Read failure', powerOnHours: 99 } } } }))
    await flushPromises()
    const last = w.find('[data-test="selftest-last"]').text()
    expect(last).toContain('Langtest')
    expect(last).toContain('fehlgeschlagen')
  })

  it('meldet einen abgelehnten Test als Fehlermeldung, ohne abzustürzen', async () => {
    const { w } = setup(disk({ id: 'd1', role: 'data', smart }), (url, init) =>
      init?.method === 'POST' ? new Response(JSON.stringify({ error: { code: 'conflict', message: 'läuft schon' } }), { status: 409 }) : undefined,
    )
    await flushPromises()
    await w.find('[data-test="selftest-short"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="selftest-short"]').exists()).toBe(true)
  })

  it('zeigt einen Fehler beim Verlauf an', async () => {
    const { w } = setup(disk({ id: 'd1', role: 'data', smart }), (url) =>
      url.includes('/history') ? new Response(JSON.stringify({ error: { code: 'internal', message: 'db kaputt' } }), { status: 500 }) : undefined,
    )
    await flushPromises()
    expect(w.text()).toContain('db kaputt')
  })
})

describe('SelfTestScheduleDialog', () => {
  it('lädt und speichert den Zeitplan', async () => {
    const puts: { url: string; body: string }[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
        if (init?.method === 'PUT') {
          puts.push({ url, body: init.body as string })
          return new Response(null, { status: 204 })
        }
        return new Response(JSON.stringify({ short: 'weekly', long: 'off', hour: 2 }), { status: 200 })
      }),
    )
    const w = mount(SelfTestScheduleDialog, { props: { visible: true }, global: { plugins: plugins() } })
    await flushPromises()
    expect((w.find('[data-test="sched-short"]').element as HTMLSelectElement).value).toBe('weekly')
    await w.find('[data-test="sched-long"]').setValue('monthly')
    await w.find('[data-test="sched-hour"]').setValue('4')
    await w.find('[data-test="sched-save"]').trigger('click')
    await flushPromises()
    expect(puts).toEqual([{ url: '/api/v1/disks/selftest/schedule', body: JSON.stringify({ short: 'weekly', long: 'monthly', hour: 4 }) }])
    expect(w.emitted('update:visible')?.[0]).toEqual([false])
  })

  it('zeigt die Ablehnung und bleibt offen', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (_u: string, init?: RequestInit) =>
        init?.method === 'PUT'
          ? new Response(JSON.stringify({ error: { code: 'invalid_params', message: 'Ungültiger Zeitplan' } }), { status: 400 })
          : new Response(JSON.stringify({ short: 'off', long: 'off', hour: 2 }), { status: 200 }),
      ),
    )
    const w = mount(SelfTestScheduleDialog, { props: { visible: true }, global: { plugins: plugins() } })
    await flushPromises()
    await w.find('[data-test="sched-save"]').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Ungültiger Zeitplan')
    expect(w.emitted('update:visible')).toBeUndefined()
  })
})

describe('DisksView: Belegung und Selbsttest', () => {
  const rows = [
    disk({ id: 'd1', role: 'data', pool: 'media', smart: { ...smart, selfTest: { running: true, remainingPercent: 25 } }, usage }),
    disk({ id: 'd2', device: '/dev/sdb', serial: 'S2', role: 'data', smart: { ...smart, selfTest: { running: false, last: { type: 'short', result: 'failed', status: 'x' } } } }),
    disk({ id: 'd3', device: '/dev/sdc', serial: 'S3', role: 'unassigned' }),
  ]
  const mountView = async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async (url: string) => new Response(JSON.stringify(url.includes('/disks') ? rows : pools), { status: 200 })))
    const w = mount(DisksView, { global: { plugins: plugins() } })
    await flushPromises()
    return w
  }

  it('zeigt Belegungsbalken, laufende und fehlgeschlagene Tests', async () => {
    const w = await mountView()
    expect(w.find('[data-test="disk-d1"] [data-test="usage-bar"]').exists()).toBe(true)
    expect(w.find('[data-test="disk-d2"] [data-test="usage-bar"]').exists()).toBe(false)
    expect(w.find('[data-test="selftest-running-d1"]').text()).toContain('75 %')
    expect(w.find('[data-test="selftest-failed-d2"]').text()).toContain('Kurztest fehlgeschlagen')
    expect(w.find('[data-test="details-d3"]').exists()).toBe(false)
  })

  it('öffnet Details und den Zeitplan', async () => {
    const w = await mountView()
    await w.find('[data-test="details-d1"]').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Selbsttest läuft')
    await w.find('[data-test="open-schedule"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="sched-save"]').exists()).toBe(true)
  })
})
