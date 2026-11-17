import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import NotificationsView from '@/views/system/NotificationsView.vue'

afterEach(() => vi.unstubAllGlobals())

const config = {
  mail: { enabled: true, host: 'smtp.example.org', port: 587, security: 'starttls', username: 'nas', passwordSet: true, from: 'nas@example.org', to: ['a@example.org', 'b@example.org'] },
  ntfy: { enabled: false, server: 'https://ntfy.sh', topic: '', tokenSet: false },
  tempWarnC: 55,
}
const history = [
  { id: 2, ts: '2026-11-17T10:00:00Z', level: 'crit', title: 'SMART-Fehler: HDD1', body: 'b', results: [{ channel: 'mail', ok: true }, { channel: 'ntfy', ok: false, error: 'ntfy: HTTP 401' }] },
]
const alerts = [{ key: 'smart:d1', level: 'crit', title: 'SMART-Fehler: HDD1', body: 'Platte tauschen', since: '2026-11-17T10:00:00Z' }]

function stub(over: { testResponse?: Response } = {}) {
  const calls: { url: string; method: string; body?: string }[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      calls.push({ url, method, body: init?.body as string | undefined })
      const ok = (b: unknown) => new Response(JSON.stringify(b), { status: 200 })
      if (url === '/api/v1/notifications/config') return ok(config)
      if (url.startsWith('/api/v1/notifications/history')) return ok(history)
      if (url === '/api/v1/alerts') return ok(alerts)
      if (url === '/api/v1/notifications/test') return over.testResponse ?? new Response(null, { status: 204 })
      return new Response('{}', { status: 404 })
    }),
  )
  return calls
}

const mountView = () => mount(NotificationsView, { global: { plugins: [createPinia(), PrimeVue, ToastService] } })

describe('Benachrichtigungen', () => {
  it('zeigt das Passwort nie an und sendet beim Speichern ein leeres Passwort (= behalten)', async () => {
    const calls = stub()
    const w = mountView()
    await flushPromises()
    const pw = w.find('input#mail-password')
    expect((pw.element as HTMLInputElement).value).toBe('')
    expect(pw.attributes('placeholder')).toContain('gespeichert')
    await w.find('input#mail-host').setValue('smtp2.example.org')
    await w.find('[data-test="notify-save"]').trigger('click')
    await flushPromises()
    const put = calls.find((c) => c.method === 'PUT')!
    const body = JSON.parse(put.body!)
    expect(body.mail.password).toBe('')
    expect(body.mail.host).toBe('smtp2.example.org')
    expect(body.mail.to).toEqual(['a@example.org', 'b@example.org'])
    expect(body.tempWarnC).toBe(55)
  })

  it('sendet die Testnachricht und zeigt den Fehlertext der API', async () => {
    const calls = stub({
      testResponse: new Response(JSON.stringify({ error: { code: 'conflict', message: 'Mail: dial tcp: Verbindung abgelehnt' } }), { status: 409 }),
    })
    const w = mountView()
    await flushPromises()
    await w.find('[data-test="mail-test"]').trigger('click')
    await flushPromises()
    const t = calls.find((c) => c.url === '/api/v1/notifications/test')!
    expect(JSON.parse(t.body!)).toEqual({ channel: 'mail' })
    expect(w.find('[data-test="mail-test-result"]').text()).toContain('Verbindung abgelehnt')
  })

  it('deaktiviert die Testnachricht bei ungespeicherten Änderungen', async () => {
    stub()
    const w = mountView()
    await flushPromises()
    const btn = () => w.find('[data-test="mail-test"]')
    expect(btn().attributes('disabled')).toBeUndefined()
    await w.find('input#mail-host').setValue('anders.example.org')
    expect(btn().attributes('disabled')).toBeDefined()
  })

  it('zeigt aktive Meldungen und den Verlauf mit Ergebnis je Kanal', async () => {
    stub()
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-test="alert-item"]').text()).toContain('SMART-Fehler: HDD1')
    const row = w.find('[data-test="history-row"]')
    expect(row.text()).toContain('SMART-Fehler: HDD1')
    expect(row.find('[data-test="result-mail"]').attributes('data-ok')).toBe('true')
    expect(row.find('[data-test="result-ntfy"]').attributes('data-ok')).toBe('false')
    expect(row.find('[data-test="result-ntfy"]').attributes('title')).toContain('HTTP 401')
  })
})
