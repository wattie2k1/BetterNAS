import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import ProxySiteDialog from '@/components/docker/ProxySiteDialog.vue'
import ProxyView from '@/views/docker/ProxyView.vue'
import StacksView from '@/views/docker/StacksView.vue'
import { isPrivateIPv4, publishedPorts, siteError } from '@/utils/proxy'
import type { ContainerInfo, ProxyConfig, ProxySite } from '@/api/docker'

const plugins = () => [createPinia(), PrimeVue, ToastService]
afterEach(() => vi.unstubAllGlobals())

const c = (name: string, ports: string[], state = 'running'): ContainerInfo => ({ id: name, name, image: 'x', state, status: '', stack: '', ports, created: '' })
const site = (o: Partial<ProxySite> = {}): ProxySite => ({ domain: 'jf.example.de', host: '', port: 8096, https: false, public: false, ...o })

describe('Hilfsfunktionen', () => {
  it('erkennt Adressen aus dem Heimnetz', () => {
    for (const ok of ['192.168.1.5', '10.0.0.1', '172.16.0.1', '172.31.255.254']) expect(isPrivateIPv4(ok), ok).toBe(true)
    for (const bad of ['8.8.8.8', '172.32.0.1', '127.0.0.1', '192.168.1', '192.168.1.256', '192.168.01.5', 'nas.local', '::1', '']) expect(isPrivateIPv4(bad), bad).toBe(false)
  })

  it('liest veröffentlichte TCP-Ports laufender Container', () => {
    const list = publishedPorts([
      c('b-app', ['0.0.0.0:8080->80/tcp', '0.0.0.0:8443->443/tcp', '53/udp', '0.0.0.0:5000-5002->5000-5002/tcp']),
      c('a-app', ['0.0.0.0:3000->3000/tcp', '8081->81/tcp', '0.0.0.0:3000->3000/tcp']),
      c('gestoppt', ['0.0.0.0:9000->9000/tcp'], 'exited'),
      c('reverse-proxy-caddy-1', ['0.0.0.0:8443->443/tcp']),
    ])
    expect(list.map((p) => `${p.container}:${p.port}`)).toEqual(['a-app:3000', 'a-app:8081', 'b-app:8080', 'b-app:8443'])
    expect(list[0].label).toBe('a-app – Port 3000')
  })

  it('prüft Einträge', () => {
    expect(siteError(site())).toBe('')
    expect(siteError(site({ domain: '' }))).toContain('Domain')
    for (const bad of ['jellyfin', 'a b.example.de', 'https://jf.example.de', 'jf.example.de/pfad', '*.example.de', '192.168.1.5', 'jf.example.de {']) {
      expect(siteError(site({ domain: bad })), bad).not.toBe('')
    }
    expect(siteError(site({ domain: 'JF.Example.DE' }))).toBe('') // wird kleingeschrieben
    expect(siteError(site(), [site()])).toContain('schon eingetragen')
    expect(siteError(site({ port: 0 }))).toContain('Port')
    expect(siteError(site({ port: 70000 }))).toContain('Port')
    expect(siteError(site({ host: '8.8.8.8' }))).toContain('Heimnetz')
    expect(siteError(site({ host: '192.168.1.50', port: 80 }))).toBe('')
  })
})

