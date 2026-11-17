import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import ConsoleDialog from '@/components/vms/ConsoleDialog.vue'
import IsoDialog from '@/components/vms/IsoDialog.vue'
import SnapshotsDialog from '@/components/vms/SnapshotsDialog.vue'
import VmsView from '@/views/vms/VmsView.vue'

const created: { url: string; opts: unknown; disconnected: boolean; handlers: Record<string, (e: unknown) => void> }[] = []
vi.mock('@novnc/novnc', () => ({
  default: class {
    scaleViewport = false
    resizeSession = true
    background = ''
    rec: (typeof created)[number]
    constructor(_el: HTMLElement, url: string, opts: unknown) {
      this.rec = { url, opts, disconnected: false, handlers: {} }
      created.push(this.rec)
    }
    addEventListener(n: string, h: (e: unknown) => void) {
      this.rec.handlers[n] = h
    }
    disconnect() {
      this.rec.disconnected = true
    }
    focus() {}
    sendCtrlAltDel() {}
  },
}))

const res = (body: unknown, status = 200) => Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }))
const plugins = () => [createPinia(), PrimeVue, ToastService]

function route(extra: Record<string, () => Promise<Response>> = {}) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    if (extra[key]) return extra[key]()
    if (key === 'GET /api/v1/vms/status') return res({ installed: true, active: true, kvm: true })
    if (key === 'GET /api/v1/vms')
      return res([
        { name: 'debian', uuid: 'a', state: 'running', vcpus: 2, memoryMiB: 4096, autostart: false, disks: [] },
        { name: 'win', uuid: 'b', state: 'shutoff', vcpus: 2, memoryMiB: 4096, autostart: false, disks: [] },
      ])
    if (key === 'GET /api/v1/vms/debian/snapshots') return res([{ name: 's1', created: '2026-11-17 10:00:00 +0100', state: 'shutoff' }])
    if (key === 'GET /api/v1/vms/isos') return res(['a.iso'])
    if (init?.method === 'POST' || init?.method === 'DELETE') return res(null, 204)
    return res({ error: { code: 'not_found', message: 'x' } }, 404)
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
const calls = (fn: ReturnType<typeof vi.fn>) => fn.mock.calls.map((c) => `${c[1]?.method ?? 'GET'} ${c[0]}`)

afterEach(() => {
  vi.unstubAllGlobals()
  created.length = 0
  document.body.innerHTML = ''
})

describe('ConsoleDialog', () => {
  it('verbindet über die Konsolen-URL und trennt beim Schließen', async () => {
    route()
    const w = mount(ConsoleDialog, { props: { visible: true, name: 'debian' }, global: { plugins: plugins() }, attachTo: document.body })
    await vi.waitFor(() => expect(created).toHaveLength(1))
    expect(created[0].url).toMatch(/^wss?:\/\/.*\/api\/v1\/vms\/debian\/console$/)
    expect(created[0].opts).toEqual({ wsProtocols: ['binary'] })
    created[0].handlers.connect({})
    await flushPromises()
    expect(w.text()).toContain('Verbunden')
    await w.setProps({ visible: false })
    expect(created[0].disconnected).toBe(true)
    w.unmount()
  })

  it('meldet eine unterbrochene Verbindung', async () => {
    route()
    const w = mount(ConsoleDialog, { props: { visible: true, name: 'debian' }, global: { plugins: plugins() }, attachTo: document.body })
    await vi.waitFor(() => expect(created).toHaveLength(1))
    created[0].handlers.disconnect({ detail: { clean: false } })
    await flushPromises()
    expect(w.find('[data-test="console-state"]').text()).toContain('unterbrochen')
    w.unmount()
  })
})

describe('SnapshotsDialog', () => {
  const mountDlg = () => mount(SnapshotsDialog, { props: { visible: true, name: 'debian' }, global: { plugins: plugins() }, attachTo: document.body })

  it('listet, legt an, setzt zurück und löscht', async () => {
    const fn = route()
    const w = mountDlg()
    await flushPromises()
    expect(w.find('[data-test="snap-s1"]').text()).toContain('s1')

    await w.find('[data-test="snap-name"]').setValue('vor update') // ungültig
    expect(w.find('[data-test="snap-create"]').attributes('disabled')).toBeDefined()
    await w.find('[data-test="snap-name"]').setValue('vor-update')
    await w.find('form').trigger('submit')
    await flushPromises()
    const post = fn.mock.calls.find((c) => c[0] === '/api/v1/vms/debian/snapshots' && c[1]?.method === 'POST')
    expect(JSON.parse(post![1].body)).toEqual({ name: 'vor-update' })

    await w.find('[data-test="snap-revert"]').trigger('click')
    expect(calls(fn)).not.toContain('POST /api/v1/vms/debian/snapshots/s1/revert') // erst nach Bestätigung
    await w.find('[data-test="snap-revert-ok"]').trigger('click')
    await flushPromises()
    expect(calls(fn)).toContain('POST /api/v1/vms/debian/snapshots/s1/revert')

    await w.find('[data-test="snap-delete"]').trigger('click')
    await w.find('[data-test="snap-delete-ok"]').trigger('click')
    await flushPromises()
    expect(calls(fn)).toContain('DELETE /api/v1/vms/debian/snapshots/s1')
    w.unmount()
  })

  it('zeigt Fehler der API', async () => {
    route({ 'POST /api/v1/vms/debian/snapshots': () => res({ error: { code: 'conflict', message: 'Ein Snapshot mit diesem Namen existiert bereits' } }, 409) })
    const w = mountDlg()
    await flushPromises()
    await w.find('[data-test="snap-name"]').setValue('s1')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('[data-test="snap-error"]').text()).toContain('existiert bereits')
    w.unmount()
  })
})

describe('IsoDialog', () => {
  it('lehnt ungültige Dateinamen ohne Anfrage ab', async () => {
    const fn = route()
    const w = mount(IsoDialog, { props: { visible: true }, global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    const input = w.find('[data-test="iso-file"]')
    Object.defineProperty(input.element, 'files', { value: [new File(['x'], 'mein system.iso')], configurable: true })
    await input.trigger('change')
    await flushPromises()
    expect(w.find('[data-test="iso-error"]').text()).toContain('Ungültiger Dateiname')
    expect(calls(fn).some((c) => c.startsWith('PUT'))).toBe(false)
    w.unmount()
  })

  it('löscht erst nach Bestätigung und zeigt den Konflikt', async () => {
    const fn = route({ 'DELETE /api/v1/vms/isos/a.iso': () => res({ error: { code: 'conflict', message: 'Die Datei wird von der VM „debian“ verwendet' } }, 409) })
    const w = mount(IsoDialog, { props: { visible: true }, global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    await w.find('[data-test="iso-delete"]').trigger('click')
    expect(calls(fn)).not.toContain('DELETE /api/v1/vms/isos/a.iso')
    await w.find('[data-test="iso-delete-ok"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="iso-error"]').text()).toContain('verwendet')
    w.unmount()
  })
})

describe('VmsView', () => {
  it('zeigt die Konsolen-Schaltfläche nur bei laufenden VMs', async () => {
    route()
    const w = mount(VmsView, { global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    expect(w.find('[data-test="vm-debian"] [data-test="vm-console"]').exists()).toBe(true)
    expect(w.find('[data-test="vm-win"] [data-test="vm-console"]').exists()).toBe(false)
    await w.find('[data-test="vm-debian"] [data-test="vm-console"]').trigger('click')
    await vi.waitFor(() => expect(created).toHaveLength(1))
    expect(created[0].url).toContain('/api/v1/vms/debian/console')
    w.unmount()
  })
})
