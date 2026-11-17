import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import { createMemoryHistory, createRouter } from 'vue-router'
import LoginView from '@/views/LoginView.vue'
import TotpCard from '@/components/users/TotpCard.vue'
import UsersView from '@/views/users/UsersView.vue'
import { useAuthStore } from '@/stores/auth'

vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn().mockResolvedValue('data:image/png;base64,QR') } }))

afterEach(() => vi.unstubAllGlobals())

interface Call { url: string; method: string; body?: string }

/** Antwortet nach (Methode URL); unbekannt → 404. */
function stub(routes: Record<string, () => Response>) {
  const calls: Call[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      calls.push({ url, method, body: init?.body as string | undefined })
      const r = routes[`${method} ${url}`]
      return r ? r() : new Response(JSON.stringify({ error: { code: 'not_found', message: 'x' } }), { status: 404 })
    }),
  )
  return calls
}
const json = (b: unknown, status = 200) => () => new Response(JSON.stringify(b), { status })
const err = (message: string, status = 401) => json({ error: { code: 'unauthorized', message } }, status)

function mountLogin() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/login', component: LoginView }, { path: '/', component: { template: '<div>start</div>' } }],
  })
  const w = mount(LoginView, { global: { plugins: [createPinia(), PrimeVue, router] } })
  return { w, router }
}

async function passwordStep(w: ReturnType<typeof mountLogin>['w']) {
  await w.find('input#username').setValue('anna')
  await w.find('input[type="password"]').setValue('geheim123')
  await w.find('form').trigger('submit')
  await flushPromises()
}

const mfa = json({ mfaRequired: true, mfaToken: 'm1' })