describe('ProxySiteDialog', () => {
  const mountIt = (props: Record<string, unknown> = {}) =>
    mount(ProxySiteDialog, {
      props: { visible: true, site: null, others: [], containers: [c('jellyfin', ['0.0.0.0:8096->8096/tcp'])], ...props },
      global: { plugins: plugins() },
    })

  it('übernimmt den Port aus einem Container und liefert den Eintrag', async () => {
    const w = mountIt()
    await flushPromises()
    expect(w.find('button[data-test="px-save"]').attributes('disabled')).toBeDefined()
    await w.find('input[data-test="px-domain"]').setValue('Jellyfin.Example.de')
    await w.find('select[data-test="px-pick"]').setValue('8096')
    expect((w.find('input[data-test="px-port"]').element as HTMLInputElement).value).toBe('8096')
    expect(w.find('[data-test="px-public-warning"]').exists()).toBe(false)
    await w.find('button[data-test="px-save"]').trigger('click')
    expect(w.emitted('save')).toEqual([[{ domain: 'jellyfin.example.de', host: '', port: 8096, https: false, public: false }]])
  })

  it('warnt bei Internet-Zugriff und liefert ein anderes Gerät mit HTTPS', async () => {
    const w = mountIt()
    await flushPromises()
    await w.find('input[data-test="px-domain"]').setValue('drucker.example.de')
    await w.find('input[data-test="px-kind-external"]').setValue(true)
    expect(w.find('select[data-test="px-pick"]').exists()).toBe(false)
    await w.find('input[data-test="px-host"]').setValue('192.168.1.50')
    await w.find('input[data-test="px-port"]').setValue('443')
    await w.find('input[data-test="px-https"]').setValue(true)
    await w.find('input#px-public').setValue(true)
    expect(w.find('[data-test="px-public-warning"]').text()).toContain('jeder im Internet')
    await w.find('button[data-test="px-save"]').trigger('click')
    expect(w.emitted('save')).toEqual([[{ domain: 'drucker.example.de', host: '192.168.1.50', port: 443, https: true, public: true }]])
  })

  it('zeigt Fehler und sperrt Übernehmen; erkennt doppelte Domains', async () => {
    const w = mountIt({ others: [site()] })
    await flushPromises()
    await w.find('input[data-test="px-domain"]').setValue('jf.example.de')
    await w.find('input[data-test="px-port"]').setValue('80')
    expect(w.find('[data-test="px-error"]').text()).toContain('schon eingetragen')
    expect(w.find('button[data-test="px-save"]').attributes('disabled')).toBeDefined()
    await w.find('input[data-test="px-domain"]').setValue('x')
    expect(w.find('[data-test="px-error"]').exists()).toBe(true)
    await w.find('input[data-test="px-kind-external"]').setValue(true)
    await w.find('input[data-test="px-domain"]').setValue('neu.example.de')
    await w.find('input[data-test="px-host"]').setValue('8.8.8.8')
    expect(w.find('[data-test="px-error"]').text()).toContain('Heimnetz')
  })

  it('füllt beim Bearbeiten die Felder', async () => {
    const w = mountIt({ site: site({ host: '192.168.1.50', port: 80, https: true, public: true }) })
    await flushPromises()
    expect((w.find('input[data-test="px-domain"]').element as HTMLInputElement).value).toBe('jf.example.de')
    expect((w.find('input[data-test="px-host"]').element as HTMLInputElement).value).toBe('192.168.1.50')
    expect((w.find('input[data-test="px-https"]').element as HTMLInputElement).checked).toBe(true)
  })
})

function stubApi(routes: Record<string, unknown>) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    const v = routes[key]
    if (v === undefined) return Promise.resolve(new Response(JSON.stringify({ error: { code: 'not_found', message: key } }), { status: 404 }))
    return Promise.resolve(new Response(JSON.stringify(v), { status: key.startsWith('POST /api/v1/docker/proxy/apply') ? 202 : 200 }))
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
const cfg: ProxyConfig = { enabled: true, email: 'a@example.de', httpPort: 8080, httpsPort: 8443, sites: [site(), site({ domain: 'a.example.de', public: true })] }
const base = {
  'GET /api/v1/docker/status': { installed: true, active: true, version: '26' },
  'GET /api/v1/stacks': [],
  'GET /api/v1/containers': [c('jellyfin', ['0.0.0.0:8096->8096/tcp']), c('reverse-proxy-caddy-1', ['0.0.0.0:8443->443/tcp'])],
  'GET /api/v1/docker/proxy': cfg,
}

