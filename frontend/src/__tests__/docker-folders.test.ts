import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import FolderPicker from '@/components/docker/FolderPicker.vue'
import TemplateDialog from '@/components/docker/TemplateDialog.vue'
import StackDialog from '@/components/docker/StackDialog.vue'

vi.mock('@/components/docker/YamlEditor.vue', async () => {
  const { defineComponent, h } = await import('vue')
  return {
    __esModule: true,
    default: defineComponent({
      props: ['modelValue'],
      emits: ['update:modelValue'],
      setup: (props, { emit }) => () =>
        h('textarea', { 'data-test': 'compose', value: props.modelValue, onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLTextAreaElement).value) }),
    }),
  }
})

const plugins = () => [createPinia(), PrimeVue, ToastService]
afterEach(() => vi.unstubAllGlobals())

const tree: Record<string, { path: string; parent: string; dirs: { name: string; path: string }[] }> = {
  '': { path: '', parent: '', dirs: [{ name: 'pools', path: '/srv/pools' }, { name: 'appdata', path: '/srv/appdata' }] },
  '/srv/pools': { path: '/srv/pools', parent: '', dirs: [{ name: 'tank', path: '/srv/pools/tank' }] },
  '/srv/pools/tank': { path: '/srv/pools/tank', parent: '/srv/pools', dirs: [{ name: 'Medien', path: '/srv/pools/tank/Medien' }] },
  '/srv/pools/tank/Medien': { path: '/srv/pools/tank/Medien', parent: '/srv/pools/tank', dirs: [] },
}

function stubFolders() {
  const fn = vi.fn().mockImplementation((url: string) => {
    const p = decodeURIComponent(new URL(url, 'http://x').searchParams.get('path') ?? '')
    const v = tree[p]
    if (!v) return Promise.resolve(new Response(JSON.stringify({ error: { code: 'not_found', message: 'Ordner nicht gefunden' } }), { status: 404 }))
    return Promise.resolve(new Response(JSON.stringify(v), { status: 200 }))
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

describe('FolderPicker', () => {
  const mountPicker = (props = {}) => mount(FolderPicker, { props: { visible: true, ...props }, global: { plugins: plugins() } })

  it('beginnt in der Übersicht, navigiert und liefert den gewählten Ordner', async () => {
    stubFolders()
    const w = mountPicker()
    await flushPromises()
    expect(w.find('[data-test="folder-current"]').text()).toBe('Bereich wählen')
    expect(w.find('[data-test="folder-pools"]').text()).toContain('Speicherpools')
    expect(w.find('button[data-test="folder-select"]').attributes('disabled')).toBeDefined()
    await w.find('[data-test="folder-pools"]').trigger('click')
    await flushPromises()
    await w.find('[data-test="folder-tank"]').trigger('click')
    await flushPromises()
    await w.find('[data-test="folder-Medien"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="folder-current"]').text()).toBe('/srv/pools/tank/Medien')
    expect(w.text()).toContain('Keine Unterordner')
    await w.find('button[data-test="folder-up"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="folder-current"]').text()).toBe('/srv/pools/tank')
    await w.find('button[data-test="folder-select"]').trigger('click')
    expect(w.emitted('select')).toEqual([['/srv/pools/tank']])
  })

  it('startet beim bisherigen Ordner und fällt bei einem fehlenden auf die Übersicht zurück', async () => {
    const fn = stubFolders()
    const w = mountPicker({ start: '/srv/pools/tank/' })
    await flushPromises()
    expect(w.find('[data-test="folder-current"]').text()).toBe('/srv/pools/tank')
    expect(fn).toHaveBeenCalledWith('/api/v1/docker/folders?path=%2Fsrv%2Fpools%2Ftank', expect.anything())
    const w2 = mountPicker({ start: '/srv/pools/weg' })
    await flushPromises()
    expect(w2.find('[data-test="folder-current"]').text()).toBe('Bereich wählen')
    // Pfade außerhalb der Bereiche werden gar nicht erst angefragt.
    fn.mockClear()
    mountPicker({ start: '/etc' })
    await flushPromises()
    expect(fn).toHaveBeenCalledTimes(1)
    expect(fn).toHaveBeenCalledWith('/api/v1/docker/folders?path=', expect.anything())
  })
})

describe('Ordner in Vorlagen und Editor', () => {
  it('übernimmt den gewählten Ordner in das Pfadfeld der Vorlage', async () => {
    stubFolders()
    const w = mount(TemplateDialog, { props: { visible: true, existing: [] }, global: { plugins: plugins() } })
    await flushPromises()
    await w.find('[data-test="template-jellyfin"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="tpl-browse-PORT"]').exists()).toBe(false)
    await w.find('[data-test="tpl-browse-MEDIA_DIR"]').trigger('click')
    await flushPromises()
    // Der Standardwert /srv/pools ist der Startpunkt der Auswahl.
    expect(w.find('[data-test="folder-current"]').text()).toBe('/srv/pools')
    await w.find('[data-test="folder-tank"]').trigger('click')
    await flushPromises()
    await w.find('button[data-test="folder-select"]').trigger('click')
    await flushPromises()
    expect((w.find('input[data-test="tpl-field-MEDIA_DIR"]').element as HTMLInputElement).value).toBe('/srv/pools/tank')
    // Andere Pfadfelder bleiben unberührt.
    expect((w.find('input[data-test="tpl-field-CONFIG_DIR"]').element as HTMLInputElement).value).toBe('/srv/appdata/jellyfin/config')
  })

  it('fügt einen Ordner als Bind-Mount in die compose.yaml ein', async () => {
    stubFolders()
    const w = mount(StackDialog, { props: { visible: true, stack: { name: 'a', compose: 'services: {}\n', env: '' }, existing: [] }, global: { plugins: plugins() } })
    await flushPromises()
    await w.find('button[data-test="stack-insert-folder"]').trigger('click')
    await flushPromises()
    await w.find('[data-test="folder-pools"]').trigger('click')
    await flushPromises()
    await w.find('button[data-test="folder-select"]').trigger('click')
    await flushPromises()
    expect((w.find('textarea[data-test="compose"]').element as HTMLTextAreaElement).value).toContain('/srv/pools:/pfad-im-container')
    expect(w.find('[data-test="stack-insert-hint"]').text()).toContain('pfad-im-container')
  })
})

