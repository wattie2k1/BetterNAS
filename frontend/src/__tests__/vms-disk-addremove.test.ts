import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import { createMemoryHistory, createRouter } from 'vue-router'
import VmConfigView from '@/views/vms/VmConfigView.vue'

const res = (body: unknown, status = 200) => Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))
const GiB = 1024 ** 3
const sys = { path: '/srv/vms/disks/ha.qcow2', sizeGiB: 32, usedBytes: 2 * GiB, virtualSize: 32 * GiB }
const data = { path: '/srv/vms/disks/ha-2.qcow2', sizeGiB: 100, usedBytes: GiB, virtualSize: 100 * GiB }
const config = (state: string) => ({
  name: 'ha', state, vcpus: 2, memoryMiB: 4096, autostart: false, firmware: 'uefi', secureBoot: false, tpm: false, guestAgent: false,
  network: 'default', hasCdrom: false, media: '', disks: [sys.path, data.path],
})

function route(state: string, snaps: object[] = []) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    if (key === 'GET /api/v1/vms/ha/config') return res(config(state))
    if (key === 'GET /api/v1/vms/ha/disks') return res([sys, data])
    if (key === 'GET /api/v1/vms/ha/snapshots') return res(snaps)
    if (key === 'GET /api/v1/vms/usb') return res([])
    if (key === 'GET /api/v1/vms/isos') return res([])
    if (key === 'GET /api/v1/vms/networks') return res(['default'])
    if (key === 'GET /api/v1/vms/interfaces') return res([])
    if (key === 'GET /api/v1/vms/status') return res({ installed: true, active: true, kvm: true })
    if (key === 'GET /api/v1/vms') return res([])
    if (key === 'POST /api/v1/vms/ha/disks') return res({ path: '/srv/vms/disks/ha-3.qcow2' }, 201)
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

const q = (sel: string) => document.body.querySelector<HTMLElement>(sel)
const body = (fn: ReturnType<typeof vi.fn>, url: string) => {
  const c = fn.mock.calls.find((x) => x[0] === url && x[1]?.method === 'POST')
  return c ? JSON.parse(c[1].body) : undefined
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('Festplatten hinzufügen und entfernen', () => {
  it('sperrt Hinzufügen und Entfernen bei laufender VM, die Systemfestplatte hat keinen Entfernen-Knopf', async () => {
    route('running')
    const w = await mountView()
    expect(w.find('button[data-test="cfg-disk-add"]').attributes('disabled')).toBeDefined()
    const removes = w.findAll('button[data-test="cfg-disk-remove"]')
    expect(removes).toHaveLength(1) // nur die Datenplatte
    expect(removes[0].attributes('disabled')).toBeDefined()
    w.unmount()
  })

  it('fügt eine Festplatte hinzu', async () => {
    const fn = route('shutoff')
    const w = await mountView()
    await w.find('button[data-test="cfg-disk-add"]').trigger('click')
    await flushPromises()
    q('button[data-test="da-submit"]')!.click()
    await flushPromises()
    expect(body(fn, '/api/v1/vms/ha/disks')).toEqual({ sizeGiB: 20, deleteSnapshots: false })
    w.unmount()
  })

  it('verlangt beim Hinzufügen mit Snapshots eine Bestätigung', async () => {
    const fn = route('shutoff', [{ name: 's1', created: '2026-11-17 10:00:00 +0100', state: 'shutoff' }])
    const w = await mountView()
    await w.find('button[data-test="cfg-disk-add"]').trigger('click')
    await flushPromises()
    expect(q('[data-test="da-snapshots"]')!.textContent).toContain('s1')
    expect(q('button[data-test="da-submit"]')!.hasAttribute('disabled')).toBe(true)
    q('[data-test="da-confirm"] input, input[data-test="da-confirm"]')!.click()
    await flushPromises()
    q('button[data-test="da-submit"]')!.click()
    await flushPromises()
    expect(body(fn, '/api/v1/vms/ha/disks').deleteSnapshots).toBe(true)
    w.unmount()
  })

  it('entfernt eine Festplatte standardmäßig ohne die Datei zu löschen', async () => {
    const fn = route('shutoff')
    const w = await mountView()
    await w.find('button[data-test="cfg-disk-remove"]').trigger('click')
    await flushPromises()
    expect(q('[data-test="dx-warn"]')).toBeNull()
    q('button[data-test="dx-submit"]')!.click()
    await flushPromises()
    expect(body(fn, '/api/v1/vms/ha/disks/remove')).toEqual({ path: data.path, deleteFile: false, deleteSnapshots: false })
    w.unmount()
  })

  it('warnt beim endgültigen Löschen der Datei und sendet deleteFile', async () => {
    const fn = route('shutoff')
    const w = await mountView()
    await w.find('button[data-test="cfg-disk-remove"]').trigger('click')
    await flushPromises()
    q('[data-test="dx-file"] input, input[data-test="dx-file"]')!.click()
    await flushPromises()
    expect(q('[data-test="dx-warn"]')).not.toBeNull()
    q('button[data-test="dx-submit"]')!.click()
    await flushPromises()
    expect(body(fn, '/api/v1/vms/ha/disks/remove').deleteFile).toBe(true)
    w.unmount()
  })
})
