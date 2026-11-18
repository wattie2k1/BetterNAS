import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import StackDialog from '@/components/docker/StackDialog.vue'
import AutoUpdateDialog from '@/components/docker/AutoUpdateDialog.vue'
import StacksView from '@/views/docker/StacksView.vue'
import ContainersView from '@/views/docker/ContainersView.vue'
import { STACK_TEMPLATE } from '@/api/docker'

// Monaco braucht einen echten Browser; der Editor wird durch ein Textfeld ersetzt.
vi.mock('@/components/docker/YamlEditor.vue', async () => {
  const { defineComponent, h } = await import('vue')
  return {
    __esModule: true,
    default: defineComponent({
      props: ['modelValue'],
      emits: ['update:modelValue'],
      setup: (props, { emit }) => () =>
        h('textarea', {
          'data-test': 'compose',
          value: props.modelValue,
          onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLTextAreaElement).value),
        }),
    }),
  }
})

const plugins = () => [createPinia(), PrimeVue, ToastService]
afterEach(() => vi.unstubAllGlobals())

describe('StackDialog', () => {
  const mountDialog = (props = {}) =>
    mount(StackDialog, { props: { visible: true, stack: null, existing: ['media'], ...props }, global: { plugins: plugins() } })

  it('startet mit einer Vorlage und verlangt einen gültigen Namen', async () => {
    const w = mountDialog()
    await flushPromises()
    expect((w.find('textarea[data-test="compose"]').element as HTMLTextAreaElement).value).toBe(STACK_TEMPLATE)
    expect(w.find('button[data-test="stack-save"]').attributes('disabled')).toBeDefined()
    await w.find('input#stack-name').setValue('Foto Server')
    expect(w.text()).toContain('Nur Kleinbuchstaben')
    await w.find('input#stack-name').setValue('media')
    expect(w.text()).toContain('existiert bereits')
    await w.find('input#stack-name').setValue('fotos')
    expect(w.find('button[data-test="stack-save"]').attributes('disabled')).toBeUndefined()
  })

  it('sendet Dateien und Modus', async () => {
    const w = mountDialog()
    await flushPromises()
    await w.find('input#stack-name').setValue('fotos')
    await w.find('textarea[data-test="compose"]').setValue('services: {}\n')
    await w.find('button[data-test="stack-start"]').trigger('click')
    await w.find('button[data-test="stack-check"]').trigger('click')
    expect(w.emitted('submit')).toEqual([
      [{ name: 'fotos', compose: 'services: {}\n', env: '' }, 'start'],
      [{ name: 'fotos', compose: 'services: {}\n', env: '' }, 'check'],
    ])
  })

  it('sperrt den Namen bei vorhandenen Stacks und zeigt den Compose-Fehler', async () => {
    const w = mountDialog({ stack: { name: 'media', compose: 'a: 1', env: 'X=1' }, check: { ok: false, message: 'services.web.image fehlt' } })
    await flushPromises()
    expect(w.find('input#stack-name').attributes('disabled')).toBeDefined()
    expect(w.text()).toContain('services.web.image fehlt')
  })
})

describe('AutoUpdateDialog', () => {
  const config = { enabled: true, time: '03:30', stacks: ['media', 'gone'] }
  const mountAuto = (props = {}) =>
    mount(AutoUpdateDialog, { props: { visible: true, config, stacks: ['media', 'wiki'], ...props }, global: { plugins: plugins() } })

  it('übernimmt die Einstellungen, lässt gelöschte Stacks weg und sendet Änderungen', async () => {
    const w = mountAuto()
    await flushPromises()
    expect((w.find('input[data-test="au-time"]').element as HTMLInputElement).value).toBe('03:30')
    expect((w.find('input[data-test="au-stack-media"]').element as HTMLInputElement).checked).toBe(true)
    expect((w.find('input[data-test="au-stack-wiki"]').element as HTMLInputElement).checked).toBe(false)
    await w.find('input[data-test="au-stack-wiki"]').setValue(true)
    await w.find('input[data-test="au-time"]').setValue('05:15')
    await w.find('button[data-test="au-save"]').trigger('click')
    expect(w.emitted('save')).toEqual([[{ enabled: true, time: '05:15', stacks: ['media', 'wiki'] }]])
  })

  it('sperrt Speichern bei ungültiger Uhrzeit und „Jetzt ausführen“ ohne gespeicherte Stacks', async () => {
    const w = mountAuto({ config: { enabled: false, time: '04:00', stacks: [] } })
    await flushPromises()
    expect(w.find('button[data-test="au-run"]').attributes('disabled')).toBeDefined()
    await w.find('input[data-test="au-time"]').setValue('')
    expect(w.find('button[data-test="au-save"]').attributes('disabled')).toBeDefined()
  })
})

