import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import StacksView from '@/views/docker/StacksView.vue'
import UpdateDialog from '@/components/docker/UpdateDialog.vue'
import RollbackDialog from '@/components/docker/RollbackDialog.vue'
import type { BackupPlan } from '@/api/docker'

vi.mock('@/components/docker/YamlEditor.vue', async () => {
  const { defineComponent, h } = await import('vue')
  return { __esModule: true, default: defineComponent({ props: ['modelValue'], setup: () => () => h('div') }) }
})

const plugins = () => [createPinia(), PrimeVue, ToastService]
afterEach(() => vi.unstubAllGlobals())

const plan: BackupPlan = {
  sources: [
    { kind: 'bind', name: '/srv/appdata/media/config', path: '/srv/appdata/media/config', bytes: 5 * 1024 ** 2 },
    { kind: 'volume', name: 'media_db', path: '/srv/appdata/docker/volumes/media_db/_data', bytes: 20 * 1024 ** 2 },
  ],
  totalBytes: 25 * 1024 ** 2,
  freeBytes: 50 * 1024 ** 3,
  possible: true,
}

describe('UpdateDialog', () => {
  const mountIt = (p: BackupPlan | null) => mount(UpdateDialog, { props: { visible: true, stack: 'media', plan: p }, global: { plugins: plugins() } })

  it('bietet die Sicherung an, schlägt sie vor und liefert die Wahl', async () => {
    const w = mountIt(plan)
    await flushPromises()
    expect(w.find('[data-test="update-sources"]').text()).toContain('/srv/appdata/media/config')
    expect(w.find('[data-test="update-sources"]').text()).toContain('media_db')
    expect(w.text()).toContain('26,2 MB')
    expect((w.find('input[data-test="update-backup"]').element as HTMLInputElement).checked).toBe(true)
    await w.find('button[data-test="update-submit"]').trigger('click')
    expect(w.emitted('confirm')).toEqual([[true]])
    await w.find('input[data-test="update-backup"]').setValue(false)
    await w.find('button[data-test="update-submit"]').trigger('click')
    expect(w.emitted('confirm')![1]).toEqual([false])
  })

  it('sperrt die Sicherung mit Begründung, wenn sie nicht möglich ist', async () => {
    const w = mountIt({ ...plan, possible: false, reason: 'Zu wenig Platz für die Sicherung: benötigt 9 MB' })
    await flushPromises()
    const box = w.find('input[data-test="update-backup"]')
    expect(box.attributes('disabled')).toBeDefined()
    expect((box.element as HTMLInputElement).checked).toBe(false)
    expect(w.find('[data-test="update-reason"]').text()).toContain('Zu wenig Platz')
    await w.find('button[data-test="update-submit"]').trigger('click')
    expect(w.emitted('confirm')).toEqual([[false]])
  })
})

describe('RollbackDialog', () => {
  const stack = { name: 'media', running: 1, total: 1, rollback: '2026-11-18T10:00:00Z', rollbackData: true }
  const mountIt = (s = stack) => mount(RollbackDialog, { props: { visible: true, stack: s }, global: { plugins: plugins() } })

  it('spielt Daten nur auf ausdrücklichen Wunsch zurück und warnt dann', async () => {
    const w = mountIt()
    await flushPromises()
    const box = w.find('input[data-test="rollback-data"]')
    expect((box.element as HTMLInputElement).checked).toBe(false)
    expect(w.find('[data-test="rollback-warning"]').exists()).toBe(false)
    await w.find('button[data-test="rollback-submit"]').trigger('click')
    expect(w.emitted('confirm')).toEqual([[false]])
    await box.setValue(true)
    expect(w.find('[data-test="rollback-warning"]').text()).toContain('gehen verloren')
    await w.find('button[data-test="rollback-submit"]').trigger('click')
    expect(w.emitted('confirm')![1]).toEqual([true])
  })

  it('zeigt ohne Sicherung keine Datenoption', async () => {
    const w = mountIt({ ...stack, rollbackData: false })
    await flushPromises()
    expect(w.find('input[data-test="rollback-data"]').exists()).toBe(false)
  })
})

