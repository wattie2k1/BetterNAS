import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import DiskDetailDialog from '@/components/storage/DiskDetailDialog.vue'
import SelfTestScheduleDialog from '@/components/storage/SelfTestScheduleDialog.vue'
import StandbyChart from '@/components/storage/StandbyChart.vue'
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
    expect(w.find('[data-test="details-d3"]').exists()).toBe(true) // Partitionen und Notizen gibt es auch ohne SMART
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

describe('SSD-Verschleiß und SMART-Attribute', () => {
  const ssdSmart = {
    ...smart,
    wear: { percentUsed: 93, bytesWritten: 2e14, spareLeftPercent: 98 },
    attributes: [
      { id: 5, name: 'Reallocated_Sector_Ct', value: 100, worst: 100, threshold: 10, raw: '0', prefail: true },
      { id: 194, name: 'Temperature_Celsius', value: 70, worst: 60, threshold: 0, raw: '30', failing: true },
    ],
  }
  const mountDetail = async (d: DiskView) => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async (url: string) => new Response(url.includes('/history') ? '[]' : JSON.stringify([d]), { status: 200 })))
    const pinia = createPinia()
    setActivePinia(pinia)
    useStorageStore().disks = [d]
    const w = mount(DiskDetailDialog, { props: { visible: true, disk: d }, global: { plugins: [pinia, PrimeVue, ToastService] } })
    await flushPromises()
    return w
  }

  it('zeigt Verschleiß mit Warnung ab 90 %', async () => {
    const w = await mountDetail(disk({ id: 's', rotational: false, role: 'data', smart: ssdSmart }))
    const t = w.find('[data-test="wear"]')
    expect(t.text()).toContain('93 % der Lebensdauer verbraucht')
    expect(t.text()).toContain('Austausch')
    expect(t.classes()).toContain('bad')
    expect(w.find('[data-test="wear-written"]').text()).toContain('200,0 TB')
  })

  it('zeigt keinen Verschleiß bei HDDs, aber die Attributtabelle mit markierten Fehlern', async () => {
    const w = await mountDetail(disk({ id: 'h', role: 'data', smart: { ...ssdSmart, wear: undefined } }))
    expect(w.find('[data-test="wear"]').exists()).toBe(false)
    expect(w.find('[data-test="attr-5"]').text()).toContain('Vorwarnung')
    expect(w.find('[data-test="attr-194"]').classes()).toContain('failing')
    expect(w.find('[data-test="attributes"]').text()).toContain('SMART-Attribute (2)')
  })

  it('lässt bei NVMe die Spalten ohne Attribut-IDs weg', async () => {
    const w = await mountDetail(disk({ id: 'n', rotational: false, role: 'data', smart: { ...smart, wear: { percentUsed: 3 }, attributes: [{ id: 0, name: 'Medienfehler', value: 0, worst: 0, threshold: 0, raw: '0' }] } }))
    expect(w.find('[data-test="attributes"] th').text()).toBe('Name')
    expect(w.find('[data-test="wear"]').classes()).not.toContain('bad')
  })

  it('zeigt den Verschleiß in der Tabelle', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async (url: string) => new Response(JSON.stringify(url.includes('/disks') ? [disk({ id: 's', rotational: false, role: 'data', smart: ssdSmart })] : pools), { status: 200 })))
    const w = mount(DisksView, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="wear-s"]').text()).toContain('Verschleiß 93 %')
    expect(w.find('[data-test="wear-s"]').classes()).toContain('warn')
  })
})

