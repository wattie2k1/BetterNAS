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
  name: 'ha', state: 'shutoff', vcpus: 4, memoryMiB: 4096, autostart: false, firmware: 'uefi', secureBoot: false, tpm: false, guestAgent: false,
  network: 'default', hasCdrom: false, media: '', disks: ['/srv/vms/disks/ha.qcow2'], boot: 'hd', mac: '52:54:00:48:4f:ce', usb: [],
  cpuMode: 'host-passthrough', sockets: 0, threads: 0, hyperv: false, clock: 'utc',
  extraNics: [{ network: 'default', model: 'virtio', mac: '52:54:00:00:00:01' }],
  diskOpts: [{ path: '/srv/vms/disks/ha.qcow2', cache: 'default', discard: 'ignore' }], ...over,
})

function route(cfg: unknown) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    if (key === 'GET /api/v1/vms/ha/config') return res(cfg)
    if (key === 'GET /api/v1/vms/usb') return res([])
    if (key === 'GET /api/v1/vms/ha/disks') return res([])
    if (key === 'GET /api/v1/vms/isos') return res([])
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

const posted = (fn: ReturnType<typeof vi.fn>) => {
  const c = fn.mock.calls.find((x) => x[0] === '/api/v1/vms/ha/config' && x[1]?.method === 'POST')
  return c ? JSON.parse(c[1].body) : undefined
}
const saveBtn = (w: Awaited<ReturnType<typeof mountView>>) => w.find('button[data-test="cfg-save"]')

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('VM-Einstellungen: Erweitert', () => {
  it('ist unverändert nicht „dirty“ und sendet keine erweiterten Felder', async () => {
    const fn = route(config())
    const w = await mountView()
    expect(saveBtn(w).attributes('disabled')).toBeDefined()
    await w.find('[data-test="cfg-ram"] input').setValue('8192')
    await w.find('[data-test="cfg-ram"] input').trigger('blur')
    await saveBtn(w).trigger('click')
    await flushPromises()
    const body = posted(fn)
    for (const k of ['cpuMode', 'topology', 'hyperv', 'clock', 'extraNics', 'diskOpts']) expect(body[k]).toBeUndefined()
    w.unmount()
  })

  it('sendet Hyper-V, CPU-Typ, Uhr und Festplattenoptionen', async () => {
    const fn = route(config())
    const w = await mountView()
    const selects = w.findAllComponents(Select)
    const by = (t: string) => selects.find((c) => c.attributes('data-test') === t)!
    await w.find('[data-test="adv-hyperv"]').trigger('click')
    by('adv-cpu').vm.$emit('update:modelValue', 'host-model')
    by('adv-clock').vm.$emit('update:modelValue', 'localtime')
    by('adv-cache').vm.$emit('update:modelValue', 'none')
    await w.find('[data-test="adv-discard"]').trigger('click')
    await flushPromises()
    await saveBtn(w).trigger('click')
    await flushPromises()
    const b = posted(fn)
    expect(b.hyperv).toBe(true)
    expect(b.cpuMode).toBe('host-model')
    expect(b.clock).toBe('localtime')
    expect(b.diskOpts).toEqual([{ path: '/srv/vms/disks/ha.qcow2', cache: 'none', discard: 'unmap' }])
    w.unmount()
  })

  it('prüft die Topologie gegen die vCPUs und zeigt die Kerne', async () => {
    const fn = route(config())
    const w = await mountView()
    const threads = w.find('[data-test="adv-threads"] input')
    await threads.setValue('3')
    await threads.trigger('blur')
    await flushPromises()
    expect(w.find('[data-test="adv-cores"]').text()).toBe('?')
    expect(saveBtn(w).attributes('disabled')).toBeDefined()
    await threads.setValue('2')
    await threads.trigger('blur')
    await flushPromises()
    expect(w.find('[data-test="adv-cores"]').text()).toBe('2')
    await saveBtn(w).trigger('click')
    await flushPromises()
    expect(posted(fn).topology).toEqual({ sockets: 1, threads: 2 })
    w.unmount()
  })

  it('verwaltet zusätzliche Netzwerkkarten', async () => {
    const fn = route(config())
    const w = await mountView()
    expect(w.findAll('[data-test="adv-nic"]')).toHaveLength(1)
    expect(w.find('[data-test="adv-nic-mac"]').element).toHaveProperty('value', '52:54:00:00:00:01')
    await w.find('[data-test="adv-nic-add"]').trigger('click')
    expect(w.findAll('[data-test="adv-nic"]')).toHaveLength(2)
    await w.findAll('[data-test="adv-nic-remove"]')[0].trigger('click')
    await flushPromises()
    await saveBtn(w).trigger('click')
    await flushPromises()
    expect(posted(fn).extraNics).toEqual([{ network: 'default', model: 'virtio', mac: '' }])
    w.unmount()
  })

  it('sperrt Speichern bei ungültiger MAC einer Zusatzkarte', async () => {
    route(config())
    const w = await mountView()
    await w.find('[data-test="adv-nic-mac"]').setValue('53:00:00:00:00:01')
    await flushPromises()
    expect(saveBtn(w).attributes('disabled')).toBeDefined()
    w.unmount()
  })

  it('sperrt alles bei laufender VM', async () => {
    route(config({ state: 'running' }))
    const w = await mountView()
    expect(w.find('[data-test="adv-nic-add"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="adv-hyperv"]').attributes('disabled') ?? w.find('[data-test="adv-hyperv"]').classes().join(' ')).toBeTruthy()
    w.unmount()
  })
})