function stubApi(routes: Record<string, unknown>) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    const v = routes[key]
    if (v === undefined) return Promise.resolve(new Response(JSON.stringify({ error: { code: 'not_found', message: key } }), { status: 404 }))
    return Promise.resolve(new Response(JSON.stringify(v), { status: key.startsWith('POST') && url.match(/\/(up|stop|restart|update|rollback|run)$/) ? 202 : 200 }))
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
const active = { installed: true, active: true, version: '26' }

describe('StacksView', () => {
  it('zeigt Stacks mit Laufzustand und startet einen Stack', async () => {
    const fn = stubApi({
      'GET /api/v1/docker/status': active,
      'GET /api/v1/stacks': [{ name: 'media', running: 1, total: 2 }],
      'GET /api/v1/containers': [],
      'POST /api/v1/stacks/media/up': { jobId: 'j1' },
    })
    const w = mount(StacksView, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="stack-media"]').text()).toContain('1 von 2 laufen')
    await w.find('[data-test="stack-media"] button[aria-label="Starten"]').trigger('click')
    await vi.waitFor(() => expect(fn).toHaveBeenCalledWith('/api/v1/stacks/media/up', expect.anything()))
  })

  it('bietet Zurückrollen nur mit Rollback-Stand an und fragt vorher nach', async () => {
    const fn = stubApi({
      'GET /api/v1/docker/status': active,
      'GET /api/v1/stacks': [
        { name: 'media', running: 1, total: 1, rollback: '2026-11-18T10:00:00Z' },
        { name: 'neu', running: 0, total: 0 },
      ],
      'GET /api/v1/containers': [],
      'POST /api/v1/stacks/media/rollback': { jobId: 'j2' },
    })
    const w = mount(StacksView, { global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    expect(w.find('[data-test="stack-neu"] button[aria-label="Zurückrollen"]').exists()).toBe(false)
    await w.find('[data-test="stack-media"] button[aria-label="Zurückrollen"]').trigger('click')
    await flushPromises()
    expect(fn).not.toHaveBeenCalledWith('/api/v1/stacks/media/rollback', expect.anything())
    expect(document.body.textContent).toContain('Daten in Volumes und Ordnern bleiben erhalten')
    ;(document.querySelector('[data-test="confirm-submit"]') as HTMLElement).click()
    await vi.waitFor(() => expect(fn).toHaveBeenCalledWith('/api/v1/stacks/media/rollback', expect.objectContaining({ method: 'POST' })))
    w.unmount()
  })

  it('speichert Auto-Update-Einstellungen und markiert aktivierte Stacks', async () => {
    const fn = stubApi({
      'GET /api/v1/docker/status': active,
      'GET /api/v1/stacks': [{ name: 'media', running: 1, total: 1 }, { name: 'wiki', running: 1, total: 1 }],
      'GET /api/v1/containers': [],
      'GET /api/v1/docker/autoupdate': { enabled: true, time: '04:00', stacks: ['media'] },
      'PUT /api/v1/docker/autoupdate': { enabled: true, time: '04:00', stacks: ['media', 'wiki'] },
    })
    const w = mount(StacksView, { global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    expect(w.find('[data-test="stack-media"] [data-test="auto-tag"]').exists()).toBe(true)
    expect(w.find('[data-test="stack-wiki"] [data-test="auto-tag"]').exists()).toBe(false)
    await w.find('button[data-test="auto-update"]').trigger('click')
    await flushPromises()
    ;(document.querySelector('input[data-test="au-stack-wiki"]') as HTMLInputElement).click()
    ;(document.querySelector('button[data-test="au-save"]') as HTMLElement).click()
    await vi.waitFor(() =>
      expect(fn).toHaveBeenCalledWith(
        '/api/v1/docker/autoupdate',
        expect.objectContaining({ method: 'PUT', body: JSON.stringify({ enabled: true, time: '04:00', stacks: ['media', 'wiki'] }) }),
      ),
    )
    w.unmount()
  })

  it('zeigt einen Hinweis und sperrt „Neuer Stack“, wenn Docker nicht läuft', async () => {
    stubApi({ 'GET /api/v1/docker/status': { installed: true, active: false, version: '' }, 'GET /api/v1/stacks': [] })
    const w = mount(StacksView, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="docker-down"]').text()).toContain('läuft nicht')
    expect(w.find('button.p-button').attributes('disabled')).toBeDefined()
  })

  it('löscht erst nach Bestätigung und nennt, dass Volumes bleiben', async () => {
    const fn = stubApi({
      'GET /api/v1/docker/status': active,
      'GET /api/v1/stacks': [{ name: 'media', running: 0, total: 0 }],
      'GET /api/v1/containers': [],
      'DELETE /api/v1/stacks/media': null,
    })
    const w = mount(StacksView, { global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    await w.find('[data-test="stack-media"] button[aria-label="Löschen"]').trigger('click')
    await flushPromises()
    expect(fn).not.toHaveBeenCalledWith('/api/v1/stacks/media', expect.objectContaining({ method: 'DELETE' }))
    expect(document.body.textContent).toContain('Volumes und Daten in Ordnern bleiben erhalten')
    ;(document.querySelector('[data-test="confirm-submit"]') as HTMLElement).click()
    await vi.waitFor(() => expect(fn).toHaveBeenCalledWith('/api/v1/stacks/media', expect.objectContaining({ method: 'DELETE' })))
    w.unmount()
  })
})

describe('ContainersView', () => {
  const c = { id: '0123456789ab', name: 'web', image: 'nginx', state: 'running', status: 'Up 2 hours', stack: 'media', ports: ['0.0.0.0:8080->80/tcp'], created: '' }

  it('listet Container und stoppt einen laufenden', async () => {
    const fn = stubApi({
      'GET /api/v1/docker/status': active,
      'GET /api/v1/stacks': [],
      'GET /api/v1/containers': [c, { ...c, id: 'ba9876543210', name: 'alone', state: 'exited', stack: '', ports: [] }],
      'POST /api/v1/containers/0123456789ab/stop': null,
    })
    const w = mount(ContainersView, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="container-web"]').text()).toContain('Stack media')
    expect(w.find('[data-test="container-web"]').text()).toContain('0.0.0.0:8080->80/tcp')
    expect(w.find('[data-test="container-alone"] button[aria-label="Starten"]').exists()).toBe(true)
    await w.find('[data-test="container-web"] button[aria-label="Stoppen"]').trigger('click')
    await vi.waitFor(() => expect(fn).toHaveBeenCalledWith('/api/v1/containers/0123456789ab/stop', expect.anything()))
  })

  it('zeigt den Healthcheck-Zustand', async () => {
    stubApi({
      'GET /api/v1/docker/status': active,
      'GET /api/v1/stacks': [],
      'GET /api/v1/containers': [{ ...c, health: 'unhealthy' }, { ...c, id: 'ba9876543210', name: 'plain' }],
    })
    const w = mount(ContainersView, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="container-web"] [data-test="health-unhealthy"]').text()).toContain('Fehlerhaft')
    expect(w.find('[data-test="container-plain"] [data-test^="health-"]').exists()).toBe(false)
  })

  it('meldet fehlendes Docker', async () => {
    stubApi({ 'GET /api/v1/docker/status': { installed: false, active: false, version: '' }, 'GET /api/v1/stacks': [] })
    const w = mount(ContainersView, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="docker-down"]').text()).toContain('nicht installiert')
  })
})
