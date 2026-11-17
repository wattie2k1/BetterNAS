import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import { createMemoryHistory, createRouter } from 'vue-router'
import VmConfigView from '@/views/vms/VmConfigView.vue'

const res = (body: unknown, status = 200) => Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))
const config = (state = 'shutoff', over: object = {}) => ({
  name: 'ha', state, vcpus: 2, memoryMiB: 4096, autostart: false, firmware: 'uefi', secureBoot: true, tpm: false,
  network: 'default', hasCdrom: false, media: '', disks: ['/srv/vms/disks/ha.qcow2'], ...over,
})

function route(cfg: unknown) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    if (key === 'GET /api/v1/vms/ha/config') return res(cfg)
    if (key === 'GET /api/v1/vms/isos') return res(['a.iso', 'haos.qcow2'])
    if (key === 'GET /api/v1/vms/networks') return res(['default'])
    if (key === 'GET /api/v1/vms/interfaces') return res(['eno1'])
    if (key === 'GET /api/v1/vms/status') return res({ installed: true, active: true, kvm: true })
    if (key === 'GET /api/v1/vms') return res([])
    if (init?.method === 'POST') return res(null, 204)
    return res({ error: { code: 'not_found', message: 'x' } }, 404)
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

async function mountView() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/vms/:name', component: VmConfigView }, { path: '/vms', component: { template: '<div/>' } }] })
  await router.push('/vms/ha')
  await router.isReady()
  const w = mount(VmConfigView, { global: { plugins: [createPinia(), PrimeVue, ToastService, router] }, attachTo: document.body })
  await flushPromises()
  return w
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('VmConfigView', () => {
  it('sperrt die Felder, solange die VM läuft', async () => {
    route(config('running'))
    const w = await mountView()
    expect(w.find('[data-test="cfg-locked"]').exists()).toBe(true)
    expect(w.find('button[data-test="cfg-save"]').attributes('disabled')).toBeDefined()
    w.unmount()
  })

  it('speichert geänderte Werte mit Secure-Boot-Warnung', async () => {
    const fn = route(config())
    const w = await mountView()
    expect(w.find('[data-test="cfg-locked"]').exists()).toBe(false)
    expect(w.find('button[data-test="cfg-save"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="cfg-sb-warn"]').exists()).toBe(false)
    await w.find('[data-test="cfg-secureboot"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="cfg-sb-warn"]').exists()).toBe(true)
    await w.find('button[data-test="cfg-save"]').trigger('click')
    await flushPromises()
    const post = fn.mock.calls.find((c) => c[1]?.method === 'POST' && c[0] === '/api/v1/vms/ha/config')
    expect(post).toBeTruthy()
    expect(JSON.parse(post![1].body)).toEqual({ vcpus: 2, memoryMiB: 4096, secureBoot: false, network: 'default', media: '' })
    w.unmount()
  })

  it('blendet Secure Boot bei BIOS-VMs aus', async () => {
    route(config('shutoff', { firmware: 'bios', secureBoot: false }))
    const w = await mountView()
    expect(w.find('[data-test="cfg-secureboot"]').exists()).toBe(false)
    w.unmount()
  })

  it('schaltet Autostart sofort', async () => {
    const fn = route(config('running'))
    const w = await mountView()
    await w.find('[data-test="cfg-autostart"]').trigger('click')
    await flushPromises()
    expect(fn.mock.calls.map((c) => `${c[1]?.method ?? 'GET'} ${c[0]}`)).toContain('POST /api/v1/vms/ha/autostart')
    w.unmount()
  })
})