function stubApi(routes: Record<string, unknown>) {
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url}`
    const v = routes[key]
    if (v === undefined) return Promise.resolve(new Response(JSON.stringify({ error: { code: 'not_found', message: key } }), { status: 404 }))
    return Promise.resolve(new Response(JSON.stringify(v), { status: key.startsWith('POST /api/v1/stacks') ? 202 : 200 }))
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
const base = { 'GET /api/v1/docker/status': { installed: true, active: true, version: '26' }, 'GET /api/v1/containers': [] }

describe('Update und Rollback in der Stack-Ansicht', () => {
  it('fragt bei Stacks mit Daten nach der Sicherung und sendet die Wahl', async () => {
    const fn = stubApi({
      ...base,
      'GET /api/v1/stacks': [{ name: 'media', running: 1, total: 1 }],
      'GET /api/v1/stacks/media/backup-plan': plan,
      'POST /api/v1/stacks/media/update': { jobId: 'j1' },
    })
    const w = mount(StacksView, { global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    await w.find('[data-test="stack-media"] button[aria-label="Aktualisieren"]').trigger('click')
    await flushPromises()
    expect(fn).not.toHaveBeenCalledWith('/api/v1/stacks/media/update', expect.anything())
    ;(document.querySelector('[data-test="update-submit"]') as HTMLElement).click()
    await vi.waitFor(() =>
      expect(fn).toHaveBeenCalledWith('/api/v1/stacks/media/update', expect.objectContaining({ method: 'POST', body: JSON.stringify({ backup: true }) })),
    )
    w.unmount()
  })

  it('aktualisiert Stacks ohne sicherbare Daten sofort', async () => {
    const fn = stubApi({
      ...base,
      'GET /api/v1/stacks': [{ name: 'media', running: 1, total: 1 }],
      'GET /api/v1/stacks/media/backup-plan': { sources: [], totalBytes: 0, freeBytes: 1, possible: false, reason: 'keine Daten' },
      'POST /api/v1/stacks/media/update': { jobId: 'j1' },
    })
    const w = mount(StacksView, { global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    await w.find('[data-test="stack-media"] button[aria-label="Aktualisieren"]').trigger('click')
    await vi.waitFor(() => expect(fn).toHaveBeenCalledWith('/api/v1/stacks/media/update', expect.objectContaining({ method: 'POST', body: undefined })))
    expect(document.querySelector('[data-test="update-submit"]')).toBeNull()
    w.unmount()
  })

  it('sendet beim Rollback mit Daten restoreData', async () => {
    const fn = stubApi({
      ...base,
      'GET /api/v1/stacks': [{ name: 'media', running: 1, total: 1, rollback: '2026-11-18T10:00:00Z', rollbackData: true }],
      'POST /api/v1/stacks/media/rollback': { jobId: 'j2' },
    })
    const w = mount(StacksView, { global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    await w.find('[data-test="stack-media"] button[aria-label="Zurückrollen"]').trigger('click')
    await flushPromises()
    ;(document.querySelector('[data-test="rollback-data"]') as HTMLInputElement).click()
    await flushPromises()
    ;(document.querySelector('[data-test="rollback-submit"]') as HTMLElement).click()
    await vi.waitFor(() =>
      expect(fn).toHaveBeenCalledWith('/api/v1/stacks/media/rollback', expect.objectContaining({ method: 'POST', body: JSON.stringify({ restoreData: true }) })),
    )
    w.unmount()
  })

  it('meldet einen Fehler beim Sicherungsplan und startet nichts', async () => {
    const fn = stubApi({ ...base, 'GET /api/v1/stacks': [{ name: 'media', running: 1, total: 1 }] }) // Plan-Route fehlt → 404
    const w = mount(StacksView, { global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    await w.find('[data-test="stack-media"] button[aria-label="Aktualisieren"]').trigger('click')
    await flushPromises()
    expect(fn).not.toHaveBeenCalledWith('/api/v1/stacks/media/update', expect.anything())
    w.unmount()
  })
})
