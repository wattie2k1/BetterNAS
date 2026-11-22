import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import Toast from 'primevue/toast'
import UpdatesView from '@/views/system/UpdatesView.vue'
import { POLL_INTERVAL_MS, useUpdatesStore } from '@/stores/updates'

const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))
const overview = (state: string, packages: unknown[] = [], log = '', channel = 'stable') => ({ packages, checkedAt: '2026-11-15T16:00:00Z', run: { state, log }, channel })

beforeEach(() => vi.useFakeTimers())
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

/** Antwortet auf die Aufrufe der Reihe nach; ein Fehler (Error) wird als Verbindungsabbruch geworfen. */
function script(routes: Record<string, (() => Promise<Response> | Response)[]>) {
  const counters: Record<string, number> = {}
  const fn = vi.fn().mockImplementation((url: string) => {
    const list = routes[url]
    if (!list) return json({}, 404)
    const i = Math.min(counters[url] ?? 0, list.length - 1)
    counters[url] = (counters[url] ?? 0) + 1
    return Promise.resolve(list[i]())
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
const fail = () => {
  throw new TypeError('Failed to fetch')
}
const ok = (body: unknown) => () => new Response(JSON.stringify(body), { status: 200 })

describe('Updates: Installation übersteht den Neustart von BetterNAS', () => {
  it('pollt weiter, solange die Verbindung fehlt, und meldet danach Erfolg', async () => {
    setActivePinia(createPinia())
    script({
      '/api/v1/system/info': [ok({ version: '0.0.8-dev' }), ok({ version: '0.0.8-dev' }), ok({ version: '0.1.0' })],
      '/api/v1/updates': [ok(overview('running')), ok(overview('succeeded', [], 'fertig'))],
      '/api/v1/updates/install': [() => new Response(JSON.stringify({ unit: 'betternas-update-1' }), { status: 202 })],
      '/api/v1/updates/run': [ok({ state: 'running', log: '' }), fail, fail, ok({ state: 'running', log: '' }), ok({ state: 'succeeded', log: 'fertig' })],
    })
    const store = useUpdatesStore()
    await store.install()
    await flushPromises()
    expect(store.installing).toBe(true)

    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS) // 1. Abfrage lief, 2. schlägt fehl
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS)
    expect(store.restarting).toBe(true)
    expect(store.installing).toBe(true)

    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 3)
    await flushPromises()
    expect(store.restarting).toBe(false)
    expect(store.installing).toBe(false)
    expect(store.overview.run.state).toBe('succeeded')
    expect(store.reloadNeeded).toBe(true) // neue Version → Oberfläche neu laden
  })

  it('lädt nicht neu, wenn die Version gleich bleibt', async () => {
    setActivePinia(createPinia())
    script({
      '/api/v1/system/info': [ok({ version: '0.1.0' })],
      '/api/v1/updates': [ok(overview('succeeded'))],
      '/api/v1/updates/install': [() => new Response(JSON.stringify({ unit: 'u' }), { status: 202 })],
      '/api/v1/updates/run': [ok({ state: 'succeeded', log: '' })],
    })
    const store = useUpdatesStore()
    await store.install()
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS)
    await flushPromises()
    expect(store.reloadNeeded).toBe(false)
  })

  it('meldet einen fehlgeschlagenen Lauf', async () => {
    setActivePinia(createPinia())
    script({
      '/api/v1/system/info': [ok({ version: '0.1.0' })],
      '/api/v1/updates': [ok(overview('failed', [], 'dpkg: Fehler'))],
      '/api/v1/updates/install': [() => new Response(JSON.stringify({ unit: 'u' }), { status: 202 })],
      '/api/v1/updates/run': [ok({ state: 'failed', log: 'dpkg: Fehler' })],
    })
    const store = useUpdatesStore()
    await store.install()
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS)
    await flushPromises()
    expect(store.overview.run.state).toBe('failed')
    expect(store.installing).toBe(false)
  })

  it('setzt einen laufenden Lauf beim Öffnen der Seite fort', async () => {
    setActivePinia(createPinia())
    script({
      '/api/v1/system/info': [ok({ version: '0.1.0' })],
      '/api/v1/updates': [ok(overview('running')), ok(overview('succeeded'))],
      '/api/v1/updates/run': [ok({ state: 'running', log: '' }), ok({ state: 'succeeded', log: '' })],
    })
    const store = useUpdatesStore()
    await store.load()
    await flushPromises()
    expect(store.installing).toBe(true)
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 2)
    await flushPromises()
    expect(store.installing).toBe(false)
    expect(store.overview.run.state).toBe('succeeded')
  })
})

describe('UpdatesView', () => {
  const mountView = () => mount(UpdatesView, { global: { plugins: [createPinia(), PrimeVue, ToastService] } })

  it('sperrt „Installieren“ ohne anstehende Updates und zeigt „Alles aktuell“', async () => {
    script({ '/api/v1/updates': [ok(overview('none', []))] })
    const w = mountView()
    await flushPromises()
    expect(w.find('button[data-test="updates-install"]').attributes('disabled')).toBeDefined()
    expect(w.text()).toContain('Alles auf dem neuesten Stand')
  })

  it('listet Pakete und zeigt den Neustart-Hinweis während der Verbindungsabbrüche', async () => {
    setActivePinia(createPinia())
    script({
      '/api/v1/system/info': [ok({ version: '0.0.8-dev' })],
      '/api/v1/updates': [ok(overview('none', [{ name: 'betternas', current: '0.0.8-dev', candidate: '0.1.0' }])), ok(overview('running'))],
      '/api/v1/updates/install': [() => new Response(JSON.stringify({ unit: 'u' }), { status: 202 })],
      '/api/v1/updates/run': [fail],
    })
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('betternas')
    expect(w.find('button[data-test="updates-install"]').attributes('disabled')).toBeUndefined()
    const store = useUpdatesStore()
    await store.install()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(10)
    expect(w.find('[data-test="updates-restarting"]').exists()).toBe(true)
  })
})

