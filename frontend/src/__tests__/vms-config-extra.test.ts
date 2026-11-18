import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import Select from 'primevue/select'
import ToastService from 'primevue/toastservice'
import { createMemoryHistory, createRouter } from 'vue-router'
import VmConfigView from '@/views/vms/VmConfigView.vue'

const res = (body: unknown, status = 200) => Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))
const config = (over: object = {}) => ({
  name: 'ha', state: 'shutoff', vcpus: 2, memoryMiB: 4096, autostart: false, firmware: 'uefi', secureBoot: false, tpm: false, guestAgent: false,
  network: 'default', hasCdrom: false, media: '', disks: ['/srv/vms/disks/ha.qcow2'], boot: 'cdrom', mac: '52:54:00:48:4f:ce', usb: [], ...over,
})
const usbHost = [
  { vendor: '1a86', product: '7523', name: 'QinHeng USB Serial' },
  { vendor: '174c', product: '55aa', name: 'ASM Platte', storage: true },
]

function route(cfg: unknown) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    if (key === 'GET /api/v1/vms/ha/config') return res(cfg)
    if (key === 'GET /api/v1/vms/usb') return res(usbHost)
    if (key === 'GET /api/v1/vms/ha/disks') return res([])
    if (key === 'GET /api/v1/vms/isos') return res([])
    if (key === 'GET /api/v1/vms/networks') return res(['default'])
    if (key === 'GET /api/v1/vms/interfaces') return res([])
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

const posted = (fn: ReturnType<typeof vi.fn>) => {
  const c = fn.mock.calls.find((x) => x[0] === '/api/v1/vms/ha/config' && x[1]?.method === 'POST')
  return c ? JSON.parse(c[1].body) : undefined
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('VM-Einstellungen: MAC und USB', () => {
  it('zeigt MAC und USB-Geräte, speichert ohne Änderung nichts davon mit', async () => {
    const fn = route(config())
    const w = await mountView()
    expect((w.find('[data-test="cfg-mac"]').element as HTMLInputElement).value).toBe('52:54:00:48:4f:ce')
    expect(w.find('[data-test="cfg-usb"]').text()).toContain('QinHeng USB Serial')
    await w.find('[data-test="cfg-ram"] input').setValue('8192')
    await w.find('[data-test="cfg-ram"] input').trigger('blur')
    await w.find('button[data-test="cfg-save"]').trigger('click')
    await flushPromises()
    const body = posted(fn)
    expect(body.mac).toBeUndefined()
    expect(body.usb).toBeUndefined()
    expect(body.bootFirst).toBeUndefined()
    w.unmount()
  })

  it('speichert geänderte MAC und gewählte USB-Geräte', async () => {
    const fn = route(config())
    const w = await mountView()
    await w.find('[data-test="cfg-mac"]').setValue('52:54:00:AA:BB:CC')
    await w.find('[data-test="cfg-usb-1a86:7523"] input').setValue(true)
    await flushPromises()
    await w.find('button[data-test="cfg-save"]').trigger('click')
    await flushPromises()
    const body = posted(fn)
    expect(body.mac).toBe('52:54:00:aa:bb:cc')
    expect(body.usb).toEqual([{ vendor: '1a86', product: '7523' }])
    w.unmount()
  })

  it('sperrt Speichern bei ungültiger MAC und warnt bei Massenspeicher', async () => {
    route(config())
    const w = await mountView()
    await w.find('[data-test="cfg-mac"]').setValue('53:54:00:aa:bb:cc')
    await flushPromises()
    expect(w.find('button[data-test="cfg-save"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="cfg-usb-storage"]').exists()).toBe(false)
    await w.find('[data-test="cfg-usb-174c:55aa"] input').setValue(true)
    await flushPromises()
    expect(w.find('[data-test="cfg-usb-storage"]').exists()).toBe(true)
    w.unmount()
  })

  it('sperrt die Boot-Auswahl bei Reihenfolge pro Gerät', async () => {
    route(config({ boot: 'custom' }))
    const w = await mountView()
    expect(w.find('[data-test="cfg-boot"]').attributes('aria-disabled') ?? w.find('[data-test="cfg-boot"]').classes().join(' ')).toContain('disabled')
    w.unmount()
  })

  it('übermittelt die geänderte Boot-Reihenfolge', async () => {
    const fn = route(config())
    const w = await mountView()
    const select = w.findAllComponents(Select).find((c) => c.attributes('data-test') === 'cfg-boot')!
    select.vm.$emit('update:modelValue', 'hd')
    await flushPromises()
    await w.find('button[data-test="cfg-save"]').trigger('click')
    await flushPromises()
    expect(posted(fn).bootFirst).toBe('hd')
    w.unmount()
  })
})