describe('Neuer Ordner im Picker', () => {
  const created = { path: '/srv/pools/tank/Fotos', parent: '/srv/pools/tank', dirs: [] as { name: string; path: string }[] }

  function stubCreate(post: () => Response) {
    const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (init?.method === 'POST') return Promise.resolve(post())
      const p = decodeURIComponent(new URL(url, 'http://x').searchParams.get('path') ?? '')
      const v = p === '/srv/pools/tank/Fotos' ? created : tree[p]
      return Promise.resolve(v ? new Response(JSON.stringify(v), { status: 200 }) : new Response(JSON.stringify({ error: { code: 'not_found', message: 'weg' } }), { status: 404 }))
    })
    vi.stubGlobal('fetch', fn)
    return fn
  }
  const mountAt = async (start: string) => {
    const w = mount(FolderPicker, { props: { visible: true, start }, global: { plugins: plugins() } })
    await flushPromises()
    return w
  }

  it('ist in der Übersicht und direkt unter /srv/pools gesperrt, in einem Pool möglich', async () => {
    stubCreate(() => new Response('{}', { status: 201 }))
    expect((await mountAt('')).find('button[data-test="folder-new"]').attributes('disabled')).toBeDefined()
    expect((await mountAt('/srv/pools')).find('button[data-test="folder-new"]').attributes('disabled')).toBeDefined()
    expect((await mountAt('/srv/pools/tank')).find('button[data-test="folder-new"]').attributes('disabled')).toBeUndefined()
  })

  it('legt den Ordner an und wechselt hinein', async () => {
    const fn = stubCreate(() => new Response(JSON.stringify({ name: 'Fotos', path: '/srv/pools/tank/Fotos' }), { status: 201 }))
    const w = await mountAt('/srv/pools/tank')
    await w.find('button[data-test="folder-new"]').trigger('click')
    await w.find('input[data-test="folder-name"]').setValue('Fotos')
    await w.find('button[data-test="folder-create-submit"]').trigger('click')
    await flushPromises()
    expect(fn).toHaveBeenCalledWith('/api/v1/docker/folders', expect.objectContaining({ method: 'POST', body: JSON.stringify({ path: '/srv/pools/tank', name: 'Fotos' }) }))
    expect(w.find('[data-test="folder-current"]').text()).toBe('/srv/pools/tank/Fotos')
    expect(w.find('[data-test="folder-create"]').exists()).toBe(false)
    await w.find('button[data-test="folder-select"]').trigger('click')
    expect(w.emitted('select')).toEqual([['/srv/pools/tank/Fotos']])
  })

  it('prüft den Namen vor dem Absenden', async () => {
    const fn = stubCreate(() => new Response('{}', { status: 201 }))
    const w = await mountAt('/srv/pools/tank')
    await w.find('button[data-test="folder-new"]').trigger('click')
    const submit = () => w.find('button[data-test="folder-create-submit"]').attributes('disabled')
    expect(submit()).toBeDefined()
    for (const bad of ['mit Leerzeichen', 'Über', '.versteckt', 'a/b', 'lost+found', 'x'.repeat(65)]) {
      await w.find('input[data-test="folder-name"]').setValue(bad)
      expect(w.find('[data-test="folder-name-error"]').exists(), bad).toBe(true)
      expect(submit(), bad).toBeDefined()
    }
    await w.find('input[data-test="folder-name"]').setValue('Medien') // gibt es schon (siehe tree)
    expect(w.find('[data-test="folder-name-error"]').text()).toContain('schon')
    await w.find('input[data-test="folder-name"]').setValue('Neu-1.0')
    expect(w.find('[data-test="folder-name-error"]').exists()).toBe(false)
    expect(submit()).toBeUndefined()
    expect(fn).not.toHaveBeenCalledWith('/api/v1/docker/folders', expect.objectContaining({ method: 'POST' }))
  })

  it('zeigt Fehler des Servers und bleibt im Ordner', async () => {
    stubCreate(() => new Response(JSON.stringify({ error: { code: 'conflict', message: 'Ein Ordner mit diesem Namen existiert bereits.' } }), { status: 409 }))
    const w = await mountAt('/srv/pools/tank')
    await w.find('button[data-test="folder-new"]').trigger('click')
    await w.find('input[data-test="folder-name"]').setValue('Fotos')
    await w.find('button[data-test="folder-create-submit"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="folder-create-error"]').text()).toContain('existiert bereits')
    expect(w.find('[data-test="folder-current"]').text()).toBe('/srv/pools/tank')
  })
})
