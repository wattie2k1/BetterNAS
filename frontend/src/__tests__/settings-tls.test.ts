import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import Select from 'primevue/select'
import Checkbox from 'primevue/checkbox'
import CertLocalCa from '@/components/settings/CertLocalCa.vue'
import CertAcme from '@/components/settings/CertAcme.vue'
import WebAccessSettings from '@/views/settings/WebAccessSettings.vue'
import { systemApi } from '@/api/system'
import { tlsApi, type TlsStatus } from '@/api/tls'
import { useJobsStore } from '@/stores/jobs'

vi.mock('@/api/system', () => ({
  systemApi: { certificate: vi.fn(), regenerateCertificate: vi.fn(), installCertificate: vi.fn() },
}))
vi.mock('@/api/tls', async (orig) => {
  const real = await orig<typeof import('@/api/tls')>()
  return { ...real, tlsApi: { status: vi.fn(), enableLocalCa: vi.fn(), saveAcme: vi.fn(), removeAcme: vi.fn(), issueAcme: vi.fn() } }
})

const pinia = createPinia()
const plugins = [PrimeVue, ToastService, pinia]

const providers = [
  { id: 'duckdns', label: 'DuckDNS', help: 'Token oben auf der Seite', fields: [{ key: 'token', label: 'Token', secret: true }] },
  { id: 'netcup', label: 'netcup', help: '', fields: [{ key: 'customer', label: 'Kundennummer', secret: false }, { key: 'key', label: 'API-Schlüssel', secret: true }] },
]
const status = (over: Partial<TlsStatus> = {}): TlsStatus => ({
  mode: 'selfsigned',
  localCA: { exists: false, names: [] },
  acme: { configured: false, provider: '', domains: [], email: '', staging: false, agreedTerms: false, credentialsSet: {}, busy: false },
  providers,
  ...over,
})
const cert = { subject: 'CN=nas', issuer: 'CN=nas', dnsNames: ['nas'], ipAddresses: [], notBefore: '2026-01-01T00:00:00Z', notAfter: '2027-01-01T00:00:00Z', fingerprint: 'AA', selfSigned: true, expired: false }

beforeEach(() => vi.clearAllMocks())

describe('CertLocalCa', () => {
  it('richtet die CA mit zusätzlichen Namen als Liste ein', async () => {
    vi.mocked(tlsApi.enableLocalCa).mockResolvedValue(status())
    const w = mount(CertLocalCa, { props: { status: status() }, global: { plugins } })
    expect(w.find('[data-test="localca-download"]').exists()).toBe(false)
    await w.find('#ca-names').setValue('nas.fritz.box, 10.0.0.5')
    await w.find('[data-test="localca-enable"]').trigger('click')
    await flushPromises()
    expect(tlsApi.enableLocalCa).toHaveBeenCalledWith(['nas.fritz.box', '10.0.0.5'], false)
    expect(w.emitted('changed')).toBeTruthy()
  })

  it('bietet den Download an und verlangt für „CA neu erzeugen“ eine Bestätigung', async () => {
    vi.mocked(tlsApi.enableLocalCa).mockResolvedValue(status())
    const st = status({ mode: 'localca', localCA: { exists: true, fingerprint: 'AB:CD', notAfter: '2036-01-01T00:00:00Z', names: ['x.local'] } })
    const w = mount(CertLocalCa, { props: { status: st }, global: { plugins }, attachTo: document.body })
    expect(w.find('[data-test="localca-download"]').attributes('href')).toBe('/api/v1/system/tls/ca.crt')
    expect(w.find('[data-test="localca-info"]').text()).toContain('AB:CD')
    expect((w.find('#ca-names').element as HTMLInputElement).value).toBe('x.local')
    await w.find('[data-test="localca-reset"]').trigger('click')
    await flushPromises()
    expect(tlsApi.enableLocalCa).not.toHaveBeenCalled()
    document.querySelector<HTMLButtonElement>('[data-test="confirm-submit"]')!.click()
    await flushPromises()
    expect(tlsApi.enableLocalCa).toHaveBeenCalledWith(['x.local'], true)
    w.unmount()
  })

  it('zeigt Fehler des Servers', async () => {
    vi.mocked(tlsApi.enableLocalCa).mockRejectedValue(new Error('„x!“ ist weder ein gültiger Name noch eine IP-Adresse.'))
    const w = mount(CertLocalCa, { props: { status: status() }, global: { plugins } })
    await w.find('[data-test="localca-enable"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="localca-error"]').text()).toContain('weder ein gültiger Name')
  })
})