describe('UpdatesView: Update-Kanal', () => {
  const mountView = () => mount(UpdatesView, { global: { plugins: [createPinia(), PrimeVue, ToastService] } })
  const putOk = () => new Response(null, { status: 204 })

  it('zeigt den Umschalter mit dem aktuellen Kanal', async () => {
    script({ '/api/v1/updates': [ok(overview('none', [], '', 'stable'))] })
    const w = mountView()
    await flushPromises()
    const sel = w.find('[data-test="updates-channel"]')
    expect(sel.exists()).toBe(true)
    expect(sel.find('[aria-pressed="true"]').text()).toContain('Stabil')
    expect(sel.text()).toContain('Testing')
  })

  it('blendet den Umschalter aus, wenn keine Quelle eingerichtet ist', async () => {
    script({ '/api/v1/updates': [ok(overview('none', [], '', ''))] })
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-test="updates-channel"]').exists()).toBe(false)
  })

  it('wechselt den Kanal per PUT und lädt neu', async () => {
    const fn = script({ '/api/v1/updates': [ok(overview('none', [], '', 'stable')), ok(overview('none', [], '', 'testing'))], '/api/v1/updates/channel': [putOk] })
    const w = mountView()
    await flushPromises()
    const buttons = w.findAll('[data-test="updates-channel"] [role="button"], [data-test="updates-channel"] button')
    const testing = buttons.find((b) => b.text().includes('Testing'))!
    await testing.trigger('click')
    await flushPromises()
    const put = fn.mock.calls.find((c) => c[0] === '/api/v1/updates/channel')
    expect(put).toBeTruthy()
    expect((put![1] as RequestInit).method).toBe('PUT')
    expect(JSON.parse((put![1] as RequestInit).body as string)).toEqual({ channel: 'testing' })
    expect(useUpdatesStore().overview.channel).toBe('testing')
    expect(w.find('[data-test="updates-channel"] [aria-pressed="true"]').text()).toContain('Testing')
  })

  it('zeigt den Fehler als Toast und behält den Kanal, wenn der Wechsel scheitert', async () => {
    script({
      '/api/v1/updates': [ok(overview('none', [], '', 'stable'))],
      '/api/v1/updates/channel': [() => new Response(JSON.stringify({ error: { code: 'conflict', message: 'Die BetterNAS-Paketquelle wurde manuell angepasst.' } }), { status: 409 })],
    })
    const Page = defineComponent({ render: () => [h(Toast), h(UpdatesView)] })
    const w = mount(Page, { attachTo: document.body, global: { plugins: [createPinia(), PrimeVue, ToastService] } })
    await flushPromises()
    const buttons = w.findAll('[data-test="updates-channel"] [role="button"], [data-test="updates-channel"] button')
    await buttons.find((b) => b.text().includes('Testing'))!.trigger('click')
    await flushPromises()
    expect(useUpdatesStore().overview.channel).toBe('stable')
    expect(document.body.textContent).toContain('Kanalwechsel fehlgeschlagen')
  })

  it('sperrt den Umschalter während der Installation', async () => {
    script({ '/api/v1/updates': [ok(overview('running', [], '', 'stable'))], '/api/v1/updates/run': [ok({ state: 'running', log: '' })], '/api/v1/system/info': [ok({ version: '1' })] })
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-test="updates-channel"] [aria-pressed]').attributes('aria-disabled') ?? w.find('[data-test="updates-channel"] [aria-pressed]').attributes('disabled')).toBeDefined()
  })
})

describe('UpdatesView: automatische Updates', () => {
  const mountView = () => mount(UpdatesView, { global: { plugins: [createPinia(), PrimeVue, ToastService] } })
  const withAuto = (auto: unknown) => ({ ...overview('none', [], '', 'stable'), auto })
  const lastRun = { at: '2026-11-22T05:15:48Z', packages: ['libc6', 'sudo'], result: 'ok' }

  it('zeigt den letzten automatischen Lauf mit Paketliste und Neustart-Hinweis', async () => {
    script({
      '/api/v1/updates': [ok(withAuto({ enabled: true, lastRun }))],
      '/api/v1/system/reboot-required': [ok({ required: true, packages: ['linux-image'] })],
    })
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-test="updates-auto-last"]').text()).toContain('2 Pakete')
    expect(w.find('[data-test="updates-auto-pkgs"]').exists()).toBe(false)
    await w.find('[data-test="updates-auto-toggle"]').trigger('click')
    expect(w.find('[data-test="updates-auto-pkgs"]').text()).toBe('libc6, sudo')
    expect(w.find('[data-test="updates-auto-reboot"]').exists()).toBe(true)
  })

  it('schaltet per PUT um und lädt neu', async () => {
    const fn = script({
      '/api/v1/updates': [ok(withAuto({ enabled: true, lastRun: null })), ok(withAuto({ enabled: false, lastRun: null }))],
      '/api/v1/updates/auto': [() => new Response(null, { status: 204 })],
    })
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-test="updates-auto-none"]').exists()).toBe(true)
    await w.find('[data-test="updates-auto-switch"] input').setValue(false)
    await flushPromises()
    const put = fn.mock.calls.find((c) => c[0] === '/api/v1/updates/auto')
    expect(put?.[1]?.method).toBe('PUT')
    expect(JSON.parse(put?.[1]?.body as string)).toEqual({ enabled: false })
    expect(useUpdatesStore().overview.auto.enabled).toBe(false)
  })
})
