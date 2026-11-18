import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import ResourcesView from '@/views/docker/ResourcesView.vue'
import StacksView from '@/views/docker/StacksView.vue'

vi.mock('@/components/docker/YamlEditor.vue', () => ({ __esModule: true, default: { render: () => null } }))

const plugins = () => [createPinia(), PrimeVue, ToastService]
afterEach(() => vi.unstubAllGlobals())

function stub(routes: Record<string, unknown>) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    const v = routes[key]
    if (v === undefined) return Promise.resolve(new Response(JSON.stringify({ error: { code: 'not_found', message: key } }), { status: 404 }))
    return Promise.resolve(new Response(v === null ? null : JSON.stringify(v), { status: v === null ? 204 : url.includes('updates/check') ? 202 : 200 }))
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
const active = { installed: true, active: true, version: '26' }

describe('Update-Prüfung in der Stack-Liste', () => {
  const base = {
    'GET /api/v1/docker/status': active,
    'GET /api/v1/stacks': [{ name: 'media', running: 1, total: 1 }, { name: 'fresh', running: 1, total: 1 }],
    'GET /api/v1/containers': [],
    'GET /api/v1/docker/updates': { checkedAt: '2026-11-16T10:00:00Z', stacks: { media: { outdated: ['nginx:stable'], errors: [] }, fresh: { outdated: [], errors: [] } } },
  }

  it('markiert nur Stacks mit neuer Image-Version', async () => {
    stub(base)
    const w = mount(StacksView, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="stack-media"] [data-test="update-tag"]').attributes('title')).toContain('nginx:stable')
    expect(w.find('[data-test="stack-fresh"] [data-test="update-tag"]').exists()).toBe(false)
    expect(w.find('[data-test="checked-at"]').exists()).toBe(true)
  })

  it('startet die Prüfung als Job', async () => {
    const fn = stub({ ...base, 'POST /api/v1/docker/updates/check': { jobId: 'j9' } })
    const w = mount(StacksView, { global: { plugins: plugins() } })
    await flushPromises()
    await w.find('[data-test="check-updates"]').trigger('click')
    await vi.waitFor(() => expect(fn).toHaveBeenCalledWith('/api/v1/docker/updates/check', expect.objectContaining({ method: 'POST' })))
  })
})

describe('ResourcesView', () => {
  const routes = {
    'GET /api/v1/docker/status': active,
    'GET /api/v1/stacks': [],
    'GET /api/v1/containers': [],
    'GET /api/v1/docker/images': [
      { id: 'aaaaaaaaaaaa', ref: 'ghcr.io/x/y:1', repository: 'ghcr.io/x/y', tag: '1', sizeBytes: 1000000, created: '2 weeks ago', dangling: false, unused: true },
      { id: 'bbbbbbbbbbbb', ref: 'bbbbbbbbbbbb', repository: '', tag: '', sizeBytes: 5, created: 'x', dangling: true, unused: true },
    ],
    'GET /api/v1/docker/networks': [
      { id: '111111111111', name: 'bridge', driver: 'bridge', scope: 'local', builtin: true, unused: false },
      { id: '222222222222', name: 'media_default', driver: 'bridge', scope: 'local', builtin: false, unused: true },
    ],
    'GET /api/v1/docker/volumes': [{ name: 'media_data', driver: 'local', unused: true }],
  }

  it('listet Images, Netzwerke und Volumes; eingebaute Netzwerke sind nicht entfernbar', async () => {
    stub(routes)
    const w = mount(ResourcesView, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="image-ghcr.io/x/y:1"]').exists()).toBe(true)
    expect(w.find('[data-test="image-bbbbbbbbbbbb"]').text()).toContain('ohne Namen')
    expect(w.text()).toContain('Images (2)')
    expect(w.text()).toContain('Netzwerke (2)')
    expect(w.text()).toContain('Volumes (1)')
  })

  it('entfernt ein Image erst nach Bestätigung und sendet die Referenz in der Query', async () => {
    const fn = stub({ ...routes, 'DELETE /api/v1/docker/images?ref=ghcr.io%2Fx%2Fy%3A1': null })
    const w = mount(ResourcesView, { global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    await w.find('[data-test="image-ghcr.io/x/y:1"] button[aria-label="Entfernen"]').trigger('click')
    await flushPromises()
    expect(fn).not.toHaveBeenCalledWith(expect.stringContaining('?ref='), expect.anything())
    ;(document.querySelector('[data-test="confirm-submit"]') as HTMLElement).click()
    await vi.waitFor(() => expect(fn).toHaveBeenCalledWith('/api/v1/docker/images?ref=ghcr.io%2Fx%2Fy%3A1', expect.objectContaining({ method: 'DELETE' })))
    w.unmount()
  })

  it('sperrt Löschen bei Einträgen in Benutzung und löscht mehrere ausgewählte', async () => {
    const r = {
      ...routes,
      'GET /api/v1/docker/images': [
        { id: 'aaaaaaaaaaaa', ref: 'a:1', repository: 'a', tag: '1', sizeBytes: 1, created: 'x', dangling: false, unused: false },
        { id: 'bbbbbbbbbbbb', ref: 'b:1', repository: 'b', tag: '1', sizeBytes: 1, created: 'x', dangling: false, unused: true },
        { id: 'cccccccccccc', ref: 'c:1', repository: 'c', tag: '1', sizeBytes: 1, created: 'x', dangling: false, unused: true },
      ],
      'DELETE /api/v1/docker/images?ref=b%3A1': null,
      'DELETE /api/v1/docker/images?ref=c%3A1': null,
    }
    const fn = stub(r)
    const w = mount(ResourcesView, { global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    expect(w.find('[data-test="image-a:1"] button[aria-label="Entfernen"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="image-a:1"] input[type="checkbox"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="remove-selected"]').exists()).toBe(false)
    await w.find('[data-test="select-all"]').setValue(true)
    expect(w.find('[data-test="remove-selected"]').text()).toContain('(2)')
    await w.find('[data-test="remove-selected"]').trigger('click')
    await flushPromises()
    ;(document.querySelector('[data-test="confirm-submit"]') as HTMLElement).click()
    await vi.waitFor(() => {
      expect(fn).toHaveBeenCalledWith('/api/v1/docker/images?ref=b%3A1', expect.objectContaining({ method: 'DELETE' }))
      expect(fn).toHaveBeenCalledWith('/api/v1/docker/images?ref=c%3A1', expect.objectContaining({ method: 'DELETE' }))
    })
    expect(fn).not.toHaveBeenCalledWith('/api/v1/docker/images?ref=a%3A1', expect.anything())
    w.unmount()
  })

  it('warnt beim Aufräumen von Volumes vor Datenverlust', async () => {
    const fn = stub({ ...routes, 'POST /api/v1/docker/volumes/prune': { reclaimed: '2GB' } })
    const w = mount(ResourcesView, { global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    // Reiter wechseln: Volumes
    const tabs = w.findAll('[role="tab"]')
    await tabs[2].trigger('click')
    await flushPromises()
    await w.find('[data-test="prune-volumes"]').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('samt ihren Daten gelöscht')
    expect(fn).not.toHaveBeenCalledWith('/api/v1/docker/volumes/prune', expect.anything())
    ;(document.querySelector('[data-test="confirm-submit"]') as HTMLElement).click()
    await vi.waitFor(() => expect(fn).toHaveBeenCalledWith('/api/v1/docker/volumes/prune', expect.objectContaining({ body: JSON.stringify({ all: true }) })))
    w.unmount()
  })
})