describe('DisksView: Platte finden', () => {
  function stub(rows: DiskView[]) {
    const calls: { url: string; method: string }[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
        const method = init?.method ?? 'GET'
        if (method !== 'GET') {
          calls.push({ url, method })
          return url.includes('/d9/') ? new Response(JSON.stringify({ error: { code: 'conflict', message: 'Die Platte lässt sich nicht lesen.' } }), { status: 409 }) : new Response(null, { status: 204 })
        }
        return new Response(JSON.stringify(url.includes('/disks') ? rows : pools), { status: 200 })
      }),
    )
    return calls
  }

  it('startet das Blinken per POST und zeigt „Stopp“, solange es läuft', async () => {
    const calls = stub([disk({ id: 'd1', role: 'data' }), disk({ id: 'd2', device: '/dev/sdb', role: 'data', identifying: true }), disk({ id: 'd4', device: '', role: 'data', state: 'missing' })])
    const w = mount(DisksView, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="identify-d1"]').text()).toContain('Finden')
    expect(w.find('[data-test="identify-d2"]').text()).toContain('Stopp')
    expect(w.find('[data-test="identify-d4"]').exists()).toBe(false)
    await w.find('[data-test="identify-d1"]').trigger('click')
    await w.find('[data-test="identify-d2"]').trigger('click')
    await flushPromises()
    expect(calls).toEqual([
      { url: '/api/v1/disks/d1/identify', method: 'POST' },
      { url: '/api/v1/disks/d2/identify', method: 'DELETE' },
    ])
  })

  it('übersteht eine Ablehnung der API', async () => {
    stub([disk({ id: 'd9', role: 'data' })])
    const w = mount(DisksView, { global: { plugins: plugins() } })
    await flushPromises()
    await w.find('[data-test="identify-d9"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="identify-d9"]').text()).toContain('Finden')
  })
})

describe('Notizen und Partitionen', () => {
  const parts = [
    { device: '/dev/sdb1', fsType: 'ext4', uuid: 'u', label: 'bnas-1', sizeBytes: 2e12, mountpoints: ['/srv/disks/d2'] },
    { device: '/dev/sdb2', fsType: '', uuid: '', label: '', sizeBytes: 5e9, mountpoints: [] },
  ]
  function setup(d: DiskView, put: () => Response = () => new Response(null, { status: 204 })) {
    const puts: { url: string; body: string }[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
        if (init?.method === 'PUT') {
          puts.push({ url, body: init.body as string })
          return put()
        }
        return new Response(url.includes('/history') ? '[]' : JSON.stringify([d]), { status: 200 })
      }),
    )
    const pinia = createPinia()
    setActivePinia(pinia)
    useStorageStore().disks = [d]
    const w = mount(DiskDetailDialog, { props: { visible: true, disk: d }, global: { plugins: [pinia, PrimeVue, ToastService] } })
    return { w, puts }
  }

  it('listet die Partitionen auch ohne SMART-Werte', async () => {
    const { w } = setup(disk({ id: 'd2', role: 'data', partitions: parts }))
    await flushPromises()
    expect(w.findAll('[data-test="partitions"] tbody tr').length).toBe(2)
    expect(w.find('[data-test="part-/dev/sdb1"]').text()).toContain('/srv/disks/d2')
    expect(w.find('[data-test="part-/dev/sdb2"]').text()).toContain('5,0 GB')
    expect(w.find('[data-test="no-smart"]').exists()).toBe(true)
  })

  it('sagt, wenn die Platte keine Partitionen hat', async () => {
    const { w } = setup(disk({ id: 'd3', role: 'unassigned', partitions: [] }))
    await flushPromises()
    expect(w.find('[data-test="no-partitions"]').exists()).toBe(true)
  })

  it('füllt das Formular mit gespeicherten Angaben und speichert nur bei Änderungen', async () => {
    const { w, puts } = setup(disk({ id: 'd2', role: 'data', note: { name: 'Schacht 2', notes: 'gebraucht', purchased: '2024-03-01', warrantyUntil: '2999-01-01' } }))
    await flushPromises()
    expect((w.find('[data-test="note-name"]').element as HTMLInputElement).value).toBe('Schacht 2')
    expect(w.find('[data-test="note-save"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="warranty-hint"]').text()).toContain('Garantie läuft bis')
    await w.find('[data-test="note-name"]').setValue('Schacht 4')
    await w.find('[data-test="note-notes"]').setValue('')
    expect(w.find('[data-test="note-save"]').attributes('disabled')).toBeUndefined()
    await w.find('[data-test="note-form"]').trigger('submit')
    await flushPromises()
    expect(puts).toEqual([{ url: '/api/v1/disks/d2/note', body: JSON.stringify({ name: 'Schacht 4', notes: '', purchased: '2024-03-01', warrantyUntil: '2999-01-01', tempWarnC: 0 }) }])
  })

  it('speichert eine Temperaturgrenze pro Platte; leer bedeutet global', async () => {
    const { w, puts } = setup(disk({ id: 'd2', role: 'data', note: { name: 'S', notes: '', purchased: '', warrantyUntil: '', tempWarnC: 45 } }))
    await flushPromises()
    const input = w.find('[data-test="note-temp"]')
    expect((input.element as HTMLInputElement).value).toBe('45')
    await input.setValue('')
    await w.find('[data-test="note-form"]').trigger('submit')
    await flushPromises()
    expect(JSON.parse(puts[0].body).tempWarnC).toBe(0)
  })

  it('markiert abgelaufene Garantie', async () => {
    const { w } = setup(disk({ id: 'd2', role: 'data', note: { name: '', notes: '', purchased: '', warrantyUntil: '2020-01-01' } }))
    await flushPromises()
    const h = w.find('[data-test="warranty-hint"]')
    expect(h.text()).toContain('abgelaufen')
    expect(h.classes()).toContain('bad')
  })

  it('zeigt die Ablehnung der API im Formular', async () => {
    const { w } = setup(disk({ id: 'd2', role: 'data' }), () => new Response(JSON.stringify({ error: { code: 'invalid_params', message: 'Datumsangaben müssen das Format JJJJ-MM-TT haben.' } }), { status: 400 }))
    await flushPromises()
    await w.find('[data-test="note-name"]').setValue('x')
    await w.find('[data-test="note-form"]').trigger('submit')
    await flushPromises()
    expect(w.find('[data-test="note-error"]').text()).toContain('JJJJ-MM-TT')
  })

  it('zeigt die Bezeichnung in der Tabelle', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async (url: string) => new Response(JSON.stringify(url.includes('/disks') ? [disk({ id: 'd1', role: 'data', note: { name: 'Schacht 3', notes: '', purchased: '', warrantyUntil: '' } })] : pools), { status: 200 })))
    const w = mount(DisksView, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="note-name-d1"]').text()).toBe('Schacht 3')
  })
})

