import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import { createMemoryHistory, createRouter } from 'vue-router'
import VmConfigView from '@/views/vms/VmConfigView.vue'

const res = (body: unknown, status = 200) => Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))
const config = (state: string, pci: string[] = []) => ({
  name: 'ha', state, vcpus: 2, maxVcpus: 2, memoryMiB: 4096, maxMemoryMiB: 4096, autostart: false, firmware: 'uefi', secureBoot: false, tpm: false,
  guestAgent: false, network: 'default', hasCdrom: false, media: '', disks: ['/srv/vms/disks/ha.qcow2'], boot: 'hd', mac: '52:54:00:48:4f:ce', usb: [],
  cpuMode: 'host-passthrough', sockets: 0, threads: 0, hyperv: false, clock: 'utc', extraNics: [], diskOpts: [], sharedDirs: [], pci,
})
const devices = [
  { address: '0000:00:1b.0', id: '8086:8c20', name: 'Intel HD Audio', kind: 'audio', group: 7, driver: 'snd_hda_intel' },
  { address: '0000:01:00.0', id: '10de:1b80', name: 'NVIDIA GTX 1080', kind: 'gpu', group: 10, driver: 'nouveau', note: 'Grafikkarten brauchen meist UEFI.' },
  { address: '0000:01:00.1', id: '10de:10f0', name: 'NVIDIA Audio', kind: 'audio', group: 10 },
  { address: '0000:00:1f.2', id: '8086:8c02', name: 'SATA Controller', kind: 'storage', group: 9, blocked: 'Datenträger des NAS hängen an diesem Gerät (sda)' },
]

function route(cfg: unknown, pci: unknown = { iommu: true, devices }) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    if (key === 'GET /api/v1/vms/ha/config') return res(cfg)
    if (key === 'GET /api/v1/vms/pci') return res(pci)
    if (key === 'GET /api/v1/shares') return res([])
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

describe('PCI-Passthrough', () => {
  it('zeigt nutzbare Gruppen und listet gesperrte Geräte mit Grund', async () => {
    route(config('shutoff'))
    const w = await mountView()
    expect(w.findAll('[data-test="pci-group"]')).toHaveLength(2) // Audio allein, GPU+Audio als Gruppe
    expect(w.find('[data-test="pci"]').text()).toContain('nur zusammen durchreichbar')
    expect(w.find('[data-test="pci-blocked"]').text()).toContain('Datenträger des NAS')
    expect(w.find('[data-test="pci-blocked"]').text()).toContain('SATA Controller')
    w.unmount()
  })

  it('wählt eine Gruppe als Ganzes und sendet alle Adressen', async () => {
    const fn = route(config('shutoff'))
    const w = await mountView()
    expect(w.find('[data-test="pci-warn"]').exists()).toBe(false)
    await w.find('[data-test="pci-0000:01:00.0"] input').setValue(true)
    await flushPromises()
    expect(w.find('[data-test="pci-warn"]').exists()).toBe(true)
    expect(w.find('[data-test="pci-note"]').text()).toContain('UEFI')
    await save(w).trigger('click')
    await flushPromises()
    expect(posted(fn).pci.sort()).toEqual(['0000:01:00.0', '0000:01:00.1'])
    w.unmount()
  })

  it('sendet ohne Änderung nichts davon und entfernt zugewiesene Geräte', async () => {
    const fn = route(config('shutoff', ['0000:00:1b.0']))
    let w = await mountView()
    expect((w.find('[data-test="pci-0000:00:1b.0"] input').element as HTMLInputElement).checked).toBe(true)
    await w.find('[data-test="cfg-ram"] input').setValue('8192')
    await w.find('[data-test="cfg-ram"] input').trigger('blur')
    await save(w).trigger('click')
    await flushPromises()
    expect(posted(fn).pci).toBeUndefined()
    w.unmount()
    document.body.innerHTML = ''
    const fn2 = route(config('shutoff', ['0000:00:1b.0']))
    w = await mountView()
    await w.find('[data-test="pci-0000:00:1b.0"] input').setValue(false)
    await flushPromises()
    await save(w).trigger('click')
    await flushPromises()
    expect(posted(fn2).pci).toEqual([])
    w.unmount()
  })

  it('zeigt fehlende zugewiesene Geräte und erklärt die inaktive IOMMU', async () => {
    route(config('shutoff', ['0000:09:00.0']))
    let w = await mountView()
    expect(w.find('[data-test="pci-missing"]').text()).toContain('0000:09:00.0')
    w.unmount()
    document.body.innerHTML = ''
    route(config('shutoff'), { iommu: false, devices: devices.map((d) => ({ ...d, blocked: 'Die IOMMU ist nicht aktiv' })) })
    w = await mountView()
    expect(w.find('[data-test="pci-noiommu"]').text()).toContain('intel_iommu=on')
    expect(w.findAll('[data-test="pci-group"]')).toHaveLength(0)
    w.unmount()
  })

  it('sperrt die Auswahl bei laufender VM', async () => {
    route(config('running'))
    const w = await mountView()
    expect(w.find('[data-test="pci-0000:00:1b.0"] input').attributes('disabled')).toBeDefined()
    w.unmount()
  })
})
