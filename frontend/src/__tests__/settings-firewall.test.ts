import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import FirewallSettings from '@/views/settings/FirewallSettings.vue'
import SshSettings from '@/views/settings/SshSettings.vue'
import { firewallApi, sshApi, type FirewallStatus } from '@/api/firewall'
import { usersApi } from '@/api/users'

vi.mock('@/api/firewall', () => ({
  firewallApi: { status: vi.fn(), apply: vi.fn(), confirm: vi.fn(), revert: vi.fn() },
  sshApi: { get: vi.fn(), set: vi.fn(), keys: vi.fn(), addKey: vi.fn(), removeKey: vi.fn() },
}))
vi.mock('@/api/users', () => ({ usersApi: { users: vi.fn() } }))
const plugins = [PrimeVue, ToastService]
beforeEach(() => vi.clearAllMocks())

const status = (over: Partial<FirewallStatus> = {}): FirewallStatus => ({
  config: { enabled: false, rules: { web: { mode: 'all', networks: [] }, ssh: { mode: 'all', networks: [] } } },
  services: [
    { id: 'web', label: 'Weboberfläche (HTTPS)', ports: ['tcp/443'], required: true },
    { id: 'ssh', label: 'SSH', ports: ['tcp/22'], required: false },
  ],
  active: false,
  pending: false,
  deadline: 0,
  sshPort: 22,
  ...over,
})

describe('FirewallSettings', () => {
  it('sendet die geänderte Konfiguration', async () => {
    vi.mocked(firewallApi.status).mockResolvedValue(status())
    vi.mocked(firewallApi.apply).mockResolvedValue()
    const w = mount(FirewallSettings, { global: { plugins } })
    await flushPromises()
    expect(w.find('[data-test="fw-apply"]').attributes('disabled')).toBeDefined()
    await w.find('input[role="switch"]').setValue(true)
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(firewallApi.apply).toHaveBeenCalledWith({
      enabled: true,
      rules: { web: { mode: 'all', networks: [] }, ssh: { mode: 'all', networks: [] } },
    })
  })

  it('zeigt Fehler der Aussperrprüfung', async () => {
    vi.mocked(firewallApi.status).mockResolvedValue(status())
    vi.mocked(firewallApi.apply).mockRejectedValue(new Error('Weboberfläche für Ihre eigene Adresse gesperrt'))
    const w = mount(FirewallSettings, { global: { plugins } })
    await flushPromises()
    await w.find('input[role="switch"]').setValue(true)
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('[data-test="fw-error"]').text()).toContain('gesperrt')
  })

  it('verlangt die Bestätigung einer laufenden Änderung', async () => {
    vi.mocked(firewallApi.status).mockResolvedValue(status({ pending: true, deadline: Math.floor(Date.now() / 1000) + 100, config: { enabled: true, rules: {} } }))
    vi.mocked(firewallApi.confirm).mockResolvedValue()
    const w = mount(FirewallSettings, { global: { plugins } })
    await flushPromises()
    expect(w.find('[data-test="fw-pending"]').exists()).toBe(true)
    expect(w.find('form').exists()).toBe(false)
    await w.find('[data-test="fw-confirm"]').trigger('click')
    await flushPromises()
    expect(firewallApi.confirm).toHaveBeenCalled()
  })
})

describe('SshSettings', () => {
  const ssh = { port: 22, passwordAuth: true, permitRootLogin: 'no' as const, socketActivated: false, active: true }

  it('speichert die Einstellungen', async () => {
    vi.mocked(sshApi.get).mockResolvedValue(ssh)
    vi.mocked(sshApi.set).mockResolvedValue()
    vi.mocked(sshApi.keys).mockResolvedValue([])
    vi.mocked(usersApi.users).mockResolvedValue([{ name: 'wattie', fullName: '', uid: 1000, groups: [], admin: true, system: true }])
    const w = mount(SshSettings, { global: { plugins } })
    await flushPromises()
    expect(w.find('[data-test="ssh-save"]').attributes('disabled')).toBeDefined()
    await w.find('[data-test="ssh-port"] input').setValue('2222')
    await w.find('[data-test="ssh-port"] input').trigger('blur')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(sshApi.set).toHaveBeenCalledWith({ port: 2222, passwordAuth: true, permitRootLogin: 'no' })
  })

  it('fügt Schlüssel hinzu und entfernt sie', async () => {
    vi.mocked(sshApi.get).mockResolvedValue(ssh)
    vi.mocked(usersApi.users).mockResolvedValue([{ name: 'wattie', fullName: '', uid: 1000, groups: [], admin: true, system: true }])
    vi.mocked(sshApi.keys).mockResolvedValue([{ type: 'ssh-ed25519', fingerprint: 'SHA256:abc', comment: 'laptop' }])
    vi.mocked(sshApi.addKey).mockResolvedValue()
    vi.mocked(sshApi.removeKey).mockResolvedValue()
    const w = mount(SshSettings, { global: { plugins } })
    await flushPromises()
    expect(w.find('[data-test="ssh-keys"]').text()).toContain('laptop')
    await w.find('[data-test="ssh-key-input"]').setValue('ssh-ed25519 AAAA x')
    await w.find('.add').trigger('submit')
    await flushPromises()
    expect(sshApi.addKey).toHaveBeenCalledWith('wattie', 'ssh-ed25519 AAAA x')
    await w.find('[data-test="ssh-key-remove"]').trigger('click')
    await flushPromises()
    expect(sshApi.removeKey).toHaveBeenCalledWith('wattie', 'SHA256:abc')
  })

  it('zeigt Serverfehler beim Hinzufügen', async () => {
    vi.mocked(sshApi.get).mockResolvedValue(ssh)
    vi.mocked(usersApi.users).mockResolvedValue([{ name: 'wattie', fullName: '', uid: 1000, groups: [], admin: true, system: true }])
    vi.mocked(sshApi.keys).mockResolvedValue([])
    vi.mocked(sshApi.addKey).mockRejectedValue(new Error('Das ist kein gültiger öffentlicher SSH-Schlüssel'))
    const w = mount(SshSettings, { global: { plugins } })
    await flushPromises()
    await w.find('[data-test="ssh-key-input"]').setValue('quatsch')
    await w.find('.add').trigger('submit')
    await flushPromises()
    expect(w.find('[data-test="ssh-key-error"]').text()).toContain('kein gültiger')
  })
})
