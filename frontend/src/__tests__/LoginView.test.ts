import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import { createMemoryHistory, createRouter } from 'vue-router'
import LoginView from '@/views/LoginView.vue'

function setup() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: LoginView },
      { path: '/', component: { template: '<div>start</div>' } },
    ],
  })
  const wrapper = mount(LoginView, { global: { plugins: [createPinia(), PrimeVue, router] } })
  return { wrapper, router }
}

afterEach(() => vi.unstubAllGlobals())

describe('LoginView', () => {
  it('zeigt die Fehlermeldung des Servers', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: { code: 'unauthorized', message: 'Benutzername oder Passwort falsch' } }), {
          status: 401,
        }),
      ),
    )
    const { wrapper } = setup()
    await wrapper.find('input#username').setValue('wattie')
    await wrapper.find('input[type="password"]').setValue('falsch')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Benutzername oder Passwort falsch')
  })

  it('deaktiviert den Button während der Anfrage', async () => {
    let resolve!: (r: Response) => void
    vi.stubGlobal('fetch', vi.fn().mockReturnValue(new Promise<Response>((r) => (resolve = r))))
    const { wrapper } = setup()
    await wrapper.find('input#username').setValue('wattie')
    await wrapper.find('input[type="password"]').setValue('richtig1')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('button[type="submit"]').attributes('disabled')).toBeDefined()
    resolve(new Response(JSON.stringify({ username: 'wattie', csrfToken: 't' }), { status: 200 }))
    await flushPromises()
  })
})