describe('Standby-Statistik und APM', () => {
  const today = new Date()
  const key = (back: number) => {
    const d = new Date(today.getFullYear(), today.getMonth(), today.getDate() - back)
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  }

  it('StandbyChart summiert Anteil und Anläufe und zeigt je Tag einen Balken', () => {
    const w = mount(StandbyChart, {
      props: { range: 7, days: [{ day: key(0), activeMin: 60, standbyMin: 180, spinups: 3 }, { day: key(1), activeMin: 120, standbyMin: 0, spinups: 1 }] },
    })
    expect(w.findAll('.col').length).toBe(7)
    expect(w.find('[data-test="standby-share"]').text()).toBe('50 %') // 180 von 360 Minuten
    expect(w.find('[data-test="standby-sum"]').text()).toContain('4 Anläufe')
  })

  it('StandbyChart fasst lange Zeiträume zu höchstens 30 Balken zusammen und meldet fehlende Daten', () => {
    expect(mount(StandbyChart, { props: { range: 365, days: [] } }).find('[data-test="standby-empty"]').exists()).toBe(true)
    const w = mount(StandbyChart, { props: { range: 365, days: [{ day: key(0), activeMin: 1, standbyMin: 1, spinups: 0 }] } })
    expect(w.findAll('.col').length).toBeLessThanOrEqual(30)
  })

  function setup(d: DiskView, tuning: unknown, put: () => Response = () => new Response(null, { status: 204 })) {
    const calls: { url: string; method: string; body?: string }[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
        const method = init?.method ?? 'GET'
        calls.push({ url, method, body: init?.body as string | undefined })
        if (method === 'PUT') return put()
        if (url.includes('/tuning')) return new Response(JSON.stringify(tuning), { status: 200 })
        if (url.includes('/power')) return new Response(JSON.stringify([{ day: key(0), activeMin: 10, standbyMin: 30, spinups: 2 }]), { status: 200 })
        if (url.includes('/history')) return new Response('[]', { status: 200 })
        return new Response(JSON.stringify([d]), { status: 200 })
      }),
    )
    const pinia = createPinia()
    setActivePinia(pinia)
    useStorageStore().disks = [d]
    const w = mount(DiskDetailDialog, { props: { visible: true, disk: d }, global: { plugins: [pinia, PrimeVue, ToastService] } })
    return { w, calls }
  }

  it('lädt die Statistik für drehende Platten, nicht für SSDs', async () => {
    const hdd = setup(disk({ id: 'h', role: 'data', smart }), { saved: { apm: 0, writeCache: '' }, current: {} })
    await flushPromises()
    expect(hdd.calls.some((c) => c.url === '/api/v1/disks/h/power?days=30')).toBe(true)
    expect(hdd.w.find('[data-test="standby-share"]').text()).toBe('75 %')
    const ssd = setup(disk({ id: 's', rotational: false, role: 'data', smart }), { saved: { apm: 0, writeCache: '' }, current: {} })
    await flushPromises()
    expect(ssd.calls.some((c) => c.url.includes('/power'))).toBe(false)
    expect(ssd.w.find('[data-test="standby-chart"]').exists()).toBe(false)
  })

  it('zeigt aktuelle Werte, speichert Änderungen per PUT', async () => {
    const { w, calls } = setup(disk({ id: 'h', role: 'data', smart }), { saved: { apm: 128, writeCache: '' }, current: { apm: 128, writeCache: true } })
    await flushPromises()
    expect(w.find('[data-test="tuning-current"]').text()).toContain('APM 128')
    expect(w.find('[data-test="tuning-current"]').text()).toContain('Schreibcache an')
    expect((w.find('[data-test="tuning-apm"]').element as HTMLSelectElement).value).toBe('128')
    expect(w.find('[data-test="tuning-save"]').attributes('disabled')).toBeDefined()
    await w.find('[data-test="tuning-apm"]').setValue('192')
    await w.find('[data-test="tuning-cache"]').setValue('off')
    await w.find('[data-test="tuning-form"]').trigger('submit')
    await flushPromises()
    expect(calls.filter((c) => c.method === 'PUT')).toEqual([{ url: '/api/v1/disks/h/tuning', method: 'PUT', body: JSON.stringify({ apm: 192, writeCache: 'off' }) }])
  })

  it('weckt keine schlafende Platte und zeigt den Hinweis', async () => {
    const { w } = setup(disk({ id: 'h', role: 'data', smart }), { saved: { apm: 0, writeCache: '' }, current: { standby: true } })
    await flushPromises()
    expect(w.find('[data-test="tuning-standby"]').text()).toContain('schläft')
    expect(w.find('[data-test="tuning-current"]').exists()).toBe(false)
  })

  it('bietet APM nicht für nicht zugewiesene Platten an und zeigt Ablehnungen', async () => {
    const un = setup(disk({ id: 'u', role: 'unassigned', smart }), {})
    await flushPromises()
    expect(un.w.find('[data-test="tuning-form"]').exists()).toBe(false)
    expect(un.calls.some((c) => c.url.includes('/tuning'))).toBe(false)
    const { w } = setup(disk({ id: 'h', role: 'data', smart }), { saved: { apm: 0, writeCache: '' }, current: {} }, () =>
      new Response(JSON.stringify({ error: { code: 'conflict', message: 'Diese Platte unterstützt die Einstellung nicht' } }), { status: 409 }),
    )
    await flushPromises()
    await w.find('[data-test="tuning-apm"]').setValue('64')
    await w.find('[data-test="tuning-form"]').trigger('submit')
    await flushPromises()
    expect(w.find('[data-test="tuning-error"]').text()).toContain('unterstützt die Einstellung nicht')
  })

  it('übersteht unvollständige Antworten', async () => {
    const { w } = setup(disk({ id: 'h', role: 'data', smart }), [])
    await flushPromises()
    expect(w.find('[data-test="tuning-form"]').exists()).toBe(true)
  })
})

