import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import DockerNotConfigured from '@/components/docker/DockerNotConfigured.vue'
import DockerPathsDialog from '@/components/docker/DockerPathsDialog.vue'
import StacksView from '@/views/docker/StacksView.vue'
import ContainersView from '@/views/docker/ContainersView.vue'
import DockerSettings from '@/views/settings/DockerSettings.vue'
import { useDockerStore } from '@/stores/docker'
import { servicesApi } from '@/api/system'

vi.mock('@/api/system', () => ({ servicesApi: { docker: vi.fn(), setDocker: vi.fn(), act: vi.fn() } }))
vi.mock('@/components/docker/YamlEditor.vue', async () => {
  const { defineComponent, h } = await import('vue')
  return { __esModule: true, default: defineComponent({ props: ['modelValue'], setup: () => () => h('div') }) }
})

const plugins = () => [createPinia(), PrimeVue, ToastService]
afterEach(() => vi.unstubAllGlobals())

const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))
function stub(map: Record<string, unknown>) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const v = map[`${init?.method ?? 'GET'} ${url.split('?')[0]}`]
    return v === undefined ? json({ error: { code: 'not_found', message: 'x' } }, 404) : json(v)
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
const unconfigured = { installed: true, active: false, configured: false, version: '' }

describe('DockerNotConfigured', () => {
  it('erklärt die Sperre und bietet die Einrichtung an', () => {
    const w = mount(DockerNotConfigured, { global: { plugins: plugins() } })
    expect(w.text()).toContain('Docker ist noch nicht eingerichtet')
    expect(w.text()).toContain('Bereite zuerst unter Speicher einen Pool oder eine Platte vor')
    expect(w.find('[data-test="docker-setup"]').text()).toContain('Speicherorte festlegen')
  })
})

describe('Docker-Ansichten ohne Einrichtung', () => {
  it('Stacks und Container zeigen die Sperransicht statt der Inhalte', async () => {
    const fn = stub({ 'GET /api/v1/docker/status': unconfigured, 'GET /api/v1/docker/paths': { configured: false, appdata: '', dockerdata: '', warnings: [], staleStacks: 0 } })
    for (const View of [StacksView, ContainersView]) {
      const w = mount(View, { global: { plugins: plugins() } })
      await flushPromises()
      expect(w.find('[data-test="docker-not-configured"]').exists(), View.__name).toBe(true)
      expect(w.text()).not.toContain('Neuer Stack')
    }
    expect(fn.mock.calls.map((c) => c[0]).some((u: string) => u.startsWith('/api/v1/stacks') || u.startsWith('/api/v1/containers'))).toBe(false)
  })
})

describe('DockerPathsDialog', () => {
  const paths = { configured: true, appdata: '/srv/pools/a/cfg', dockerdata: '/srv/pools/b/dd', warnings: ['Der Speicherort liegt auf der Systemplatte.'], staleStacks: 2 }

  it('sperrt Speichern ohne beide Pfade und speichert beide', async () => {
    setActivePinia(createPinia())
    const fn = stub({
      'PUT /api/v1/docker/paths': paths,
      'GET /api/v1/docker/status': { installed: true, active: true, configured: true, version: '26' },
      'GET /api/v1/stacks': [],
      'GET /api/v1/containers': [],
    })
    const w = mount(DockerPathsDialog, { props: { visible: true }, global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    const save = () => document.querySelector<HTMLButtonElement>('[data-test="paths-save"]')!
    expect(save().disabled).toBe(true)
    const set = async (key: string, v: string) => {
      const el = document.querySelector<HTMLInputElement>(`[data-test="paths-${key}"]`)!
      el.value = v
      el.dispatchEvent(new Event('input'))
      await flushPromises()
    }
    await set('appdata', '/srv/pools/a/cfg')
    expect(save().disabled).toBe(true)
    await set('dockerdata', '/srv/pools/b/dd')
    expect(save().disabled).toBe(false)
    save().click()
    await flushPromises()
    const put = fn.mock.calls.find((c) => c[1]?.method === 'PUT')!
    expect(JSON.parse(put[1].body)).toEqual({ appdata: '/srv/pools/a/cfg', dockerdata: '/srv/pools/b/dd' })
    expect(document.body.textContent).toContain('liegt auf der Systemplatte')
    expect(document.body.textContent).toContain('2 Stacks')
    expect(useDockerStore().configured).toBe(true)
    w.unmount()
  })

  it('zeigt Fehler der API an', async () => {
    setActivePinia(createPinia())
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => json({ error: { code: 'invalid_params', message: 'Der Speicherort liegt auf einem mergerfs-Pool.' } }, 400)))
    const w = mount(DockerPathsDialog, { props: { visible: true, initial: { appdata: '/srv/pools/a/cfg', dockerdata: '/srv/pools/b/dd' } }, global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    document.querySelector<HTMLButtonElement>('[data-test="paths-save"]')!.click()
    await flushPromises()
    expect(document.querySelector('[data-test="paths-error"]')!.textContent).toContain('mergerfs')
    w.unmount()
  })
})

describe('Einstellungen → Docker: Speicherorte', () => {
  const daemon = { dataRoot: '/srv/pools/b/dd/docker', logMaxSize: '10m', logMaxFile: 3, registryMirrors: [], liveRestore: false }

  it('öffnet bei nicht eingerichtetem Docker die Einrichtung', async () => {
    vi.mocked(servicesApi.docker).mockResolvedValue(daemon)
    stub({ 'GET /api/v1/docker/status': unconfigured, 'GET /api/v1/docker/paths': { configured: false, appdata: '', dockerdata: '', warnings: [], staleStacks: 0 } })
    const w = mount(DockerSettings, { global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    expect(w.find('[data-test="paths-missing"]').exists()).toBe(true)
    expect(document.querySelector('[data-test="paths-save"]')).not.toBeNull()
    w.unmount()
  })

  it('zeigt eingerichtete Speicherorte samt Warnung und öffnet den Dialog nicht von selbst', async () => {
    vi.mocked(servicesApi.docker).mockResolvedValue(daemon)
    stub({
      'GET /api/v1/docker/status': { installed: true, active: true, configured: true, version: '26' },
      'GET /api/v1/docker/paths': { configured: true, appdata: '/srv/pools/a/cfg', dockerdata: '/srv/pools/b/dd', warnings: ['Der Speicherort liegt auf der Systemplatte.'], staleStacks: 0 },
      'GET /api/v1/stacks': [],
      'GET /api/v1/containers': [],
    })
    const w = mount(DockerSettings, { global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    expect(w.find('[data-test="paths-show-appdata"]').text()).toBe('/srv/pools/a/cfg')
    expect(w.find('[data-test="paths-show-dockerdata"]').text()).toBe('/srv/pools/b/dd')
    expect(w.text()).toContain('liegt auf der Systemplatte')
    expect(document.querySelector('[data-test="paths-save"]')).toBeNull()
    w.unmount()
  })
})
