import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { TRACK_POLL_MS, useJobsStore, type Job } from '@/stores/jobs'
import { pollWhileVisible } from '@/utils/poll'
import { connectEvents } from '@/api/ws'
import JobsPanel from '@/components/JobsPanel.vue'

const job = (over: Partial<Job> = {}): Job => ({
  id: 'j1', kind: 'pool.create', target: 'media', status: 'running', progress: 0, log: '', startedBy: 'admin', startedAt: '2026-01-01T00:00:00Z', ...over,
})

beforeEach(() => {
  setActivePinia(createPinia())
  vi.useFakeTimers()
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('Jobs-Store', () => {
  it('fragt einen Auftrag ab, wenn das WebSocket-Ereignis ausbleibt', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(job({ status: 'succeeded' })), { status: 200 })))
    const p = useJobsStore().track('j1')
    await vi.advanceTimersByTimeAsync(TRACK_POLL_MS + 10)
    await expect(p).resolves.toMatchObject({ status: 'succeeded' })
  })

  it('löst mehrere Wartende auf denselben Auftrag alle auf', async () => {
    const s = useJobsStore()
    const a = s.track('j1')
    const b = s.track('j1')
    s.update(job({ status: 'succeeded' }))
    await expect(Promise.all([a, b])).resolves.toHaveLength(2)
  })

  it('load() löst Wartende auf, deren Auftrag inzwischen fertig ist', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify([job({ status: 'failed', log: 'a\nboom' })]), { status: 200 })))
    const s = useJobsStore()
    const p = s.track('j1')
    const caught = p.catch((e: Error) => e.message)
    await s.load()
    await expect(caught).resolves.toBe('boom')
  })
})

describe('pollWhileVisible', () => {
  it('pausiert bei verstecktem Tab und holt beim Sichtbarwerden sofort nach', () => {
    const fn = vi.fn()
    const stop = pollWhileVisible(fn, 1000)
    const hidden = vi.spyOn(document, 'hidden', 'get')
    hidden.mockReturnValue(true)
    vi.advanceTimersByTime(3000)
    expect(fn).not.toHaveBeenCalled()
    hidden.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(fn).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(1000)
    expect(fn).toHaveBeenCalledTimes(2)
    stop()
    vi.advanceTimersByTime(5000)
    expect(fn).toHaveBeenCalledTimes(2)
    hidden.mockRestore()
  })
})

describe('connectEvents', () => {
  it('meldet nur ein erneutes Verbinden, nicht das erste', () => {
    const sockets: { onopen?: () => void; onclose?: () => void; close: () => void }[] = []
    vi.stubGlobal('WebSocket', function () {
      const s = { close: () => {} }
      sockets.push(s)
      return s
    })
    const onReconnect = vi.fn()
    const stop = connectEvents(() => {}, onReconnect)
    sockets[0].onopen?.()
    expect(onReconnect).not.toHaveBeenCalled()
    sockets[0].onclose?.()
    vi.advanceTimersByTime(1000)
    sockets[1].onopen?.()
    expect(onReconnect).toHaveBeenCalledTimes(1)
    stop()
  })
})

describe('JobsPanel', () => {
  it('stürzt bei unbekanntem Status nicht ab', () => {
    const s = useJobsStore()
    s.jobs = [job({ status: 'cancelled' as Job['status'] })]
    const w = mount(JobsPanel, { global: { stubs: { RouterLink: true } } })
    expect(w.text()).toContain('cancelled')
  })
})
