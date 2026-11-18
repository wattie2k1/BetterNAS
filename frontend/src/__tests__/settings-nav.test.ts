import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import SettingsView from '@/views/settings/SettingsView.vue'
import NetworkSettings from '@/views/settings/NetworkSettings.vue'

describe('SettingsView', () => {
  it('zeigt die Bereiche und rendert den gewählten', async () => {
    const ip = { interface: 'eno1', bridgeActive: false, config: { mode: 'dhcp', address: '', gateway: '', dns: [], domains: [] }, current: { addresses: [], gateway: '', dns: [], domains: [] }, interfaces: [], pending: false, deadline: 0, hostname: 'betternas' }
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async (url: string) => new Response(JSON.stringify(url.endsWith('/api/v1/network') ? ip : {}), { status: 200 })))
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/einstellungen', component: SettingsView, children: [{ path: 'netzwerk', component: NetworkSettings }] }],
    })
    await router.push('/einstellungen/netzwerk')
    const pinia = createPinia()
    setActivePinia(pinia)
    const w = mount(SettingsView, { global: { plugins: [pinia, router, PrimeVue, ToastService] } })
    await flushPromises()
    const nav = w.find('[data-test="settings-nav"]').text()
    for (const l of ['Allgemein', 'Netzwerk', 'Webzugriff', 'Energie', 'Benachrichtigungen', 'Sicherung', 'Updates']) expect(nav).toContain(l)
    expect(w.find('[data-test="settings-network"]').exists()).toBe(true)
    vi.unstubAllGlobals()
  })
})