describe('Anmeldung mit zweitem Faktor', () => {
  it('fragt nach dem Code und sendet mfaToken und Code', async () => {
    const calls = stub({
      'POST /api/v1/auth/login': mfa,
      'POST /api/v1/auth/login/totp': json({ username: 'anna', csrfToken: 't', totp: true }),
    })
    const { w, router } = mountLogin()
    await passwordStep(w)
    const input = w.find('input#totp-code')
    expect(input.exists()).toBe(true)
    expect(input.attributes('autocomplete')).toBe('one-time-code')
    await input.setValue('123456')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(JSON.parse(calls.find((c) => c.url.endsWith('/login/totp'))!.body!)).toEqual({ mfaToken: 'm1', code: '123456' })
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('bleibt bei falschem Code auf Schritt 2', async () => {
    stub({ 'POST /api/v1/auth/login': mfa, 'POST /api/v1/auth/login/totp': err('Code ungültig') })
    const { w } = mountLogin()
    await passwordStep(w)
    await w.find('input#totp-code').setValue('000000')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('Code ungültig')
    expect(w.find('input#totp-code').exists()).toBe(true)
  })

  it('wechselt für Wiederherstellungscodes das Feld und nutzt denselben Endpunkt', async () => {
    const calls = stub({
      'POST /api/v1/auth/login': mfa,
      'POST /api/v1/auth/login/totp': json({ username: 'anna', csrfToken: 't', totp: true }),
    })
    const { w } = mountLogin()
    await passwordStep(w)
    await w.find('[data-test="use-recovery"]').trigger('click')
    expect(w.text()).toContain('Wiederherstellungscode')
    await w.find('input#totp-code').setValue('ABCDE-FGHIJ')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(JSON.parse(calls.find((c) => c.url.endsWith('/login/totp'))!.body!)).toEqual({ mfaToken: 'm1', code: 'ABCDE-FGHIJ' })
  })

  it('geht bei abgelaufener Anmeldung zurück zu Schritt 1', async () => {
    stub({ 'POST /api/v1/auth/login': mfa, 'POST /api/v1/auth/login/totp': err('Zu viele falsche Codes. Bitte erneut anmelden.') })
    const { w } = mountLogin()
    await passwordStep(w)
    await w.find('input#totp-code').setValue('000000')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('input#totp-code').exists()).toBe(false)
    expect(w.find('input#username').exists()).toBe(true)
    expect(w.text()).toContain('erneut anmelden')
  })

  it('„Zurück“ führt zum Passwortschritt', async () => {
    stub({ 'POST /api/v1/auth/login': mfa })
    const { w } = mountLogin()
    await passwordStep(w)
    await w.find('[data-test="totp-back"]').trigger('click')
    expect(w.find('input#username').exists()).toBe(true)
  })
})

const mountCard = () => mount(TotpCard, { global: { plugins: [createPinia(), PrimeVue, ToastService] } })
const codes = Array.from({ length: 10 }, (_, i) => `AAAA${i}-BBBB${i}`)

describe('TotpCard', () => {
  it('richtet 2FA ein: Geheimnis und QR, Bestätigung nur mit 6 Ziffern, Codes einmalig anzeigen', async () => {
    const calls = stub({
      'GET /api/v1/auth/totp': json({ enabled: false }),
      'POST /api/v1/auth/totp/setup': json({ secret: 'JBSWY3DPEHPK3PXP', uri: 'otpauth://totp/BetterNAS:anna?secret=JBSWY3DPEHPK3PXP' }),
      'POST /api/v1/auth/totp/enable': json({ recoveryCodes: codes }),
    })
    const w = mountCard()
    await flushPromises()
    await w.find('[data-test="totp-setup"]').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('JBSWY3DPEHPK3PXP')
    expect(w.find('img[data-test="totp-qr"]').attributes('src')).toContain('data:image/png')
    const btn = () => w.find('[data-test="totp-enable"]')
    expect(btn().attributes('disabled')).toBeDefined()
    await w.find('input#totp-confirm').setValue('12345')
    expect(btn().attributes('disabled')).toBeDefined()
    await w.find('input#totp-confirm').setValue('123456')
    expect(btn().attributes('disabled')).toBeUndefined()
    await btn().trigger('click')
    await flushPromises()
    expect(JSON.parse(calls.find((c) => c.url.endsWith('/totp/enable'))!.body!)).toEqual({ code: '123456' })
    expect(w.findAll('[data-test="recovery-code"]')).toHaveLength(10)
    expect(w.text()).toContain('nicht mehr')
    expect(w.find('[data-test="codes-copy"]').exists()).toBe(true)
  })

  it('schaltet mit Code ab', async () => {
    const calls = stub({
      'GET /api/v1/auth/totp': json({ enabled: true }),
      'POST /api/v1/auth/totp/disable': () => new Response(null, { status: 204 }),
    })
    const w = mountCard()
    await flushPromises()
    expect(w.text()).toContain('aktiv')
    await w.find('[data-test="totp-disable-open"]').trigger('click')
    const submit = () => w.find('[data-test="totp-disable"]')
    expect(submit().attributes('disabled')).toBeDefined()
    await w.find('input#totp-disable-code').setValue('654321')
    await submit().trigger('click')
    await flushPromises()
    expect(JSON.parse(calls.find((c) => c.url.endsWith('/totp/disable'))!.body!)).toEqual({ code: '654321' })
  })

  it('zeigt Fehler der Bestätigung', async () => {
    stub({
      'GET /api/v1/auth/totp': json({ enabled: false }),
      'POST /api/v1/auth/totp/setup': json({ secret: 'ABC', uri: 'otpauth://x' }),
      'POST /api/v1/auth/totp/enable': json({ error: { code: 'invalid_params', message: 'Der Code stimmt nicht.' } }, 400),
    })
    const w = mountCard()
    await flushPromises()
    await w.find('[data-test="totp-setup"]').trigger('click')
    await flushPromises()
    await w.find('input#totp-confirm').setValue('111111')
    await w.find('[data-test="totp-enable"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="totp-error"]').text()).toContain('Der Code stimmt nicht')
    expect(w.findAll('[data-test="recovery-code"]')).toHaveLength(0)
  })
})

describe('Benutzerliste: 2FA zurücksetzen', () => {
  const users = [
    { name: 'wattie', fullName: '', uid: 1000, groups: [], admin: true, system: true },
    { name: 'anna', fullName: '', uid: 10000, groups: [], admin: true, system: false },
  ]
  function mountUsers() {
    const calls = stub({
      'GET /api/v1/users': json(users),
      'GET /api/v1/groups': json([]),
      'DELETE /api/v1/users/anna/totp': () => new Response(null, { status: 204 }),
    })
    const pinia = createPinia()
    setActivePinia(pinia)
    useAuthStore().user = 'wattie'
    return { calls, w: mount(UsersView, { global: { plugins: [pinia, PrimeVue, ToastService] } }) }
  }

  it('sendet DELETE erst nach Bestätigung und bietet die Aktion für sich selbst nicht an', async () => {
    const { calls, w } = mountUsers()
    await flushPromises()
    expect(w.find('[data-test="user-wattie"] [data-test="user-reset-2fa"]').attributes('disabled')).toBeDefined()
    await w.find('[data-test="user-anna"] [data-test="user-reset-2fa"]').trigger('click')
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
    await w.find('[data-test="confirm-submit"]').trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.method === 'DELETE' && c.url === '/api/v1/users/anna/totp')).toBe(true)
  })
})