describe('CertAcme', () => {
  const fillAndAgree = async (w: ReturnType<typeof mount>) => {
    await w.findComponent(Select).vm.$emit('update:modelValue', 'duckdns')
    await flushPromises()
    await w.find('#acme-domains').setValue('meinnas.duckdns.org, zweite.duckdns.org')
    await w.find('#acme-f-token').setValue('geheim')
  }

  it('verlangt Anbieter, Domain, Zugangsdaten und Zustimmung, bevor angefordert werden darf', async () => {
    const w = mount(CertAcme, { props: { status: status() }, global: { plugins } })
    expect(w.find('[data-test="acme-missing"]').text()).toContain('DNS-Anbieter')
    expect(w.find('[data-test="acme-issue"]').attributes('disabled')).toBeDefined()
    await fillAndAgree(w)
    expect(w.find('[data-test="acme-missing"]').text()).toContain('zustimmen')
    await w.findComponent(Checkbox).vm.$emit('update:modelValue', true)
    await flushPromises()
    expect(w.find('[data-test="acme-missing"]').exists()).toBe(false)
    expect(w.find('[data-test="acme-issue"]').attributes('disabled')).toBeUndefined()
  })

  it('speichert, fordert als Auftrag an und wartet darauf', async () => {
    vi.mocked(tlsApi.saveAcme).mockResolvedValue(status())
    vi.mocked(tlsApi.issueAcme).mockResolvedValue({ jobId: 'j1' })
    const track = vi.spyOn(useJobsStore(pinia), 'track').mockResolvedValue({} as never)
    const w = mount(CertAcme, { props: { status: status() }, global: { plugins } })
    await fillAndAgree(w)
    // Die Zustimmung-Checkbox ist die erste Checkbox, „Testumgebung“ die zweite.
    await w.findAllComponents(Checkbox)[0]!.vm.$emit('update:modelValue', true)
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(tlsApi.saveAcme).toHaveBeenCalledWith({
      provider: 'duckdns',
      domains: ['meinnas.duckdns.org', 'zweite.duckdns.org'],
      email: '',
      staging: false,
      agreedTerms: true,
      credentials: { token: 'geheim' },
    })
    expect(tlsApi.issueAcme).toHaveBeenCalled()
    expect(track).toHaveBeenCalledWith('j1')
    expect(w.emitted('changed')).toBeTruthy()
  })

  it('zeigt gespeicherte Werte, behält gespeicherte Geheimnisse und meldet den letzten Fehler', async () => {
    const st = status({
      mode: 'acme',
      acme: { configured: true, provider: 'duckdns', domains: ['a.duckdns.org'], email: 'a@b.de', staging: false, agreedTerms: true, credentialsSet: { token: true }, lastIssued: '2026-11-01T10:00:00Z', lastError: 'DNS-Eintrag nicht gefunden', busy: false },
    })
    const w = mount(CertAcme, { props: { status: st }, global: { plugins } })
    await flushPromises()
    expect((w.find('#acme-domains').element as HTMLInputElement).value).toBe('a.duckdns.org')
    expect(w.find('[data-test="acme-last-error"]').text()).toContain('DNS-Eintrag nicht gefunden')
    expect(w.find('[data-test="acme-last-issued"]').exists()).toBe(true)
    // Token ist gespeichert → kein Pflichtfeld mehr.
    expect(w.find('[data-test="acme-missing"]').exists()).toBe(false)
    expect(w.find('[data-test="acme-help"]').text()).toContain('Token')
  })

  it('zeigt den Fehler, wenn das Anfordern scheitert', async () => {
    vi.mocked(tlsApi.saveAcme).mockResolvedValue(status())
    vi.mocked(tlsApi.issueAcme).mockResolvedValue({ jobId: 'j2' })
    vi.spyOn(useJobsStore(pinia), 'track').mockRejectedValue(new Error('Let’s Encrypt: DNS-Eintrag nicht gefunden'))
    const w = mount(CertAcme, { props: { status: status() }, global: { plugins } })
    await fillAndAgree(w)
    await w.findAllComponents(Checkbox)[0]!.vm.$emit('update:modelValue', true)
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('[data-test="acme-error"]').text()).toContain('DNS-Eintrag nicht gefunden')
  })
})

describe('WebAccessSettings mit Zertifikatsquellen', () => {
  it('zeigt die Herkunft und beide neuen Bereiche', async () => {
    vi.mocked(systemApi.certificate).mockResolvedValue({ ...cert, selfSigned: false, mode: 'acme' })
    vi.mocked(tlsApi.status).mockResolvedValue(status({ mode: 'acme' }))
    const w = mount(WebAccessSettings, { global: { plugins } })
    await flushPromises()
    expect(w.find('[data-test="cert-info"]').text()).toContain('Let’s Encrypt')
    expect(w.find('[data-test="cert-localca"]').exists()).toBe(true)
    expect(w.find('[data-test="cert-acme"]').exists()).toBe(true)
  })

  it('funktioniert weiter, wenn der Server die neuen Routen nicht kennt', async () => {
    vi.mocked(systemApi.certificate).mockResolvedValue(cert)
    vi.mocked(tlsApi.status).mockRejectedValue(new Error('nicht gefunden'))
    const w = mount(WebAccessSettings, { global: { plugins } })
    await flushPromises()
    expect(w.find('[data-test="cert-info"]').text()).toContain('Selbstsigniert')
    expect(w.find('[data-test="cert-localca"]').exists()).toBe(false)
    expect(w.find('[data-test="web-error"]').exists()).toBe(false)
  })
})
