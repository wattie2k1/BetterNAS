import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import GeneralSettings from '@/views/settings/GeneralSettings.vue'
import WebAccessSettings from '@/views/settings/WebAccessSettings.vue'
import PowerSettings from '@/views/settings/PowerSettings.vue'
import { systemApi } from '@/api/system'

vi.mock('@/api/system', () => ({
  systemApi: { settings: vi.fn(), timezones: vi.fn(), update: vi.fn(), power: vi.fn(), certificate: vi.fn(), regenerateCertificate: vi.fn(), installCertificate: vi.fn() },
}))

const plugins = [PrimeVue, ToastService]
const base = { hostname: 'betternas', timezone: 'Europe/Berlin', ntp: true, ntpSynced: true, ntpServers: [] as string[], time: '' }
const cert = { subject: 'CN=betternas', issuer: 'CN=betternas', dnsNames: ['betternas', 'betternas.local'], ipAddresses: [], notBefore: '2026-01-01T00:00:00Z', notAfter: '2036-01-01T00:00:00Z', fingerprint: 'AA:BB', selfSigned: true, expired: false }

beforeEach(() => vi.clearAllMocks())

describe('GeneralSettings', () => {
  it('speichert nur geänderte Felder und weist auf den neuen Namen hin', async () => {
    vi.mocked(systemApi.settings).mockResolvedValue(base)
    vi.mocked(systemApi.timezones).mockResolvedValue(['Europe/Berlin', 'UTC'])
    vi.mocked(systemApi.update).mockResolvedValue()
    const w = mount(GeneralSettings, { global: { plugins } })
    await flushPromises()
    expect(w.find('[data-test="general-save"]').attributes('disabled')).toBeDefined()
    await w.find('#g-host').setValue('nas-neu')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(systemApi.update).toHaveBeenCalledWith({ hostname: 'nas-neu' })
    expect(w.find('[data-test="general-renamed"]').text()).toContain('https://nas-neu.local')
  })

  it('zeigt Fehler', async () => {
    vi.mocked(systemApi.settings).mockResolvedValue(base)
    vi.mocked(systemApi.timezones).mockResolvedValue(['UTC'])
    vi.mocked(systemApi.update).mockRejectedValue(new Error('Ungültiger Hostname'))
    const w = mount(GeneralSettings, { global: { plugins } })
    await flushPromises()
    await w.find('#g-host').setValue('X')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('[data-test="general-error"]').text()).toContain('Ungültiger Hostname')
  })
})

describe('WebAccessSettings', () => {
  it('zeigt das Zertifikat und installiert ein eigenes', async () => {
    vi.mocked(systemApi.certificate).mockResolvedValue(cert)
    vi.mocked(systemApi.installCertificate).mockResolvedValue({ ...cert, selfSigned: false })
    const w = mount(WebAccessSettings, { global: { plugins }, attachTo: document.body })
    await flushPromises()
    expect(w.find('[data-test="cert-info"]').text()).toContain('betternas.local')
    expect(w.find('[data-test="cert-info"]').text()).toContain('Selbstsigniert')
    expect(w.find('[data-test="cert-install"]').attributes('disabled')).toBeDefined()
    await w.find('#w-cert').setValue('CERT')
    await w.find('#w-key').setValue('KEY')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(systemApi.installCertificate).toHaveBeenCalledWith('CERT', 'KEY')
    expect(w.find('[data-test="cert-info"]').text()).toContain('Zertifizierungsstelle')
    w.unmount()
  })
})

describe('PowerSettings', () => {
  it('verlangt das Bestätigungswort', async () => {
    vi.mocked(systemApi.power).mockResolvedValue()
    const w = mount(PowerSettings, { global: { plugins }, attachTo: document.body })
    await w.find('[data-test="power-reboot"]').trigger('click')
    await flushPromises()
    const confirm = () => document.querySelector<HTMLButtonElement>('[data-test="power-confirm"]')!
    expect(confirm().disabled).toBe(true)
    const input = document.querySelector<HTMLInputElement>('[data-test="power-word"]')!
    input.value = 'NEUSTARTEN'
    input.dispatchEvent(new Event('input'))
    await flushPromises()
    expect(confirm().disabled).toBe(false)
    confirm().click()
    await flushPromises()
    expect(systemApi.power).toHaveBeenCalledWith('reboot', 'NEUSTARTEN')
    expect(w.find('[data-test="power-done"]').exists()).toBe(true)
    w.unmount()
  })
})