describe('ProxyView', () => {
  const mountView = async (routes: Record<string, unknown> = {}) => {
    const fn = stubApi({ ...base, ...routes })
    const w = mount(ProxyView, { global: { plugins: plugins(), stubs: { RouterLink: { template: '<a><slot /></a>' } } }, attachTo: document.body })
    await flushPromises()
    return { w, fn }
  }

  it('zeigt Einstellungen, Einträge und den Zustand von Caddy', async () => {
    const { w } = await mountView()
    expect((w.find('input[data-test="proxy-email"]').element as HTMLInputElement).value).toBe('a@example.de')
    expect(w.find('[data-test="proxy-status"]').text()).toContain('Läuft')
    expect(w.find('[data-test="site-jf.example.de"]').text()).toContain('Nur Heimnetz')
    expect(w.find('[data-test="site-a.example.de"]').text()).toContain('Auch Internet')
    expect(w.find('[data-test="howto-https"]').text()).toBe('8443')
    expect(w.find('button[data-test="proxy-apply"]').attributes('disabled')).toBeDefined() // nichts geändert, läuft
    w.unmount()
  })

  it('speichert und wendet an: erst PUT, dann POST', async () => {
    const { w, fn } = await mountView({
      'PUT /api/v1/docker/proxy': { ...cfg, httpsPort: 9443 },
      'POST /api/v1/docker/proxy/apply': { jobId: 'j1' },
    })
    await w.find('input[data-test="proxy-https"]').setValue('9443')
    expect(w.find('[data-test="howto-https"]').text()).toBe('9443')
    expect(w.find('[data-test="proxy-dirty"]').exists()).toBe(true)
    await w.find('button[data-test="proxy-apply"]').trigger('click')
    await vi.waitFor(() => expect(fn).toHaveBeenCalledWith('/api/v1/docker/proxy/apply', expect.objectContaining({ method: 'POST' })))
    const calls = fn.mock.calls.map((x) => `${x[1]?.method ?? 'GET'} ${x[0]}`)
    expect(calls.indexOf('PUT /api/v1/docker/proxy')).toBeGreaterThan(-1)
    expect(calls.indexOf('PUT /api/v1/docker/proxy')).toBeLessThan(calls.indexOf('POST /api/v1/docker/proxy/apply'))
    const put = fn.mock.calls.find((x) => x[1]?.method === 'PUT')!
    expect(JSON.parse(put[1].body as string)).toMatchObject({ httpsPort: 9443, httpPort: 8080, enabled: true })
    expect(typeof JSON.parse(put[1].body as string).httpsPort).toBe('number')
    w.unmount()
  })

  it('sperrt bei Port 443 oder gleichen Ports', async () => {
    const { w, fn } = await mountView()
    await w.find('input[data-test="proxy-https"]').setValue('443')
    expect(w.find('[data-test="proxy-port-error"]').text()).toContain('443')
    expect(w.find('button[data-test="proxy-apply"]').attributes('disabled')).toBeDefined()
    await w.find('input[data-test="proxy-https"]').setValue('8080')
    expect(w.find('[data-test="proxy-port-error"]').text()).toContain('verschieden')
    expect(fn).not.toHaveBeenCalledWith('/api/v1/docker/proxy', expect.objectContaining({ method: 'PUT' }))
    w.unmount()
  })

  it('legt Einträge an und entfernt sie nach Bestätigung', async () => {
    const { w } = await mountView()
    await w.find('button[data-test="proxy-add"]').trigger('click')
    await flushPromises()
    ;(document.querySelector('input[data-test="px-domain"]') as HTMLInputElement).value = ''
    const dom = document.querySelector('input[data-test="px-domain"]') as HTMLInputElement
    dom.value = 'neu.example.de'
    dom.dispatchEvent(new Event('input'))
    const port = document.querySelector('input[data-test="px-port"]') as HTMLInputElement
    port.value = '3000'
    port.dispatchEvent(new Event('input'))
    await flushPromises()
    ;(document.querySelector('button[data-test="px-save"]') as HTMLElement).click()
    await flushPromises()
    expect(w.find('[data-test="site-neu.example.de"]').exists()).toBe(true)
    expect(w.find('[data-test="proxy-dirty"]').exists()).toBe(true)
    await w.find('[data-test="site-neu.example.de"] button[aria-label="Entfernen"]').trigger('click')
    await flushPromises()
    ;(document.querySelector('[data-test="confirm-submit"]') as HTMLElement).click()
    await flushPromises()
    expect(w.find('[data-test="site-neu.example.de"]').exists()).toBe(false)
    w.unmount()
  })

  it('zeigt „Nicht eingerichtet“, solange es keinen Caddy gibt', async () => {
    const { w } = await mountView({ 'GET /api/v1/containers': [], 'GET /api/v1/docker/proxy': { ...cfg, enabled: false, sites: [] } })
    expect(w.find('[data-test="proxy-status"]').text()).toContain('Nicht eingerichtet')
    expect(w.find('.empty').exists()).toBe(true)
    w.unmount()
  })
})

describe('Stack-Liste', () => {
  it('kennzeichnet den vom Reverse-Proxy verwalteten Stack', async () => {
    stubApi({ ...base, 'GET /api/v1/stacks': [{ name: 'reverse-proxy', running: 1, total: 1 }, { name: 'media', running: 1, total: 1 }] })
    const w = mount(StacksView, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.find('[data-test="stack-reverse-proxy"] [data-test="managed-tag"]').exists()).toBe(true)
    expect(w.find('[data-test="stack-media"] [data-test="managed-tag"]').exists()).toBe(false)
  })
})
