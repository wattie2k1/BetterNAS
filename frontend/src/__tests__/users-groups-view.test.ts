import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import UsersGroupsView from '@/views/users/UsersGroupsView.vue'

afterEach(() => vi.unstubAllGlobals())

async function setup(query = '') {
  vi.stubGlobal('fetch', vi.fn().mockImplementation(() => Promise.resolve(new Response('[]', { status: 200 }))))
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/benutzer', component: UsersGroupsView }] })
  router.push('/benutzer' + query)
  await router.isReady()
  const w = mount(UsersGroupsView, { global: { plugins: [createPinia(), PrimeVue, ToastService, router] } })
  await flushPromises()
  return { w, router }
}

describe('UsersGroupsView', () => {
  it('zeigt standardmäßig die Benutzer mit passender Aktion', async () => {
    const { w } = await setup()
    expect(w.find('[data-test="new-user"]').exists()).toBe(true)
    expect(w.find('[data-test="new-group"]').exists()).toBe(false)
  })

  it('wechselt per Umschalter zu den Gruppen und merkt sich das in der Adresse', async () => {
    const { w, router } = await setup()
    const buttons = w.findAll('[data-test="users-tabs"] [role="radio"], [data-test="users-tabs"] button, [data-test="users-tabs"] .p-togglebutton')
    await buttons[1].trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.tab).toBe('gruppen')
    expect(w.find('[data-test="new-group"]').exists()).toBe(true)
  })

  it('öffnet direkt die Gruppen bei ?tab=gruppen', async () => {
    const { w } = await setup('?tab=gruppen')
    expect(w.find('[data-test="new-group"]').exists()).toBe(true)
  })
})
