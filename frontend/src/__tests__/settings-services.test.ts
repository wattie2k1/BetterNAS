import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import ServicesSettings from '@/views/settings/ServicesSettings.vue'
import FileServicesSettings from '@/views/settings/FileServicesSettings.vue'
import DockerSettings from '@/views/settings/DockerSettings.vue'
import { servicesApi } from '@/api/system'

vi.mock('@/api/system', () => ({ servicesApi: { list: vi.fn(), act: vi.fn(), smb: vi.fn(), setSmb: vi.fn(), docker: vi.fn(), setDocker: vi.fn() } }))
const plugins = [PrimeVue, ToastService]
// Die Docker-Einstellungen nutzen den Docker-Store (Speicherorte); dessen Abfragen laufen hier ins Leere.
const dockerPlugins = () => [createPinia(), PrimeVue, ToastService]
beforeEach(() => vi.clearAllMocks())

describe('ServicesSettings', () => {
  it('startet direkt, fragt aber vor dem Neustart von Docker', async () => {
    vi.mocked(servicesApi.list).mockResolvedValue([
      { unit: 'nmbd', label: 'Netzwerkumgebung', active: false, enabled: true, installed: true },
      { unit: 'docker', label: 'Docker', active: true, enabled: true, installed: true },
    ])
    vi.mocked(servicesApi.act).mockResolvedValue()
    const w = mount(ServicesSettings, { global: { plugins }, attachTo: document.body })
    await flushPromises()
    await w.find('[data-test="svc-nmbd"] button[aria-label$="starten"]').trigger('click')
    await flushPromises()
    expect(servicesApi.act).toHaveBeenCalledWith('nmbd', 'start')
    await w.find('[data-test="svc-docker"] button[aria-label$="neu starten"]').trigger('click')
    await flushPromises()
    expect(servicesApi.act).toHaveBeenCalledTimes(1)
    document.querySelector<HTMLElement>('[data-test="confirm-submit"]')!.click()
    await flushPromises()
    expect(servicesApi.act).toHaveBeenCalledWith('docker', 'restart')
    w.unmount()
  })
})

describe('FileServicesSettings', () => {
  it('speichert die SMB-Werte', async () => {
    vi.mocked(servicesApi.smb).mockResolvedValue({ workgroup: 'WORKGROUP', serverName: 'BetterNAS', minProtocol: 'SMB2_10' })
    vi.mocked(servicesApi.setSmb).mockResolvedValue()
    const w = mount(FileServicesSettings, { global: { plugins } })
    await flushPromises()
    expect(w.find('[data-test="smb-save"]').attributes('disabled')).toBeDefined()
    await w.find('#smb-wg').setValue('heim')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(servicesApi.setSmb).toHaveBeenCalledWith({ workgroup: 'heim', serverName: 'BetterNAS', minProtocol: 'SMB2_10' })
  })
})

describe('DockerSettings', () => {
  it('speichert und weist auf den Neustart hin', async () => {
    vi.mocked(servicesApi.docker).mockResolvedValue({ dataRoot: '/srv/appdata/docker', logMaxSize: '10m', logMaxFile: 3, registryMirrors: [], liveRestore: false })
    vi.mocked(servicesApi.setDocker).mockResolvedValue()
    const w = mount(DockerSettings, { global: { plugins: dockerPlugins() } })
    await flushPromises()
    expect(w.find('[data-test="docker-dataroot"]').text()).toBe('/srv/appdata/docker')
    await w.find('#d-size').setValue('50m')
    await w.find('#d-mirrors').setValue('https://a.example\nhttps://b.example')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(servicesApi.setDocker).toHaveBeenCalledWith({ logMaxSize: '50m', logMaxFile: 3, registryMirrors: ['https://a.example', 'https://b.example'], liveRestore: false })
    expect(w.find('[data-test="docker-restart-hint"]').exists()).toBe(true)
  })

  it('zeigt Fehler', async () => {
    vi.mocked(servicesApi.docker).mockResolvedValue({ dataRoot: '/x', logMaxSize: '10m', logMaxFile: 3, registryMirrors: [], liveRestore: false })
    vi.mocked(servicesApi.setDocker).mockRejectedValue(new Error('Ungültige Log-Größe'))
    const w = mount(DockerSettings, { global: { plugins: dockerPlugins() } })
    await flushPromises()
    await w.find('#d-size').setValue('x')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('[data-test="docker-error"]').text()).toContain('Ungültige Log-Größe')
  })
})
