import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import BackupView from '@/views/system/BackupView.vue'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

const manifest = (v: string) => ({ format: 1, createdAt: '2026-11-17T02:30:00Z', version: v, schemaVersion: 5, host: 'nas', counts: { pools: 2, shares: 3, stacks: 1, disks: 4 } })
const list = [
  { name: 'betternas-backup-20260617-023000.tar.gz', size: 120000, manifest: manifest('0.5.0') },
  { name: 'betternas-backup-20260616-023000.tar.gz', size: 118000, manifest: manifest('0.4.0') },
]
const preview = { token: 'tok123', manifest: manifest('0.4.0'), expires: '2026-11-17T12:00:00Z', warnings: ['Das Backup stammt von BetterNAS 0.4.0, hier läuft 0.5.0.'] }

interface Call { url: string; method: string; body?: unknown; headers?: Record<string, string> }

function stub(over: { previewResponse?: Response } = {}) {
  const calls: Call[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      const headers: Record<string, string> = {}
      new Headers(init?.headers).forEach((v, k) => (headers[k] = v))
      calls.push({ url, method, body: init?.body, headers })
      const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status })
      if (url === '/api/v1/backups' && method === 'GET') return json(list)
      if (url === '/api/v1/backups' && method === 'POST') return json(list[0], 201)
      if (url.endsWith('/restore/preview')) return over.previewResponse ?? json(preview)
      if (url === '/api/v1/backups/restore') return json({ status: 'restarting' }, 202)
      if (method === 'DELETE') return new Response(null, { status: 204 })
      if (url === '/api/v1/system/info') throw new TypeError('Failed to fetch') // API startet neu
      return json({}, 404)
    }),
  )
  return calls
}

const mountView = () => mount(BackupView, { global: { plugins: [createPinia(), PrimeVue, ToastService] } })

async function pickFile(w: ReturnType<typeof mountView>) {
  const input = w.find('input[data-test="backup-file"]')
  const file = new File([new Uint8Array([31, 139, 8])], 'mein-backup.tar.gz', { type: 'application/gzip' })
  Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
  await input.trigger('change')
  await flushPromises()
  return file
}

describe('Sicherung', () => {
  it('listet Sicherungen mit Größe, Version und Inhalt', async () => {
    stub()
    const w = mountView()
    await flushPromises()
    const rows = w.findAll('[data-test="backup-row"]')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('0.5.0')
    expect(w.text()).toContain('Zugangsdaten') // Hinweis: Backup enthält Mail-/ntfy-Zugangsdaten
    expect(rows[0].text()).toContain('2 Pools')
    expect(rows[0].text()).toContain('3 Freigaben')
    expect(rows[0].find('a[download]').attributes('href')).toBe('/api/v1/backups/betternas-backup-20260617-023000.tar.gz')
  })

  it('legt per Knopf eine Sicherung an und lädt die Liste neu', async () => {
    const calls = stub()
    const w = mountView()
    await flushPromises()
    await w.find('[data-test="backup-create"]').trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.method === 'POST' && c.url === '/api/v1/backups')).toBe(true)
    expect(calls.filter((c) => c.method === 'GET' && c.url === '/api/v1/backups').length).toBeGreaterThanOrEqual(2)
  })

  it('löscht erst nach Bestätigung', async () => {
    const calls = stub()
    const w = mountView()
    await flushPromises()
    await w.find('[data-test="backup-delete"]').trigger('click')
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
    await w.find('[data-test="confirm-submit"]').trigger('click')
    await flushPromises()
    expect(calls.find((c) => c.method === 'DELETE')!.url).toBe('/api/v1/backups/betternas-backup-20260617-023000.tar.gz')
  })

  it('zeigt nach dem Hochladen die Vorschau; Wiederherstellen erst nach exakt getipptem Wort', async () => {
    vi.useFakeTimers()
    const calls = stub()
    const w = mountView()
    await flushPromises()
    const file = await pickFile(w)
    const up = calls.find((c) => c.url === '/api/v1/backups/restore/preview')!
    expect(up.body).toBe(file)
    expect(up.headers!['content-type']).toBe('application/gzip')
    expect(w.text()).toContain('2 Pools')
    expect(w.text()).toContain('hier läuft 0.5.0')
    expect(w.text()).toContain('Benutzerkonten')
    const btn = () => w.find('[data-test="restore-submit"]')
    expect(btn().attributes('disabled')).toBeDefined()
    await w.find('input#restore-confirm').setValue('wiederherstellen')
    expect(btn().attributes('disabled')).toBeDefined()
    await w.find('input#restore-confirm').setValue('WIEDERHERSTELLEN')
    expect(btn().attributes('disabled')).toBeUndefined()
    await btn().trigger('click')
    await flushPromises()
    const r = calls.find((c) => c.url === '/api/v1/backups/restore')!
    expect(JSON.parse(r.body as string)).toEqual({ token: 'tok123' })
    expect(w.find('[data-test="restarting"]').exists()).toBe(true)
    w.unmount()
  })

  it('zeigt Fehler der Vorschau als Text und öffnet keinen Dialog', async () => {
    stub({ previewResponse: new Response(JSON.stringify({ error: { code: 'invalid_params', message: 'Das ist kein gültiges BetterNAS-Backup (kein .tar.gz).' } }), { status: 400 }) })
    const w = mountView()
    await flushPromises()
    await pickFile(w)
    expect(w.find('[data-test="backup-error"]').text()).toContain('kein gültiges BetterNAS-Backup')
    expect(w.find('input#restore-confirm').exists()).toBe(false)
  })

  it('stellt aus der Liste wieder her (Vorschau des gespeicherten Backups)', async () => {
    const calls = stub()
    const w = mountView()
    await flushPromises()
    await w.findAll('[data-test="backup-restore"]')[1].trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.url === '/api/v1/backups/betternas-backup-20260616-023000.tar.gz/restore/preview' && c.method === 'POST')).toBe(true)
    expect(w.find('input#restore-confirm').exists()).toBe(true)
  })
})
