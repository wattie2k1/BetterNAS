import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PowerOverlay from '@/components/PowerOverlay.vue'
import { hooks, PROBE_INTERVAL_MS, usePowerStore } from '@/stores/power'

const up = () => new Response('{}', { status: 401 })

beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

async function tick(n = 1) {
  for (let i = 0; i < n; i++) {
    await vi.advanceTimersByTimeAsync(PROBE_INTERVAL_MS)
  }
}

describe('Neustart-Overlay', () => {
  it('lädt die Seite erst neu, wenn der Server zuerst weg und dann wieder erreichbar war', async () => {
    const reload = vi.fn()
    const responses: Array<() => Response | Promise<Response>> = [up, up, () => Promise.reject(new TypeError('offline')), () => Promise.reject(new TypeError('offline')), up]
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => responses.shift()!()))
    const p = usePowerStore()
    hooks.reload = reload
    p.begin('reboot')
    expect(p.phase).toBe('rebooting')
    await tick(2) // Server antwortet noch (Neustart ist verzögert) → kein Reload
    expect(reload).not.toHaveBeenCalled()
    await tick(2) // weg
    expect(p.sawDown).toBe(true)
    expect(reload).not.toHaveBeenCalled()
    await tick(1) // wieder da
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('zeigt das Overlay mit passendem Text', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(up()))
    const p = usePowerStore()
    const w = mount(PowerOverlay)
    expect(w.find('[data-test="power-overlay"]').exists()).toBe(false)
    p.begin('reboot')
    await flushPromises()
    expect(w.find('[data-test="power-overlay"]').text()).toContain('startet neu')
    p.stop()
  })

  it('Herunterfahren: wartet, bis der Server weg ist, und lädt nie neu', async () => {
    const reload = vi.fn()
    const responses: Array<() => Response | Promise<Response>> = [up, () => Promise.reject(new TypeError('offline'))]
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => (responses.length ? responses.shift()! : () => Promise.reject(new TypeError('offline')))()))
    const p = usePowerStore()
    hooks.reload = reload
    p.begin('poweroff')
    expect(p.phase).toBe('shuttingDown')
    await tick(3)
    expect(p.phase).toBe('off')
    expect(reload).not.toHaveBeenCalled()
  })

  it('meldet nach langer Wartezeit, dass der Server nicht antwortet, und wartet weiter', async () => {
    const reload = vi.fn()
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    const p = usePowerStore()
    hooks.reload = reload
    p.begin('reboot')
    await tick(200)
    expect(p.slow).toBe(true)
    expect(p.phase).toBe('rebooting')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(up()))
    await tick(1)
    expect(reload).toHaveBeenCalledTimes(1)
  })
})
