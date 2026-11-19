import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import DisksView from '@/views/storage/DisksView.vue'
import { runBulk } from '@/utils/bulk'
import type { DiskView } from '@/api/storage'
import { disk, pools } from './fixtures'

const plugins = () => [createPinia(), PrimeVue, ToastService]
afterEach(() => vi.unstubAllGlobals())

const smart = { passed: true, temperatureC: 40, powerOnHours: 1, standby: false }
const rows = (): DiskView[] => [
  disk({ id: 'sys', device: '/dev/nvme0n1', model: 'Samsung SSD', role: 'system', system: true, manageable: false, rotational: false, smart }),
  disk({ id: 'd1', device: '/dev/sda', model: 'HDD1', role: 'data', smart }),
  disk({ id: 'd2', device: '/dev/sdb', model: 'HDD2', role: 'data', smart: { ...smart, selfTest: { running: true, remainingPercent: 50 } } }),
  disk({ id: 'd3', device: '/dev/sdc', model: 'HDD3', role: 'unassigned' }),
  disk({ id: 'd4', device: '', model: 'Fehlt', role: 'data', state: 'missing' }),
]

function stub(failOn: string[] = []) {
  const calls: { url: string; method: string; body?: string }[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (method === 'GET') return new Response(JSON.stringify(url.includes('/disks') ? rows() : pools), { status: 200 })
      calls.push({ url, method, body: init?.body as string | undefined })
      if (failOn.some((f) => url.includes(f))) return new Response(JSON.stringify({ error: { code: 'conflict', message: 'läuft schon' } }), { status: 409 })
      return new Response(null, { status: 204 })
    }),
  )
  return calls
}

async function mountView() {
  const w = mount(DisksView, { global: { plugins: plugins() } })
  await flushPromises()
  return w
}
const pick = async (w: Awaited<ReturnType<typeof mountView>>, ...ids: string[]) => {
  for (const id of ids) await w.find(`[data-test="select-${id}"]`).setValue(true)
}

describe('runBulk', () => {
  it('sammelt Erfolge und Fehler, ohne beim ersten Fehler abzubrechen', async () => {
    const res = await runBulk(['a', 'b', 'c'], async (id) => {
      if (id === 'b') throw new Error('kaputt')
    })
    expect(res.done).toEqual(['a', 'c'])
    expect(res.failed).toEqual([{ id: 'b', message: 'kaputt' }])
  })
})

describe('DisksView: Mehrfachauswahl', () => {
  it('zeigt Auswahlfelder nur für angeschlossene Platten und die Leiste erst bei einer Auswahl', async () => {
    stub()
    const w = await mountView()
    expect(w.find('[data-test="bulk-bar"]').exists()).toBe(false)
    expect(w.find('[data-test="select-d4"]').exists()).toBe(false)
    await pick(w, 'd1', 'd3')
    expect(w.find('[data-test="bulk-count"]').text()).toBe('2 ausgewählt')
  })

  it('„Alle“ wählt alle angeschlossenen Platten und hebt die Auswahl wieder auf', async () => {
    stub()
    const w = await mountView()
    await w.find('[data-test="select-all"]').setValue(true)
    expect(w.find('[data-test="bulk-count"]').text()).toBe('4 ausgewählt')
    await w.find('[data-test="bulk-clear"]').trigger('click')
    expect(w.find('[data-test="bulk-bar"]').exists()).toBe(false)
    await pick(w, 'd1')
    expect((w.find('[data-test="select-all"]').element as HTMLInputElement).indeterminate).toBe(true)
  })

  it('zählt nur passende Platten je Aktion und startet nur dort Kurztests', async () => {
    const calls = stub()
    const w = await mountView()
    await pick(w, 'sys', 'd1', 'd2', 'd3')
    expect(w.find('[data-test="bulk-short"]').text()).toBe('Kurztest (2)') // sys + d1; d2 läuft schon, d3 ohne SMART
    expect(w.find('[data-test="bulk-abort"]').text()).toBe('Test abbrechen (1)')
    expect(w.find('[data-test="bulk-spin-apply"]').text()).toBe('Ruhezustand (2)') // d1, d2 (Daten)
    await w.find('[data-test="bulk-short"]').trigger('click')
    await flushPromises()
    expect(calls.map((c) => [c.method, c.url, c.body]).sort()).toEqual([
      ['POST', '/api/v1/disks/d1/selftest', JSON.stringify({ kind: 'short' })],
      ['POST', '/api/v1/disks/sys/selftest', JSON.stringify({ kind: 'short' })],
    ])
  })

  it('bricht laufende Tests ab und setzt den Ruhezustand für Daten- und Paritätsplatten', async () => {
    const calls = stub()
    const w = await mountView()
    await pick(w, 'd1', 'd2', 'd3')
    await w.find('[data-test="bulk-abort"]').trigger('click')
    await flushPromises()
    expect(calls).toEqual([{ url: '/api/v1/disks/d2/selftest', method: 'DELETE', body: undefined }])
    calls.length = 0
    await w.find('[data-test="bulk-spin"]').setValue('60')
    await w.find('[data-test="bulk-spin-apply"]').trigger('click')
    await flushPromises()
    expect(calls.map((c) => [c.method, c.url, c.body]).sort()).toEqual([
      ['PUT', '/api/v1/disks/d1/spindown', JSON.stringify({ minutes: 60 })],
      ['PUT', '/api/v1/disks/d2/spindown', JSON.stringify({ minutes: 60 })],
    ])
  })

  it('lässt Platten blinken und sperrt Aktionen ohne passende Platte', async () => {
    const calls = stub()
    const w = await mountView()
    await pick(w, 'd3') // nicht zugewiesen, ohne SMART
    expect(w.find('[data-test="bulk-short"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="bulk-spin-apply"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="bulk-abort"]').exists()).toBe(false)
    await w.find('[data-test="bulk-identify"]').trigger('click')
    await flushPromises()
    expect(calls).toEqual([{ url: '/api/v1/disks/d3/identify', method: 'POST', body: undefined }])
  })

  it('macht bei Teilfehlern weiter und lässt die Auswahl stehen', async () => {
    const calls = stub(['/d1/'])
    const w = await mountView()
    await pick(w, 'sys', 'd1')
    await w.find('[data-test="bulk-long"]').trigger('click')
    await flushPromises()
    expect(calls.length).toBe(2) // beide Platten wurden versucht
    expect(w.find('[data-test="bulk-count"]').text()).toBe('2 ausgewählt')
  })
})
