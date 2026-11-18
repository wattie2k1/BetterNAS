import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import { createMemoryHistory, createRouter } from 'vue-router'
import VmConfigView from '@/views/vms/VmConfigView.vue'

const res = (body: unknown, status = 200) => Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))
const config = (state: string) => ({
  name: 'ha', state, vcpus: 2, memoryMiB: 4096, autostart: false, firmware: 'uefi', secureBoot: false, tpm: false, guestAgent: false,
  network: 'default', hasCdrom: false, media: '', disks: ['/srv/vms/disks/ha.qcow2'],
})
const disk = { path: '/srv/vms/disks/ha.qcow2', sizeGiB: 32, usedBytes: 2 * 1024 ** 3, virtualSize: 32 * 1024 ** 3 }

function route(state: string, snaps: object[] = []) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    if (key === 'GET /api/v1/vms/ha/config') return res(config(state))
    if (key === 'GET /api/v1/vms/ha/disks') return res([disk])
    if (key === 'GET /api/v1/vms/ha/snapshots') return res(snaps)
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

const q = (sel: string) => document.body.querySelector<HTMLElement>(sel)

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('Festplatte vergrößern', () => {
  it('zeigt Größe; Vergrößern geht bei laufender VM live, bei abgestürzter nicht', async () => {
    route('running')
    let w = await mountView()
    expect(w.find('[data-test="cfg-disks"]').text()).toContain('32 GiB, belegt 2 GiB')
    expect(w.find('button[data-test="cfg-disk-resize"]').attributes('disabled')).toBeUndefined()
    await w.find('button[data-test="cfg-disk-resize"]').trigger('click')
    await flushPromises()
    expect(q('[data-test="dr-live"]')).not.toBeNull()
    w.unmount()
    document.body.innerHTML = ''
    route('crashed')
    w = await mountView()
    expect(w.find('button[data-test="cfg-disk-resize"]').attributes('disabled')).toBeDefined()
    w.unmount()
  })

  it('vergrößert ohne Snapshots', async () => {
    const fn = route('shutoff')
    const w = await mountView()
    await w.find('button[data-test="cfg-disk-resize"]').trigger('click')
    await flushPromises()
    expect(q('[data-test="dr-snapshots"]')).toBeNull()
    q('button[data-test="dr-submit"]')!.click()
    await flushPromises()
    const post = fn.mock.calls.find((c) => c[0] === '/api/v1/vms/ha/disks/resize')
    expect(post).toBeTruthy()
    expect(JSON.parse(post![1].body)).toEqual({ path: disk.path, sizeGiB: 40, deleteSnapshots: false })
    w.unmount()
  })

  it('verlangt bei Snapshots eine Bestätigung zum Löschen', async () => {
    const fn = route('shutoff', [{ name: 's1', created: '2026-11-17 10:00:00 +0100', state: 'shutoff' }])
    const w = await mountView()
    await w.find('button[data-test="cfg-disk-resize"]').trigger('click')
    await flushPromises()
    expect(q('[data-test="dr-snapshots"]')!.textContent).toContain('s1')
    expect(q('button[data-test="dr-submit"]')!.hasAttribute('disabled')).toBe(true)
    q('[data-test="dr-confirm"] input, input[data-test="dr-confirm"]')!.click()
    await flushPromises()
    expect(q('button[data-test="dr-submit"]')!.hasAttribute('disabled')).toBe(false)
    q('button[data-test="dr-submit"]')!.click()
    await flushPromises()
    const post = fn.mock.calls.find((c) => c[0] === '/api/v1/vms/ha/disks/resize')
    expect(JSON.parse(post![1].body).deleteSnapshots).toBe(true)
    w.unmount()
  })
})