describe('Hot-Plug: Neue Platte erkannt', () => {
  const fresh = disk({ id: 'n1', device: '/dev/sdz', model: 'WD Red', serial: 'N1', sizeBytes: 8e12, role: 'unassigned', new: true })

  function stub(rows: () => DiskView[]) {
    const calls: { url: string; method: string }[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
        const method = init?.method ?? 'GET'
        if (method !== 'GET') {
          calls.push({ url, method })
          return new Response(null, { status: 204 })
        }
        return new Response(JSON.stringify(url.includes('/disks') ? rows() : pools), { status: 200 })
      }),
    )
    return calls
  }

  it('zeigt einen Hinweis mit Aktionen und blendet ihn per „Ignorieren“ aus', async () => {
    let list = [disk({ id: 'd1', role: 'data' }), fresh]
    const calls = stub(() => list)
    const w = mount(DisksView, { global: { plugins: plugins() } })
    await flushPromises()
    const banner = w.find('[data-test="new-disk-n1"]')
    expect(banner.text()).toContain('Neue Platte erkannt')
    expect(banner.text()).toContain('WD Red')
    expect(banner.text()).toContain('8,0 TB')
    expect(w.find('[data-test="new-disk-d1"]').exists()).toBe(false)
    expect(banner.find('[data-test="new-import-n1"]').exists()).toBe(false) // kein vorhandenes Dateisystem
    list = [disk({ id: 'd1', role: 'data' }), { ...fresh, new: false }]
    await banner.find('[data-test="new-dismiss-n1"]').trigger('click')
    await flushPromises()
    expect(calls).toEqual([{ url: '/api/v1/disks/n1/dismiss', method: 'POST' }])
    expect(w.find('[data-test="new-disk-n1"]').exists()).toBe(false)
  })

  it('bietet „Übernehmen“ nur bei erkennbarem Dateisystem und „Formatieren“ immer', async () => {
    const withFs = { ...fresh, partitions: [{ device: '/dev/sdz1', fsType: 'ext4', uuid: 'u', label: '', sizeBytes: 1, mountpoints: [] }] }
    stub(() => [withFs])
    const w = mount(DisksView, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="new-import-n1"]').exists()).toBe(true)
    expect(w.find('[data-test="new-format-n1"]').exists()).toBe(true)
  })

})

