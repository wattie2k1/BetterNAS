import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import Select from 'primevue/select'
import ToastService from 'primevue/toastservice'
import { createMemoryHistory, createRouter } from 'vue-router'
import VmConfigView from '@/views/vms/VmConfigView.vue'

const res = (body: unknown, status = 200) => Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))
const config = (state: string, sharedDirs: object[] = []) => ({
  name: 'ha', state, vcpus: 2, maxVcpus: 2, memoryMiB: 4096, maxMemoryMiB: 4096, autostart: false, firmware: 'uefi', secureBoot: false, tpm: false,
  guestAgent: false, network: 'default', hasCdrom: false, media: '', disks: ['/srv/vms/disks/ha.qcow2'], boot: 'hd', mac: '52:54:00:48:4f:ce', usb: [],
  cpuMode: 'host-passthrough', sockets: 0, threads: 0, hyperv: false, clock: 'utc', extraNics: [], diskOpts: [], sharedDirs,
})
const shares = [
  { name: 'Medien', pool: 'test', subdir: 'Medien', path: '/srv/pools/test/Medien' },
  { name: 'Backup', pool: 'test', subdir: 'Backup', path: '/srv/pools/test/Backup' },
  { name: 'Alles', pool: 'test', subdir: '', path: '/srv/pools/test/' },
]

function route(cfg: unknown) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    if (key === 'GET /api/v1/vms/ha/config') return res(cfg)
    if (key === 'GET /api/v1/shares') return res(shares)
    if (key === 'GET /api/v1/vms/usb') return res([])
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
const save = (w: Awaited<ReturnType<typeof mountView>>) => w.find('button[data-test="cfg-save"]')

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('Geteilte Ordner (virtiofs)', () => {
  it('sendet ohne Änderung nichts davon', async () => {
    const fn = route(config('shutoff', [{ path: '/srv/pools/test/Medien', tag: 'medien', readOnly: false }]))
    const w = await mountView()
    expect(w.findAll('[data-test="shared-row"]')).toHaveLength(1)
    await w.find('[data-test="cfg-ram"] input').setValue('8192')
    await w.find('[data-test="cfg-ram"] input').trigger('blur')
    await save(w).trigger('click')
    await flushPromises()
    expect(posted(fn).sharedDirs).toBeUndefined()
    w.unmount()
  })

  it('fügt einen Ordner mit Vorschlag für den Namen hinzu und sendet ihn', async () => {
    const fn = route(config('shutoff'))
    const w = await mountView()
    expect(w.find('[data-test="shared-warn"]').exists()).toBe(false)
    await w.find('[data-test="shared-add"]').trigger('click')
    await flushPromises()
    expect((w.find('[data-test="shared-tag"]').element as HTMLInputElement).value).toBe('Medien')
    expect(w.find('[data-test="shared-warn"]').exists()).toBe(true)
    await w.find('[data-test="shared-ro"]').trigger('click')
    await flushPromises()
    await save(w).trigger('click')
    await flushPromises()
    expect(posted(fn).sharedDirs).toEqual([{ path: '/srv/pools/test/Medien', tag: 'Medien', readOnly: true }])
    w.unmount()
  })

  it('übernimmt beim Wechsel der Freigabe den neuen Vorschlag und normalisiert den Pool-Wurzelpfad', async () => {
    const fn = route(config('shutoff'))
    const w = await mountView()
    await w.find('[data-test="shared-add"]').trigger('click')
    await flushPromises()
    const select = w.findAllComponents(Select).find((c) => c.attributes('data-test') === 'shared-path')!
    select.vm.$emit('update:modelValue', '/srv/pools/test') // Wurzel der Freigabe „Alles“ (ohne Schrägstrich am Ende)
    await flushPromises()
    expect((w.find('[data-test="shared-tag"]').element as HTMLInputElement).value).toBe('Alles')
    await save(w).trigger('click')
    await flushPromises()
    expect(posted(fn).sharedDirs[0].path).toBe('/srv/pools/test')
    w.unmount()
  })

  it('sperrt Speichern bei ungültigem oder doppeltem Namen', async () => {
    route(config('shutoff', [{ path: '/srv/pools/test/Medien', tag: 'a', readOnly: false }, { path: '/srv/pools/test/Backup', tag: 'b', readOnly: false }]))
    const w = await mountView()
    const tags = w.findAll('[data-test="shared-tag"]')
    await tags[1].setValue('a')
    await flushPromises()
    expect(w.find('[data-test="shared-bad"]').exists()).toBe(true)
    expect(save(w).attributes('disabled')).toBeDefined()
    await tags[1].setValue('schlecht name')
    await flushPromises()
    expect(save(w).attributes('disabled')).toBeDefined()
    await tags[1].setValue('b2')
    await flushPromises()
    expect(w.find('[data-test="shared-bad"]').exists()).toBe(false)
    expect(save(w).attributes('disabled')).toBeUndefined()
    w.unmount()
  })

  it('entfernt Ordner und sperrt alles bei laufender VM', async () => {
    const fn = route(config('shutoff', [{ path: '/srv/pools/test/Medien', tag: 'medien', readOnly: false }]))
    let w = await mountView()
    await w.find('[data-test="shared-remove"]').trigger('click')
    await flushPromises()
    await save(w).trigger('click')
    await flushPromises()
    expect(posted(fn).sharedDirs).toEqual([])
    w.unmount()
    document.body.innerHTML = ''
    route(config('running', [{ path: '/srv/pools/test/Medien', tag: 'medien', readOnly: false }]))
    w = await mountView()
    expect(w.find('[data-test="shared-add"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="shared-remove"]').attributes('disabled')).toBeDefined()
    w.unmount()
  })
})
