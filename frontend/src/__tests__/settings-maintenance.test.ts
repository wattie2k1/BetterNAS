import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import SecuritySettings from '@/views/settings/SecuritySettings.vue'
import LogsSettings from '@/views/settings/LogsSettings.vue'
import AboutSettings from '@/views/settings/AboutSettings.vue'
import { maintenanceApi, securityApi, fetchLog } from '@/api/security'
import { api } from '@/api/client'

vi.mock('@/api/security', async (orig) => ({
  ...(await orig<typeof import('@/api/security')>()),
  securityApi: { policy: vi.fn(), setPolicy: vi.fn(), sessions: vi.fn(), revoke: vi.fn(), revokeOthers: vi.fn() },
  maintenanceApi: { audit: vi.fn(), factoryReset: vi.fn() },
  fetchLog: vi.fn(),
}))
vi.mock('@/api/client', () => ({ api: { get: vi.fn() } }))

const plugins = [PrimeVue, ToastService]
const policy = { sessionHours: 12, maxFailures: 5, lockoutMinutes: 5, requireTotp: false }
const sess = [
  { id: 'aaaa', username: 'anna', createdAt: '2026-11-18T10:00:00Z', expiresAt: '2026-11-18T22:00:00Z', ip: '192.168.1.10', current: true },
  { id: 'bbbb', username: 'anna', createdAt: '2026-11-18T09:00:00Z', expiresAt: '2026-11-18T21:00:00Z', ip: '192.168.1.20', current: false },
]

beforeEach(() => vi.clearAllMocks())

describe('SecuritySettings', () => {
  it('speichert geänderte Werte und beendet fremde Sitzungen', async () => {
    vi.mocked(securityApi.policy).mockResolvedValue(policy)
    vi.mocked(securityApi.sessions).mockResolvedValue(sess)
    vi.mocked(securityApi.setPolicy).mockResolvedValue()
    vi.mocked(securityApi.revoke).mockResolvedValue()
    const w = mount(SecuritySettings, { global: { plugins } })
    await flushPromises()
    expect(w.find('[data-test="security-save"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="sessions-table"]').text()).toContain('diese Sitzung')
    expect(w.find('[data-test="session-revoke-aaaa"]').exists()).toBe(false)
    await w.find('[data-test="security-require-totp"] input').setValue(true)
    expect(w.find('[data-test="security-save"]').attributes('disabled')).toBeUndefined()
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(securityApi.setPolicy).toHaveBeenCalledWith({ ...policy, requireTotp: true })
    await w.find('[data-test="session-revoke-bbbb"]').trigger('click')
    await flushPromises()
    expect(securityApi.revoke).toHaveBeenCalledWith('bbbb')
  })

  it('zeigt Fehler beim Speichern', async () => {
    vi.mocked(securityApi.policy).mockResolvedValue(policy)
    vi.mocked(securityApi.sessions).mockResolvedValue([])
    vi.mocked(securityApi.setPolicy).mockRejectedValue(new Error('Die Sitzungsdauer muss zwischen 1 und 720 Stunden liegen'))
    const w = mount(SecuritySettings, { global: { plugins } })
    await flushPromises()
    await w.find('[data-test="security-require-totp"] input').setValue(true)
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('[data-test="security-error"]').text()).toContain('Sitzungsdauer')
  })
})

describe('LogsSettings', () => {
  it('zeigt Audit, wechselt auf ein Journal und bietet Downloads', async () => {
    vi.mocked(maintenanceApi.audit).mockResolvedValue([{ id: 1, time: '2026-11-18T10:00:00Z', actor: 'anna', action: 'auth.login', target: '1.2.3.4', result: 'ok' }])
    vi.mocked(fetchLog).mockResolvedValue('2026-11-18 api gestartet')
    const w = mount(LogsSettings, { global: { plugins } })
    await flushPromises()
    expect(w.find('[data-test="audit-table"]').text()).toContain('auth.login')
    expect(w.find('[data-test="logs-download"]').exists()).toBe(false)
    expect(w.find('[data-test="support-bundle"]').attributes('href')).toBe('/api/v1/support-bundle')
    ;(w.vm as unknown as { source: string }).source = 'api'
    await flushPromises()
    expect(fetchLog).toHaveBeenCalledWith('api', 200)
    expect(w.find('[data-test="log-text"]').text()).toContain('api gestartet')
    expect(w.find('[data-test="logs-download"]').attributes('href')).toBe('/api/v1/logs/api?lines=200&download=1')
  })
})

describe('AboutSettings', () => {
  it('zeigt Version und verlangt das Wort für die Werkseinstellungen', async () => {
    vi.mocked(api.get).mockResolvedValue({ version: '0.7.0', hostname: 'nas', kernel: '6.1', uptimeSeconds: 3600 })
    vi.mocked(maintenanceApi.factoryReset).mockResolvedValue()
    const w = mount(AboutSettings, { global: { plugins }, attachTo: document.body })
    await flushPromises()
    expect(w.find('[data-test="about-facts"]').text()).toContain('0.7.0')
    expect(w.find('[data-test="about-facts"]').text()).toContain('GNU General Public License v3.0')
    await w.find('[data-test="reset-open"]').trigger('click')
    await flushPromises()
    const confirm = () => document.querySelector<HTMLButtonElement>('[data-test="reset-confirm"]')!
    expect(confirm().disabled).toBe(true)
    const input = document.querySelector<HTMLInputElement>('[data-test="reset-word"]')!
    input.value = 'ZURÜCKSETZEN'
    input.dispatchEvent(new Event('input'))
    await flushPromises()
    expect(confirm().disabled).toBe(false)
    confirm().click()
    await flushPromises()
    expect(maintenanceApi.factoryReset).toHaveBeenCalledWith('ZURÜCKSETZEN')
    expect(w.find('[data-test="reset-done"]').exists()).toBe(true)
    w.unmount()
  })
})
