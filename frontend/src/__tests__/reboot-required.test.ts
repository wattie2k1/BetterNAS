import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import PrimeVue from 'primevue/config'
import JobsPanel from '@/components/JobsPanel.vue'
import { useRebootStore } from '@/stores/reboot'

const required = { required: true, reason: 'Ein neuer Kernel (6.12.48) ist installiert, es läuft noch 6.12.43.', runningKernel: '6.12.43', installedKernel: '6.12.48', packages: ['libc6'] }
const none = { required: false, packages: [] }

function stub(body: unknown) {
  vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => new Response(JSON.stringify(body), { status: 200 })))
}

async function mountPanel() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div/>' } }] })
  const pinia = createPinia()
  setActivePinia(pinia)
  const store = useRebootStore()
  await store.load()
  const w = mount(JobsPanel, { global: { plugins: [pinia, router, PrimeVue] } })
  await flushPromises()
  return { w, store }
}

afterEach(() => vi.unstubAllGlobals())

describe('Neustart erforderlich', () => {
  it('zeigt eine feste Meldung unter Aufträgen, solange ein Neustart nötig ist', async () => {
    stub(required)
    const { w } = await mountPanel()
    const card = w.find('[data-test="reboot-required"]')
    expect(card.exists()).toBe(true)
    expect(card.text()).toContain('Neustart erforderlich')
    expect(card.text()).toContain('6.12.48')
    expect(card.text()).toContain('libc6')
    expect(card.find('[data-test="reboot-now"]').exists()).toBe(true)
  })

  it('zeigt nichts, wenn kein Neustart nötig ist', async () => {
    stub(none)
    const { w } = await mountPanel()
    expect(w.find('[data-test="reboot-required"]').exists()).toBe(false)
  })

  it('verschwindet nach dem Neustart (nächste Abfrage liefert required=false)', async () => {
    stub(required)
    const { w, store } = await mountPanel()
    expect(w.find('[data-test="reboot-required"]').exists()).toBe(true)
    stub(none)
    await store.load()
    await flushPromises()
    expect(w.find('[data-test="reboot-required"]').exists()).toBe(false)
  })

  it('behält die Meldung, wenn die Abfrage fehlschlägt (z. B. beim Neustart der API)', async () => {
    stub(required)
    const { store } = await mountPanel()
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))
    await store.load()
    expect(store.status.required).toBe(true)
  })
})
