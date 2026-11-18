import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import StackDialog from '@/components/docker/StackDialog.vue'
import DockerTile from '@/components/docker/DockerTile.vue'
import { useDockerStore } from '@/stores/docker'
import type { ContainerInfo, StackFiles } from '@/api/docker'

vi.mock('@/components/docker/YamlEditor.vue', async () => {
  const { defineComponent, h } = await import('vue')
  return { __esModule: true, default: defineComponent({ props: ['modelValue'], setup: (p) => () => h('textarea', { value: p.modelValue }) }) }
})

const container = (over: Partial<ContainerInfo> = {}): ContainerInfo => ({
  id: 'a', name: 'web', image: 'nginx', state: 'running', status: 'Up', stack: 'media', ports: [], created: '', ...over,
})

const json = (body: unknown, status = 200) => Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))

function stubFetch(routes: Record<string, unknown> = {}) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    if (key in routes) return json(routes[key], routes[key] === null ? 204 : 200)
    return json({ error: { code: 'not_found', message: 'x' } }, 404)
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: () => 'blob:test', revokeObjectURL: () => {} }))
})
afterEach(() => vi.unstubAllGlobals())

describe('Docker-Store: Icons', () => {
  it('leitet die Icon-Adresse eines Containers vom Stack ab', () => {
    const s = useDockerStore()
    s.stacks = [{ name: 'media', running: 1, total: 1, icon: 'abc123' }, { name: 'plain', running: 0, total: 0 }]
    expect(s.iconUrlFor(container())).toBe('/api/v1/stacks/media/icon?v=abc123')
    expect(s.iconUrlFor(container({ stack: 'plain' }))).toBeNull()
    expect(s.iconUrlFor(container({ stack: '' }))).toBeNull()
  })

  it('speichert erst den Stack und setzt dann das Icon; das Icon geht nicht in die compose-Anfrage', async () => {
    const fn = stubFetch({ 'POST /api/v1/stacks': { name: 'media' }, 'PUT /api/v1/stacks/media/icon': null, 'GET /api/v1/docker/status': { installed: true, active: false, version: '' }, 'GET /api/v1/stacks': [] })
    const file = new File([new Uint8Array([1, 2, 3])], 'i.png', { type: 'image/png' })
    await useDockerStore().create({ name: 'media', compose: 'services: {}', env: '', icon: { kind: 'file', file } })
    const calls = fn.mock.calls.map(([u, i]) => `${i?.method ?? 'GET'} ${u}`)
    expect(calls.indexOf('POST /api/v1/stacks')).toBeLessThan(calls.indexOf('PUT /api/v1/stacks/media/icon'))
    const post = fn.mock.calls.find(([, i]) => i?.method === 'POST')![1] as RequestInit
    expect(JSON.parse(post.body as string)).toEqual({ name: 'media', compose: 'services: {}', env: '' })
    const put = fn.mock.calls.find(([u]) => String(u).endsWith('/icon'))![1] as RequestInit
    expect(new Headers(put.headers).get('Content-Type')).toBe('image/png')
  })

  it('meldet, dass der Stack gespeichert wurde, wenn nur das Icon scheitert', async () => {
    stubFetch({ 'PUT /api/v1/stacks/media': null, 'GET /api/v1/docker/status': { installed: true, active: false, version: '' }, 'GET /api/v1/stacks': [] })
    await expect(useDockerStore().update({ name: 'media', compose: 'x', env: '', icon: { kind: 'url', url: 'http://x/i.png' } })).rejects.toThrow(/Stack wurde gespeichert/)
  })
})

describe('StackDialog: Icon-Tab', () => {
  const mountDialog = (props = {}) =>
    mount(StackDialog, { props: { visible: true, stack: null, existing: [], ...props }, global: { plugins: [createPinia(), PrimeVue, ToastService] } })

  it('übernimmt eine Adresse und sendet sie mit dem Speichern', async () => {
    const w = mountDialog()
    await flushPromises()
    await w.find('input#stack-name').setValue('fotos')
    await w.find('input#stack-icon-url').setValue('https://example.org/icon.png')
    await w.find('button[data-test="stack-icon-url-apply"]').trigger('click')
    expect(w.find('img[data-test="stack-icon-preview"]').attributes('src')).toBe('https://example.org/icon.png')
    await w.find('button[data-test="stack-save"]').trigger('click')
    const files = w.emitted('submit')![0][0] as StackFiles
    expect(files.icon).toEqual({ kind: 'url', url: 'https://example.org/icon.png' })
  })

  it('lehnt Adressen ohne http(s) und Nicht-Bilder ab', async () => {
    const w = mountDialog()
    await flushPromises()
    await w.find('input#stack-icon-url').setValue('javascript:alert(1)')
    await w.find('button[data-test="stack-icon-url-apply"]').trigger('click')
    expect(w.find('[data-test="stack-icon-error"]').text()).toContain('http://')
    const input = w.find('input[data-test="stack-icon-file"]')
    Object.defineProperty(input.element, 'files', { value: [new File(['x'], 'a.txt', { type: 'text/plain' })], configurable: true })
    await input.trigger('change')
    expect(w.find('[data-test="stack-icon-error"]').text()).toContain('Bilddatei')
  })

  it('wählt eine Datei und zeigt die Vorschau; Entfernen merkt die Löschung vor', async () => {
    const w = mountDialog()
    await flushPromises()
    const input = w.find('input[data-test="stack-icon-file"]')
    const file = new File([new Uint8Array(10)], 'i.png', { type: 'image/png' })
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    await input.trigger('change')
    expect(w.find('img[data-test="stack-icon-preview"]').attributes('src')).toBe('blob:test')
    await w.find('button[data-test="stack-icon-remove"]').trigger('click')
    expect(w.find('img[data-test="stack-icon-preview"]').exists()).toBe(false)
    await w.find('input#stack-name').setValue('fotos')
    await w.find('button[data-test="stack-save"]').trigger('click')
    expect((w.emitted('submit')![0][0] as StackFiles).icon).toEqual({ kind: 'remove' })
  })
})

describe('Docker-Kachel: Icon', () => {
  it('zeigt das Stack-Icon statt des Anfangsbuchstabens', async () => {
    const s = useDockerStore()
    s.status = { installed: true, active: true, version: '27' }
    s.stacks = [{ name: 'media', running: 1, total: 1, icon: 'v1' }]
    s.containers = [container(), container({ id: 'b', name: 'solo', stack: '' })]
    stubFetch({ 'GET /api/v1/docker/status': s.status, 'GET /api/v1/stacks': s.stacks, 'GET /api/v1/containers': s.containers })
    const w = mount(DockerTile, { global: { plugins: [PrimeVue, ToastService] } })
    await flushPromises()
    expect(w.find('[data-test="dock-icon-web"] img').attributes('src')).toBe('/api/v1/stacks/media/icon?v=v1')
    expect(w.find('[data-test="dock-icon-solo"]').text()).toBe('S')
  })
})
