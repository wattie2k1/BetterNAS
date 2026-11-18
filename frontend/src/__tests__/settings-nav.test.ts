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
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', { status: 200 })))
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
    for (const l of ['Netzwerk', 'Benachrichtigungen', 'Sicherung', 'Updates']) expect(nav).toContain(l)
    expect(w.find('[data-test="settings-network"]').exists()).toBe(true)
    vi.unstubAllGlobals()
  })
})
