import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import { createMemoryHistory, createRouter } from 'vue-router'
import VmConfigView from '@/views/vms/VmConfigView.vue'

const res = (body: unknown, status = 200) => Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))
const config = (state: string, over: object = {}) => ({
  name: 'ha', state, vcpus: 2, maxVcpus: 4, memoryMiB: 4096, maxMemoryMiB: 8192, autostart: false, firmware: 'uefi', secureBoot: false, tpm: false,
  guestAgent: false, network: 'default', hasCdrom: true, media: '', disks: ['/srv/vms/disks/ha.qcow2'], boot: 'hd', mac: '52:54:00:48:4f:ce', usb: [],
  cpuMode: 'host-passthrough', sockets: 0, threads: 0, hyperv: false, clock: 'utc', extraNics: [], diskOpts: [], ...over,
})
const usbHost = [{ vendor: '1a86', product: '7523', name: 'QinHeng USB Serial' }]

function route(cfg: unknown) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    if (key === 'GET /api/v1/vms/ha/config') return res(cfg)
    if (key === 'GET /api/v1/vms/usb') return res(usbHost)
    if (key === 'GET /api/v1/vms/ha/disks') return res([])
    if (key === 'GET /api/v1/vms/isos') return res(['a.iso'])
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

const post = (fn: ReturnType<typeof vi.fn>, url: string) => {
  const c = fn.mock.calls.find((x) => x[0] === url && x[1]?.method === 'POST')
  return c ? JSON.parse(c[1].body) : undefined
}
const input = (w: Awaited<ReturnType<typeof mountView>>, sel: string) => w.find(`${sel} input`)
const setNum = async (w: Awaited<ReturnType<typeof mountView>>, sel: string, v: string) => {
  const i = input(w, sel)
  await i.setValue(v)
  await i.trigger('blur')
  await flushPromises()
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('Hot-Plug im Betrieb', () => {
  it('schaltet bei laufender VM nur die Live-Felder frei und beschriftet den Knopf', async () => {
    route(config('running'))
    const w = await mountView()
    expect(w.find('[data-test="cfg-locked"]').text()).toContain('wirken sofort')
    expect(input(w, '[data-test="cfg-vcpus"]').attributes('disabled')).toBeUndefined()
    expect(input(w, '[data-test="cfg-ram"]').attributes('disabled')).toBeUndefined()
    expect(w.find('[data-test="cfg-live-ram"]').text()).toContain('8192')
    expect(w.find('[data-test="cfg-live-cpu"]').text()).toContain('4')
    expect(w.find('[data-test="cfg-secureboot"]').attributes('aria-disabled') ?? w.find('[data-test="cfg-secureboot"]').classes().join(' ')).toBeTruthy()
    expect(w.find('[data-test="cfg-headroom"]').exists()).toBe(false)
    expect(w.find('button[data-test="cfg-save"]').text()).toContain('Live übernehmen')
    w.unmount()
  })

  it('sendet nur geänderte Live-Werte an /live', async () => {
    const fn = route(config('running'))
    const w = await mountView()
    await setNum(w, '[data-test="cfg-ram"]', '6144')
    await setNum(w, '[data-test="cfg-vcpus"]', '3')
    await w.find('[data-test="cfg-usb-1a86:7523"] input').setValue(true)
    await flushPromises()
    await w.find('button[data-test="cfg-save"]').trigger('click')
    await flushPromises()
    expect(post(fn, '/api/v1/vms/ha/live')).toEqual({ vcpus: 3, memoryMiB: 6144, usb: [{ vendor: '1a86', product: '7523' }] })
    expect(post(fn, '/api/v1/vms/ha/config')).toBeUndefined()
    w.unmount()
  })

  it('begrenzt die Live-Werte auf die Maxima', async () => {
    route(config('running'))
    const w = await mountView()
    await setNum(w, '[data-test="cfg-ram"]', '9000')
    await flushPromises()
    // PrimeVue begrenzt auf das Maximum; der Wert darf 8192 nicht überschreiten.
    expect(Number((input(w, '[data-test="cfg-ram"]').element as HTMLInputElement).value.replace(/\D/g, ''))).toBeLessThanOrEqual(8192)
    w.unmount()
  })

  it('stellt das CD-Medium live um, nur wenn die VM ein Laufwerk hat', async () => {
    const fn = route(config('running'))
    let w = await mountView()
    expect(w.find('[data-test="cfg-media"]').classes().join(' ')).not.toContain('disabled')
    const select = w.findAllComponents({ name: 'Select' }).find((c) => c.attributes('data-test') === 'cfg-media')!
    select.vm.$emit('update:modelValue', 'a.iso')
    await flushPromises()
    await w.find('button[data-test="cfg-save"]').trigger('click')
    await flushPromises()
    expect(post(fn, '/api/v1/vms/ha/live')).toEqual({ media: 'a.iso' })
    w.unmount()
    document.body.innerHTML = ''
    route(config('running', { hasCdrom: false }))
    w = await mountView()
    expect(w.find('[data-test="cfg-media"]').classes().join(' ')).toContain('disabled')
    w.unmount()
  })
})

describe('Hot-Plug vorbereiten (ausgeschaltet)', () => {
  it('sendet ohne Spielraum keine Maxima', async () => {
    const fn = route(config('shutoff', { maxVcpus: 2, maxMemoryMiB: 4096 }))
    const w = await mountView()
    expect(w.find('[data-test="cfg-maxcpu"]').exists()).toBe(false)
    await setNum(w, '[data-test="cfg-ram"]', '8192')
    await w.find('button[data-test="cfg-save"]').trigger('click')
    await flushPromises()
    const b = post(fn, '/api/v1/vms/ha/config')
    expect(b.maxVcpus).toBeUndefined()
    expect(b.maxMemoryMiB).toBeUndefined()
    w.unmount()
  })

  it('zeigt vorhandenen Spielraum und sendet die Maxima mit', async () => {
    const fn = route(config('shutoff'))
    const w = await mountView()
    expect(w.find('[data-test="cfg-maxcpu"]').exists()).toBe(true)
    await setNum(w, '[data-test="cfg-maxram"]', '16384')
    await w.find('button[data-test="cfg-save"]').trigger('click')
    await flushPromises()
    const b = post(fn, '/api/v1/vms/ha/config')
    expect(b.maxVcpus).toBe(4)
    expect(b.maxMemoryMiB).toBe(16384)
    w.unmount()
  })

  it('legt über den Schalter einen Spielraum an', async () => {
    const fn = route(config('shutoff', { maxVcpus: 2, maxMemoryMiB: 4096 }))
    const w = await mountView()
    await w.find('[data-test="cfg-headroom"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="cfg-maxcpu"]').exists()).toBe(true)
    await w.find('button[data-test="cfg-save"]').trigger('click')
    await flushPromises()
    const b = post(fn, '/api/v1/vms/ha/config')
    expect(b.maxVcpus).toBe(4) // Vorschlag: doppelter Wert
    expect(b.maxMemoryMiB).toBe(8192)
    w.unmount()
  })

  it('warnt, wenn ein Maximum unter dem aktuellen Wert liegt', async () => {
    route(config('shutoff'))
    const w = await mountView()
    await setNum(w, '[data-test="cfg-ram"]', '16384')
    expect(w.find('[data-test="cfg-headroom-bad"]').exists()).toBe(true)
    expect(w.find('button[data-test="cfg-save"]').attributes('disabled')).toBeDefined()
    w.unmount()
  })
})