describe('Dateisystem-Prüfung', () => {
  function setupFs(d: DiskView) {
    const posts: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
        if (init?.method === 'POST') {
          posts.push(url)
          return new Response(null, { status: 202 })
        }
        return new Response(url.includes('/history') ? '[]' : JSON.stringify([d]), { status: 200 })
      }),
    )
    const pinia = createPinia()
    setActivePinia(pinia)
    useStorageStore().disks = [d]
    const w = mount(DiskDetailDialog, { props: { visible: true, disk: d }, global: { plugins: [pinia, PrimeVue, ToastService] } })
    return { w, posts }
  }

  it('startet die Prüfung für eine Datenplatte', async () => {
    const { w, posts } = setupFs(disk({ id: 'd2', role: 'data', fsType: 'ext4' }))
    await flushPromises()
    await w.find('[data-test="fscheck-start"]').trigger('click')
    await flushPromises()
    expect(posts).toContain('/api/v1/disks/d2/fscheck')
  })

  it('zeigt Lauf, Ergebnis mit Online-Hinweis und Ausgabe', async () => {
    const running = setupFs(disk({ id: 'd2', role: 'data', fsType: 'xfs', fsCheck: { running: true } }))
    await flushPromises()
    expect(running.w.find('[data-test="fscheck-running"]').exists()).toBe(true)
    expect(running.w.find('[data-test="fscheck-start"]').exists()).toBe(false)
    running.w.unmount()

    const done = setupFs(disk({ id: 'd2', role: 'data', fsType: 'ext4', fsCheck: { running: false, result: 'errors', online: true, tool: 'e2fsck', output: 'Inode 12 kaputt', finishedAt: '2026-11-19T10:00:00Z' } }))
    await flushPromises()
    expect(done.w.find('[data-test="fscheck-last"]').text()).toContain('Fehler gefunden')
    expect(done.w.find('[data-test="fscheck-online-hint"]').exists()).toBe(true)
    expect(done.w.find('[data-test="fscheck-output"]').text()).toContain('Inode 12 kaputt')
  })

  it('bietet die Prüfung nicht für die Systemplatte oder fehlende Platten an', async () => {
    const sys = setupFs(disk({ id: 's', role: 'system', fsType: 'ext4' }))
    await flushPromises()
    expect(sys.w.find('[data-test="fscheck"]').exists()).toBe(false)
    sys.w.unmount()
    const missing = setupFs(disk({ id: 'm', role: 'data', fsType: 'ext4', state: 'missing' }))
    await flushPromises()
    expect(missing.w.find('[data-test="fscheck"]').exists()).toBe(false)
  })
})

