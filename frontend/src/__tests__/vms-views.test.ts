import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import VmsView from '@/views/vms/VmsView.vue'

const res = (body: unknown, status = 200) => Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))
const vm = (name: string, state: string, autostart = false) => ({ name, uuid: name, state, vcpus: 2, memoryMiB: 4096, autostart, disks: [] })

function route(status: unknown, list: unknown[]) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    if (key === 'GET /api/v1/vms/status') return res(status)
    if (key === 'GET /api/v1/vms') return res(list)
    if (init?.method === 'POST' || init?.method === 'DELETE') return res(null, 204)
    return res({ error: { code: 'not_found', message: 'x' } }, 404)
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

const mountView = () => mount(VmsView, { global: { plugins: [createPinia(), PrimeVue, ToastService] }, attachTo: document.body })
const calls = (fn: ReturnType<typeof vi.fn>) => fn.mock.calls.map((c) => `${c[1]?.method ?? 'GET'} ${c[0]}`)

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('VmsView', () => {
  it('zeigt Karten je VM', async () => {
    route({ installed: true, active: true, kvm: true }, [vm('debian', 'running'), vm('win', 'shutoff')])
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-test="vm-debian"]').text()).toContain('Läuft')
    expect(w.find('[data-test="vm-win"]').text()).toContain('Aus')
    expect(w.text()).toContain('2 vCPU · 4 GiB')
    w.unmount()
  })

  it('zeigt einen Hinweis, wenn libvirt nicht aktiv ist', async () => {
    route({ installed: true, active: false, kvm: true }, [])
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-test="vm-down"]').text()).toContain('läuft nicht')
    expect(w.find('button[data-test="vm-new"]').attributes('disabled')).toBeDefined()
    w.unmount()
  })

  it('warnt ohne KVM und zeigt den Leerzustand', async () => {
    route({ installed: true, active: true, kvm: false }, [])
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-test="vm-nokvm"]').exists()).toBe(true)
    expect(w.text()).toContain('Noch keine virtuelle Maschine')
    w.unmount()
  })

  it('startet und fährt herunter über die Schaltflächen', async () => {
    const fn = route({ installed: true, active: true, kvm: true }, [vm('debian', 'running'), vm('win', 'shutoff')])
    const w = mountView()
    await flushPromises()
    await w.find('[data-test="vm-win"] button[data-test="vm-start"]').trigger('click')
    await w.find('[data-test="vm-debian"] button[data-test="vm-shutdown"]').trigger('click')
    await flushPromises()
    expect(calls(fn)).toContain('POST /api/v1/vms/win/start')
    expect(calls(fn)).toContain('POST /api/v1/vms/debian/shutdown')
    w.unmount()
  })

  it('schaltet Autostart um', async () => {
    const fn = route({ installed: true, active: true, kvm: true }, [vm('debian', 'running', true)])
    const w = mountView()
    await flushPromises()
    await w.find('[data-test="vm-debian"] input[type="checkbox"]').trigger('click')
    await flushPromises()
    expect(calls(fn)).toContain('POST /api/v1/vms/debian/noautostart')
    w.unmount()
  })

  it('löscht Festplatten nur bei gesetzter Checkbox', async () => {
    const fn = route({ installed: true, active: true, kvm: true }, [vm('win', 'shutoff')])
    const w = mountView()
    await flushPromises()
    await w.find('button[data-test="vm-menu"]').trigger('click')
    await flushPromises()
    const del = [...document.body.querySelectorAll('.p-menu-item-link')].find((e) => e.textContent?.includes('Löschen')) as HTMLElement
    del.click()
    await flushPromises()
    ;(document.body.querySelector('button[data-test="confirm-submit"]') as HTMLElement).click()
    await flushPromises()
    expect(calls(fn)).toContain('DELETE /api/v1/vms/win')

    await w.find('button[data-test="vm-menu"]').trigger('click')
    await flushPromises()
    ;([...document.body.querySelectorAll('.p-menu-item-link')].find((e) => e.textContent?.includes('Löschen')) as HTMLElement).click()
    await flushPromises()
    ;(document.body.querySelector('[data-test="vm-delete-disks"] input') as HTMLElement).click()
    await flushPromises()
    ;(document.body.querySelector('button[data-test="confirm-submit"]') as HTMLElement).click()
    await flushPromises()
    expect(calls(fn)).toContain('DELETE /api/v1/vms/win?disks=1')
    w.unmount()
  })
})
