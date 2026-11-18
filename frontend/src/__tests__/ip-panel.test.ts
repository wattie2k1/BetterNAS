import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import IpPanel from '@/components/settings/IpPanel.vue'
import { networkApi, type IpStatus } from '@/api/network'

vi.mock('@/api/network', () => ({ networkApi: { status: vi.fn(), configure: vi.fn(), confirm: vi.fn(), revert: vi.fn() } }))

const dhcp: IpStatus = {
  interface: 'br0',
  bridgeActive: true,
  config: { mode: 'dhcp', address: '', gateway: '', dns: [], domains: [] },
  current: { addresses: ['192.168.1.65/24'], gateway: '192.168.1.1', dns: ['192.168.1.1', 'fd34::1'], domains: [] },
  interfaces: [],
  pending: false,
  deadline: 0,
  hostname: 'betternas',
}
const mountIt = () => mount(IpPanel, { global: { plugins: [PrimeVue, ToastService] } })

describe('IpPanel', () => {
  beforeEach(() => vi.clearAllMocks())

  it('zeigt die aktuelle Konfiguration und den Adress-Hinweis', async () => {
    vi.mocked(networkApi.status).mockResolvedValue(dhcp)
    const w = mountIt()
    await flushPromises()
    expect(w.find('[data-test="ip-current"]').text()).toContain('192.168.1.65/24')
    expect(w.find('[data-test="ip-hint"]').text()).toContain('betternas.local')
    expect(w.find('[data-test="ip-static"]').exists()).toBe(false)
  })

  it('übernimmt die aktuellen Werte als Vorschlag und sendet die statische Konfiguration', async () => {
    vi.mocked(networkApi.status).mockResolvedValueOnce(dhcp).mockResolvedValue({ ...dhcp, pending: true, deadline: Math.floor(Date.now() / 1000) + 100, config: { mode: 'static', address: '192.168.1.65/24', gateway: '192.168.1.1', dns: ['192.168.1.1'], domains: [] } })
    vi.mocked(networkApi.configure).mockResolvedValue()
    const w = mountIt()
    await flushPromises()
    const buttons = w.findAll('[data-test="ip-mode"] [role="radio"], [data-test="ip-mode"] button, [data-test="ip-mode"] .p-togglebutton')
    await buttons[1].trigger('click')
    await flushPromises()
    expect((w.find('#ip-address').element as HTMLInputElement).value).toBe('192.168.1.65')
    expect((w.find('#ip-netmask').element as HTMLInputElement).value).toBe('255.255.255.0')
    expect((w.find('#ip-dns').element as HTMLInputElement).value).toBe('192.168.1.1')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(networkApi.configure).toHaveBeenCalledWith({ mode: 'static', address: '192.168.1.65/24', gateway: '192.168.1.1', dns: ['192.168.1.1'], domains: [] })
    expect(w.find('[data-test="ip-pending"]').exists()).toBe(true)
    expect(w.find('[data-test="ip-links"]').text()).toContain('https://192.168.1.65')
  })

  it('bestätigt und nimmt zurück', async () => {
    vi.mocked(networkApi.status).mockResolvedValue({ ...dhcp, pending: true, deadline: Math.floor(Date.now() / 1000) + 100 })
    vi.mocked(networkApi.confirm).mockResolvedValue()
    vi.mocked(networkApi.revert).mockResolvedValue()
    const w = mountIt()
    await flushPromises()
    await w.find('[data-test="ip-confirm"]').trigger('click')
    await flushPromises()
    expect(networkApi.confirm).toHaveBeenCalled()
    await w.find('[data-test="ip-revert"]').trigger('click')
    await flushPromises()
    expect(networkApi.revert).toHaveBeenCalled()
  })

  it('zeigt Fehler der API', async () => {
    vi.mocked(networkApi.status).mockResolvedValue(dhcp)
    vi.mocked(networkApi.configure).mockRejectedValue(new Error('Ungültiges Gateway'))
    const w = mountIt()
    await flushPromises()
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('[data-test="ip-error"]').text()).toContain('Ungültiges Gateway')
  })
})
