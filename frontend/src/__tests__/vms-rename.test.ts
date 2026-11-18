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

function route(state: string, snaps: object[] = []) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    if (key === 'GET /api/v1/vms/ha/config') return res(config(state))
    if (key === 'GET /api/v1/vms/ha/snapshots') return res(snaps)
    if (key === 'GET /api/v1/vms/ha/disks') return res([])
    if (key === 'GET /api/v1/vms/usb') return res([])
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
  return { w, router }
}

const q = (sel: string) => document.body.querySelector<HTMLElement>(sel)
const setName = async (v: string) => {
  const input = q('[data-test="rn-name"]') as HTMLInputElement
  input.value = v
  input.dispatchEvent(new Event('input'))
  await flushPromises()
}
const submitBody = (fn: ReturnType<typeof vi.fn>) => {
  const c = fn.mock.calls.find((x) => x[0] === '/api/v1/vms/ha/rename' && x[1]?.method === 'POST')
  return c ? JSON.parse(c[1].body) : undefined
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('VM umbenennen', () => {
  it('sperrt den Knopf bei laufender VM', async () => {
    route('running')
    const { w } = await mountView()
    expect(w.find('button[data-test="cfg-rename"]').attributes('disabled')).toBeDefined()
    w.unmount()
  })

  it('prüft den Namen und benennt um, danach geht es zur neuen Seite', async () => {
    const fn = route('shutoff')
    const { w, router } = await mountView()
    await w.find('button[data-test="cfg-rename"]').trigger('click')
    await flushPromises()
    expect(q('button[data-test="rn-submit"]')!.hasAttribute('disabled')).toBe(true) // Name unverändert
    await setName('Falsch Name')
    expect(q('[data-test="rn-hint"]')!.textContent).toContain('Kleinbuchstaben')
    expect(q('button[data-test="rn-submit"]')!.hasAttribute('disabled')).toBe(true)
    await setName('homeassistant')
    expect(q('button[data-test="rn-submit"]')!.hasAttribute('disabled')).toBe(false)
    q('button[data-test="rn-submit"]')!.click()
    await flushPromises()
    expect(submitBody(fn)).toEqual({ newName: 'homeassistant', renameFiles: true, deleteSnapshots: false })
    expect(router.currentRoute.value.path).toBe('/vms/homeassistant')
    w.unmount()
  })

  it('verlangt Bestätigung für Snapshots nur, wenn die Dateien mit umbenannt werden', async () => {
    const fn = route('shutoff', [{ name: 's1', created: '2026-11-17 10:00:00 +0100', state: 'shutoff' }])
    const { w } = await mountView()
    await w.find('button[data-test="cfg-rename"]').trigger('click')
    await flushPromises()
    await setName('neu')
    expect(q('[data-test="rn-snapshots"]')!.textContent).toContain('s1')
    expect(q('button[data-test="rn-submit"]')!.hasAttribute('disabled')).toBe(true)
    // Ohne Datei-Umbenennung stören Snapshots nicht.
    q('[data-test="rn-files"] input, input[data-test="rn-files"]')!.click()
    await flushPromises()
    expect(q('[data-test="rn-snapshots"]')).toBeNull()
    expect(q('button[data-test="rn-submit"]')!.hasAttribute('disabled')).toBe(false)
    q('button[data-test="rn-submit"]')!.click()
    await flushPromises()
    expect(submitBody(fn)).toEqual({ newName: 'neu', renameFiles: false, deleteSnapshots: false })
    w.unmount()
  })

  it('sendet deleteSnapshots nach Bestätigung', async () => {
    const fn = route('shutoff', [{ name: 's1', created: '2026-11-17 10:00:00 +0100', state: 'shutoff' }])
    const { w } = await mountView()
    await w.find('button[data-test="cfg-rename"]').trigger('click')
    await flushPromises()
    await setName('neu')
    q('[data-test="rn-confirm"] input, input[data-test="rn-confirm"]')!.click()
    await flushPromises()
    q('button[data-test="rn-submit"]')!.click()
    await flushPromises()
    expect(submitBody(fn)).toEqual({ newName: 'neu', renameFiles: true, deleteSnapshots: true })
    w.unmount()
  })
})
