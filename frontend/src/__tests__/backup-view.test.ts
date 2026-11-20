import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import BackupView from '@/views/system/BackupView.vue'
import FolderPicker from '@/components/docker/FolderPicker.vue'
import { useJobsStore } from '@/stores/jobs'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

const manifest = (v: string) => ({ format: 1, createdAt: '2026-11-17T02:30:00Z', version: v, schemaVersion: 5, host: 'nas', counts: { pools: 2, shares: 3, stacks: 1, disks: 4 } })
const list = [
  { name: 'betternas-backup-20260617-023000.tar.gz', kind: 'config', size: 120000, manifest: manifest('0.5.0') },
  { name: 'betternas-backup-20260616-023000.tar.gz', kind: 'config', size: 118000, manifest: manifest('0.4.0') },
]
const preview = { token: 'tok123', manifest: manifest('0.4.0'), expires: '2026-11-17T12:00:00Z', warnings: ['Das Backup stammt von BetterNAS 0.4.0, hier läuft 0.5.0.'] }

interface Call { url: string; method: string; body?: unknown; headers?: Record<string, string> }

function stub(over: { previewResponse?: Response; config?: object; status?: object; list?: object[] } = {}) {
  const calls: Call[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      const headers: Record<string, string> = {}
      new Headers(init?.headers).forEach((v, k) => (headers[k] = v))
      calls.push({ url, method, body: init?.body, headers })
      const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status })
      if (url === '/api/v1/backups/config' && method === 'GET') return json(over.config ?? { enabled: true, time: '02:30', keep: 7, dir: '', appdata: false })
      if (url === '/api/v1/backups/status') return json(over.status ?? { dir: '/var/lib/betternas/backups', default: true, appdata: '/srv/appdata', free: 5e9, problem: '' })
      if (url === '/api/v1/backups/config' && method === 'PUT') return json(JSON.parse(init!.body as string))
      if (url === '/api/v1/backups' && method === 'GET') return json(over.list ?? list)
      if (url === '/api/v1/backups/appdata' && method === 'POST') return json({ jobId: 'j1' }, 202)
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

  it('zeigt die Einstellungen und speichert Uhrzeit und Anzahl', async () => {
    const calls = stub()
    const w = mountView()
    await flushPromises()
    expect((w.find('[data-test="cfg-time"]').element as HTMLInputElement).value).toBe('02:30')
    expect((w.find('[data-test="cfg-keep"]').element as HTMLInputElement).value).toBe('7')
    expect(w.find('[data-test="cfg-save"]').attributes('disabled')).toBeDefined() // unverändert
    await w.find('[data-test="cfg-time"]').setValue('04:15')
    await w.find('[data-test="cfg-keep"]').setValue('14')
    await w.find('[data-test="cfg-save"]').trigger('click')
    await flushPromises()
    const put = calls.find((c) => c.method === 'PUT' && c.url === '/api/v1/backups/config')
    expect(JSON.parse(put!.body as string)).toEqual({ enabled: true, time: '04:15', keep: 14, dir: '', appdata: false })
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

  it('wählt einen eigenen Ordner und speichert ihn', async () => {
    const calls = stub()
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-test="loc-dir"]').text()).toBe('Systemplatte (Standard)')
    expect(w.find('[data-test="loc-free"]').text()).toContain('GB')
    await w.find('[data-test="loc-pick"]').trigger('click')
    expect(w.findComponent(FolderPicker).props('visible')).toBe(true)
    w.findComponent(FolderPicker).vm.$emit('select', '/mnt/ssd/sicherung')
    await flushPromises()
    expect(w.find('[data-test="loc-dir"]').text()).toBe('/mnt/ssd/sicherung')
    await w.find('[data-test="cfg-save"]').trigger('click')
    await flushPromises()
    const put = calls.find((c) => c.method === 'PUT' && c.url === '/api/v1/backups/config')
    expect(JSON.parse(put!.body as string)).toMatchObject({ dir: '/mnt/ssd/sicherung', appdata: false })
    // zurück auf die Systemplatte
    await w.find('[data-test="loc-reset"]').trigger('click')
    expect(w.find('[data-test="loc-dir"]').text()).toBe('Systemplatte (Standard)')
  })

  it('zeigt Probleme des Sicherungsorts', async () => {
    stub({ status: { dir: '/mnt/weg/b', default: false, appdata: '/srv/appdata', free: 0, problem: 'Der Ordner für die Sicherungen wurde nicht gefunden.' } })
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-test="loc-problem"]').text()).toContain('nicht gefunden')
    expect(w.text()).toContain('/mnt/weg/b')
  })

  it('schaltet appdata ein und speichert es', async () => {
    const calls = stub()
    const w = mountView()
    await flushPromises()
    const box = w.find('[data-test="cfg-appdata"]')
    expect(box.attributes('disabled')).toBeUndefined()
    expect(w.find('[data-test="appdata-create"]').exists()).toBe(false)
    await box.setValue(true)
    await w.find('[data-test="cfg-save"]').trigger('click')
    await flushPromises()
    const put = calls.find((c) => c.method === 'PUT' && c.url === '/api/v1/backups/config')
    expect(JSON.parse(put!.body as string).appdata).toBe(true)
    expect(w.find('[data-test="appdata-create"]').exists()).toBe(true)
  })

  it('sperrt appdata, solange Docker nicht eingerichtet ist', async () => {
    stub({ status: { dir: '/var/lib/betternas/backups', default: true, appdata: '', free: 1e9, problem: '' } })
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-test="cfg-appdata"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="appdata-hint"]').text()).toContain('Docker eingerichtet')
  })

  it('sichert appdata per Knopf als Auftrag und lädt danach die Liste', async () => {
    const calls = stub({ config: { enabled: true, time: '02:30', keep: 7, dir: '', appdata: true } })
    const pinia = createPinia()
    const w = mount(BackupView, { global: { plugins: [pinia, PrimeVue, ToastService] } })
    await flushPromises()
    await w.find('[data-test="appdata-create"]').trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.method === 'POST' && c.url === '/api/v1/backups/appdata')).toBe(true)
    const before = calls.filter((c) => c.method === 'GET' && c.url === '/api/v1/backups').length
    useJobsStore(pinia).update({ id: 'j1', kind: 'backup.appdata', target: 'appdata', status: 'succeeded', progress: 100, log: '', startedBy: 'a', startedAt: '2026-11-20T02:00:00Z' })
    await flushPromises()
    expect(calls.filter((c) => c.method === 'GET' && c.url === '/api/v1/backups').length).toBe(before + 1)
  })

  it('zeigt appdata-Sicherungen ohne Wiederherstellen-Knopf, aber mit Hinweis', async () => {
    stub({
      list: [
        { name: 'betternas-appdata-20261120-023000.tar.gz', kind: 'appdata', size: 5000000, manifest: { createdAt: '2026-11-20T02:30:00Z' } },
        ...list,
      ],
    })
    const w = mountView()
    await flushPromises()
    const rows = w.findAll('[data-test="backup-row"]')
    expect(rows).toHaveLength(3)
    expect(rows[0].find('[data-test="backup-kind"]').text()).toBe('appdata')
    expect(rows[0].find('[data-test="backup-restore"]').exists()).toBe(false)
    expect(rows[0].find('a[download]').attributes('href')).toBe('/api/v1/backups/betternas-appdata-20261120-023000.tar.gz')
    expect(rows[1].find('[data-test="backup-restore"]').exists()).toBe(true)
    expect(w.find('[data-test="appdata-restore-hint"]').text()).toContain('tar -xzpf')
  })
})
