import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import TemplateDialog from '@/components/docker/TemplateDialog.vue'
import { rootsState, roots, setRoots } from '@/utils/appTemplates'

const plugins = () => [createPinia(), PrimeVue]
afterEach(() => {
  vi.unstubAllGlobals()
  rootsState.known = false
})

function stubPaths(res: Response) {
  const fn = vi.fn().mockImplementation(() => Promise.resolve(res.clone()))
  vi.stubGlobal('fetch', fn)
  return fn
}

describe('Vorlagen-Dialog ohne geladene Speicherorte', () => {
  it('warnt, wenn die Speicherorte nicht geladen werden konnten', async () => {
    stubPaths(new Response(JSON.stringify({ error: { code: 'internal', message: 'kaputt' } }), { status: 500 }))
    const w = mount(TemplateDialog, { props: { visible: false, existing: [] }, global: { plugins: plugins() }, attachTo: document.body })
    await w.setProps({ visible: true })
    await flushPromises()
    await w.find('[data-test="template-jellyfin"]').trigger('click')
    expect(w.find('[data-test="tpl-roots-unknown"]').exists()).toBe(true)
    w.unmount()
  })

  it('lädt die Speicherorte beim Öffnen nach und warnt dann nicht mehr', async () => {
    const before = { ...roots }
    stubPaths(new Response(JSON.stringify({ configured: true, appdata: '/srv/pools/a/cfg', dockerdata: '/srv/pools/b/dd', warnings: [], staleStacks: 0 }), { status: 200 }))
    const w = mount(TemplateDialog, { props: { visible: false, existing: [] }, global: { plugins: plugins() }, attachTo: document.body })
    await w.setProps({ visible: true })
    await flushPromises()
    expect(rootsState.known).toBe(true)
    expect(roots.dockerdata).toBe('/srv/pools/b/dd')
    await w.find('[data-test="template-jellyfin"]').trigger('click')
    expect(w.find('[data-test="tpl-roots-unknown"]').exists()).toBe(false)
    setRoots(before)
    w.unmount()
  })
})
