import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import { createMemoryHistory, createRouter } from 'vue-router'
import LoginView from '@/views/LoginView.vue'
import { useSetupStore } from '@/stores/setup'
import { safeNext } from '@/utils/redirect'

afterEach(() => vi.unstubAllGlobals())

const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))

describe('Einrichtungsassistent: Weiterleitung', () => {
  it('leitet bei nicht abgeschlossener Einrichtung einmal um', async () => {
    setActivePinia(createPinia())
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => json({ completed: false, hostname: 'nas' })))
    const s = useSetupStore()
    expect(await s.firstRunRedirect('/')).toBe('/einrichtung')
    expect(s.hostname).toBe('nas')
    // Danach darf der Nutzer frei navigieren.
    expect(await s.firstRunRedirect('/freigaben')).toBeNull()
  })

  it('leitet nicht um, wenn die Einrichtung abgeschlossen ist', async () => {
    setActivePinia(createPinia())
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => json({ completed: true, hostname: 'nas' })))
    expect(await useSetupStore().firstRunRedirect('/')).toBeNull()
  })

  it('leitet nicht um, wenn der Assistent schon geöffnet ist', async () => {
    setActivePinia(createPinia())
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => json({ completed: false, hostname: 'nas' })))
    expect(await useSetupStore().firstRunRedirect('/einrichtung')).toBeNull()
  })

  it('blockiert die Navigation nicht, wenn der Status nicht ladbar ist', async () => {
    setActivePinia(createPinia())
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))
    expect(await useSetupStore().firstRunRedirect('/')).toBeNull()
  })

  it('merkt sich den Abschluss', async () => {
    setActivePinia(createPinia())
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(new Response(null, { status: 204 })))
    vi.stubGlobal('fetch', fetchMock)
    const s = useSetupStore()
    await s.complete()
    expect(s.completed).toBe(true)
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/setup/complete')
    expect(await s.firstRunRedirect('/')).toBeNull()
  })
})

describe('Login: Weiterleitungsziel', () => {
  it('erlaubt nur Pfade innerhalb der App', () => {
    expect(safeNext('/freigaben')).toBe('/freigaben')
    expect(safeNext('/speicher/pools?x=1')).toBe('/speicher/pools?x=1')
    for (const bad of ['//evil.example', '/\\evil.example', 'https://evil.example', 'evil', '', undefined, ['/a']]) {
      expect(safeNext(bad)).toBe('/')
    }
  })

  it('LoginView folgt einem fremden Ziel nicht', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/login', component: LoginView },
        { path: '/', component: { template: '<div>start</div>' } },
        { path: '/:pathMatch(.*)*', component: { template: '<div>x</div>' } },
      ],
    })
    await router.push('/login?next=//evil.example')
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => json({ username: 'anna', csrfToken: 't' })))
    const w = mount(LoginView, { global: { plugins: [createPinia(), PrimeVue, router] } })
    await w.find('input#username').setValue('anna')
    await w.find('input[type="password"]').setValue('geheim123')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/')
  })
})

describe('SetupView', () => {
  it('zeigt die vier Schritte und sperrt „Weiter“ ohne Datenplatte', async () => {
    const { default: SetupView } = await import('@/views/SetupView.vue')
    const ToastService = (await import('primevue/toastservice')).default
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => json([])))
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div/>' } }] })
    const w = mount(SetupView, { global: { plugins: [createPinia(), PrimeVue, ToastService, router] } })
    await flushPromises()
    for (const label of ['Datenplatten', 'Speicherpool', 'Freigabe', 'Fertig']) expect(w.text()).toContain(label)
    expect(w.find('button[data-test="step-1-next"]').attributes('disabled')).toBeDefined()
    expect(w.find('button[data-test="setup-skip"]').exists()).toBe(true)
  })
})
