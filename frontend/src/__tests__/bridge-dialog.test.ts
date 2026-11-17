import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import BridgeDialog from '@/components/vms/BridgeDialog.vue'
import { networkApi, type BridgeStatus } from '@/api/network'

vi.mock('@/api/network', () => ({ networkApi: { bridge: vi.fn(), enableBridge: vi.fn(), confirmBridge: vi.fn(), disableBridge: vi.fn() } }))

const off: BridgeStatus = { active: false, interface: '', pending: false, deadline: 0, candidates: ['eno1'] }
const mountIt = () => mount(BridgeDialog, { props: { visible: true }, global: { plugins: [PrimeVue, ToastService] }, attachTo: document.body })
const q = (s: string) => document.querySelector<HTMLElement>(`[data-test="${s}"]`)

describe('BridgeDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    document.body.innerHTML = ''
  })

  it('zeigt die Warnung und richtet die Bridge auf der gewählten Schnittstelle ein', async () => {
    vi.mocked(networkApi.bridge).mockResolvedValueOnce(off).mockResolvedValue({ ...off, active: true, pending: true, interface: 'eno1', deadline: Math.floor(Date.now() / 1000) + 100 })
    vi.mocked(networkApi.enableBridge).mockResolvedValue()
    const w = mountIt()
    await flushPromises()
    expect(q('bridge-off')).toBeTruthy()
    q('bridge-enable')!.click()
    await flushPromises()
    expect(networkApi.enableBridge).toHaveBeenCalledWith('eno1')
    expect(q('bridge-pending')).toBeTruthy()
    w.unmount()
  })

  it('bestätigt die Änderung', async () => {
    vi.mocked(networkApi.bridge).mockResolvedValue({ ...off, active: true, pending: true, interface: 'eno1', deadline: Math.floor(Date.now() / 1000) + 100 })
    vi.mocked(networkApi.confirmBridge).mockResolvedValue()
    const w = mountIt()
    await flushPromises()
    q('bridge-confirm')!.click()
    await flushPromises()
    expect(networkApi.confirmBridge).toHaveBeenCalled()
    w.unmount()
  })

  it('zeigt eine bestätigte Bridge mit Zurücknehmen', async () => {
    vi.mocked(networkApi.bridge).mockResolvedValue({ ...off, active: true, interface: 'eno1' })
    const w = mountIt()
    await flushPromises()
    expect(q('bridge-active')).toBeTruthy()
    expect(q('bridge-disable')).toBeTruthy()
    w.unmount()
  })
})