describe('Benchmark', () => {
  function setupBench(d: DiskView) {
    const posts: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
        if (init?.method === 'POST') {
          posts.push(url)
          return new Response(null, { status: 202 })
        }
        return new Response(url.includes('/history') ? '[]' : JSON.stringify([d]), { status: 200 })
      }),
    )
    const pinia = createPinia()
    setActivePinia(pinia)
    useStorageStore().disks = [d]
    const w = mount(DiskDetailDialog, { props: { visible: true, disk: d }, global: { plugins: [pinia, PrimeVue, ToastService] } })
    return { w, posts }
  }
  const run = (avg: number) => ({
    at: '2026-11-19T10:00:00Z', avgMBps: avg, minMBps: avg - 20, maxMBps: avg + 20,
    samples: [{ percent: 0, mbps: avg + 20 }, { percent: 50, mbps: avg }, { percent: 100, mbps: avg - 20 }],
  })

  it('startet den Benchmark', async () => {
    const { w, posts } = setupBench(disk({ id: 'd1', role: 'data' }))
    await flushPromises()
    await w.find('[data-test="bench-start"]').trigger('click')
    await flushPromises()
    expect(posts).toContain('/api/v1/disks/d1/bench')
  })

  it('zeigt Balken und den Vergleich mit dem vorherigen Lauf', async () => {
    const { w } = setupBench(disk({ id: 'd1', role: 'data', bench: { running: false, last: run(100), history: [{ at: 'a', avgMBps: 150 }, { at: 'b', avgMBps: 100 }] } }))
    await flushPromises()
    expect(w.findAll('[data-test="bench-bars"] li').length).toBe(3)
    expect(w.find('[data-test="bench-last"]').text()).toContain('100 MB/s')
    expect(w.find('[data-test="bench-delta"]').text()).toContain('-33 %')
    expect(w.find('[data-test="bench-warn"]').exists()).toBe(true)
  })

  it('zeigt Lauf und Fehler, ohne Knopf während des Laufs', async () => {
    const a = setupBench(disk({ id: 'd1', role: 'data', bench: { running: true } }))
    await flushPromises()
    expect(a.w.find('[data-test="bench-running"]').exists()).toBe(true)
    expect(a.w.find('[data-test="bench-start"]').exists()).toBe(false)
    a.w.unmount()
    const b = setupBench(disk({ id: 'd1', role: 'data', bench: { running: false, error: 'Die Platte lässt sich nicht lesen.' } }))
    await flushPromises()
    expect(b.w.find('[data-test="bench-error"]').text()).toContain('nicht lesen')
  })
})

describe('Ersatzplatte', () => {
  function setupSpare(d: DiskView) {
    const puts: { url: string; body: string }[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
        if (init?.method === 'PUT') {
          puts.push({ url, body: init.body as string })
          return new Response(null, { status: 204 })
        }
        return new Response(url.includes('/history') ? '[]' : JSON.stringify([d]), { status: 200 })
      }),
    )
    const pinia = createPinia()
    setActivePinia(pinia)
    useStorageStore().disks = [d]
    const w = mount(DiskDetailDialog, { props: { visible: true, disk: d }, global: { plugins: [pinia, PrimeVue, ToastService] } })
    return { w, puts }
  }

  it('merkt eine unbenutzte Platte als Ersatzplatte vor', async () => {
    const { w, puts } = setupSpare(disk({ id: 'd9', role: 'data' }))
    await flushPromises()
    await w.find('[data-test="spare-toggle"]').setValue(true)
    await flushPromises()
    expect(puts).toEqual([{ url: '/api/v1/disks/d9/spare', body: JSON.stringify({ spare: true }) }])
  })

  it('bietet es nicht für Platten im Pool oder die Systemplatte an', async () => {
    const pooled = setupSpare(disk({ id: 'd1', role: 'data', pool: 'media' }))
    await flushPromises()
    expect(pooled.w.find('[data-test="spare"]').exists()).toBe(false)
    pooled.w.unmount()
    const sys = setupSpare(disk({ id: 's', role: 'system', system: true }))
    await flushPromises()
    expect(sys.w.find('[data-test="spare"]').exists()).toBe(false)
  })
})
