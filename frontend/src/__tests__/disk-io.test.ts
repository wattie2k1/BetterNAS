import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import DisksView from '@/views/storage/DisksView.vue'
import { ioRates } from '@/utils/diskio'

const st = (at: string, r: number, w: number, busy: number) => ({ at, devices: { sda: { readBytes: r, writeBytes: w, busyMs: busy } } })

describe('ioRates', () => {
  it('bildet Raten und Auslastung aus zwei Messungen', () => {
    const r = ioRates(st('2026-01-01T00:00:00Z', 0, 0, 0), st('2026-01-01T00:00:02Z', 20e6, 4e6, 1000))
    expect(r.sda.readBps).toBe(10e6)
    expect(r.sda.writeBps).toBe(2e6)
    expect(r.sda.busyPct).toBe(50)
  })
  it('lässt Geräte mit zurückgesetzten Zählern oder ohne Vorwert aus', () => {
    const prev = st('2026-01-01T00:00:00Z', 100, 100, 100)
    expect(ioRates(prev, st('2026-01-01T00:00:02Z', 50, 100, 100))).toEqual({})
    expect(ioRates({ at: prev.at, devices: {} }, st('2026-01-01T00:00:02Z', 1, 1, 1))).toEqual({})
    expect(ioRates(prev, st('2026-01-01T00:00:00Z', 200, 200, 200))).toEqual({})
  })
  it('deckelt die Auslastung bei 100 %', () => {
    expect(ioRates(st('2026-01-01T00:00:00Z', 0, 0, 0), st('2026-01-01T00:00:01Z', 0, 0, 5000)).sda.busyPct).toBe(100)
  })
})

describe('DisksView: Live-I/O', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })
  it('zeigt Raten erst nach der zweiten Messung', async () => {
    const disk = { id: 'd1', device: '/dev/sda', model: 'X', serial: 's', sizeBytes: 1e12, role: 'data', state: 'online', rotational: true, partitions: [] }
    let n = 0
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string) => {
        if (url.endsWith('/disks/io')) {
          n++
          return new Response(JSON.stringify(n === 1 ? st('2026-01-01T00:00:00Z', 0, 0, 0) : st('2026-01-01T00:00:02Z', 20e6, 0, 1000)), { status: 200 })
        }
        return new Response(url.endsWith('/disks') ? JSON.stringify([disk]) : '[]', { status: 200 })
      }),
    )
    vi.useFakeTimers()
    setActivePinia(createPinia())
    const w = mount(DisksView, { global: { plugins: [createPinia(), PrimeVue, ToastService] } })
    await vi.advanceTimersByTimeAsync(0)
    expect(w.find('[data-test="io-d1"]').exists()).toBe(false)
    await w.find('[data-test="live-toggle"]').setValue(true)
    await vi.advanceTimersByTimeAsync(0)
    expect(w.find('[data-test="io-d1"]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(2100)
    expect(w.find('[data-test="io-d1"]').text()).toContain('10,0 MB/s')
    expect(w.find('[data-test="io-d1"]').text()).toContain('50 %')
    await w.find('[data-test="live-toggle"]').setValue(false)
    await vi.advanceTimersByTimeAsync(0)
    expect(w.find('[data-test="io-d1"]').exists()).toBe(false)
    w.unmount()
  })
})
