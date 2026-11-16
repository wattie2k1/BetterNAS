import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import StackDialog from '@/components/docker/StackDialog.vue'
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

function stubApi(routes: Record<string, unknown>) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    const v = routes[key]
    if (v === undefined) return Promise.resolve(new Response(JSON.stringify({ error: { code: 'not_found', message: key } }), { status: 404 }))
    return Promise.resolve(new Response(JSON.stringify(v), { status: key.startsWith('POST') && url.match(/\/(up|stop|restart|update)$/) ? 202 : 200 }))
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

  it('meldet fehlendes Docker', async () => {
    stubApi({ 'GET /api/v1/docker/status': { installed: false, active: false, version: '' }, 'GET /api/v1/stacks': [] })
    const w = mount(ContainersView, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="docker-down"]').text()).toContain('nicht installiert')
  })
})
