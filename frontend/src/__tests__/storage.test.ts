import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import FormatDialog from '@/components/storage/FormatDialog.vue'
import PoolDialog from '@/components/storage/PoolDialog.vue'
import DisksView from '@/views/storage/DisksView.vue'
import { useStorageStore } from '@/stores/storage'
import { POOL_NAME_RE } from '@/api/storage'
import { disks, pools, preview } from './fixtures'

const plugins = () => [createPinia(), PrimeVue, ToastService]

afterEach(() => vi.unstubAllGlobals())

describe('FormatDialog', () => {
  const mountDialog = () =>
    mount(FormatDialog, { props: { visible: true, preview }, global: { plugins: plugins() } })

  it('aktiviert Formatieren erst bei exakt eingetippter Seriennummer', async () => {
    const w = mountDialog()
    const btn = () => w.find('button[data-test="format-submit"]')
    expect(btn().attributes('disabled')).toBeDefined()
    await w.find('input#confirm-serial').setValue('wd-wcc7k1234567')
    expect(btn().attributes('disabled')).toBeDefined()
    await w.find('input#confirm-serial').setValue('WD-WCC7K1234567')
    expect(btn().attributes('disabled')).toBeUndefined()
    await btn().trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([{ fsType: 'ext4', confirmSerial: 'WD-WCC7K1234567', confirmToken: 'tok' }])
  })

  it('warnt vor vorhandenen Daten', () => {
    expect(mountDialog().text()).toContain('unwiderruflich gelöscht')
  })
})

describe('PoolDialog', () => {
  const mountDialog = (props = {}) =>
    mount(PoolDialog, { props: { visible: true, disks, pools, ...props }, global: { plugins: plugins() } })

  it('bietet nur freie Datenplatten an', () => {
    const ids = mountDialog()
      .findAll('[data-test="branch-option"]')
      .map((o) => o.attributes('data-id'))
    expect(ids).toEqual(['d2'])
  })

  it('bietet beim Bearbeiten auch die eigenen Platten an', () => {
    const ids = mountDialog({ pool: pools[0] })
      .findAll('[data-test="branch-option"]')
      .map((o) => o.attributes('data-id'))
    expect(ids).toEqual(['d1', 'd2'])
  })

  it('prüft den Namen und verlangt mindestens eine Platte', async () => {
    const w = mountDialog()
    const save = () => w.find('button[data-test="pool-submit"]')
    await w.find('input#pool-name').setValue('Media')
    expect(w.text()).toContain('Kleinbuchstaben')
    await w.find('input#pool-name').setValue('backup')
    expect(save().attributes('disabled')).toBeDefined()
    await w.find('[data-test="branch-option"] input').setValue(true)
    expect(save().attributes('disabled')).toBeUndefined()
    await save().trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([{ name: 'backup', branches: ['d2'], createPolicy: 'epmfs', minFree: '20G' }])
  })

  it('verwendet dieselbe Namensregel wie das Backend', () => {
    expect(POOL_NAME_RE.test('media-2')).toBe(true)
    for (const bad of ['Media', '../x', 'a,b', '1abc', 'a'.repeat(33)]) expect(POOL_NAME_RE.test(bad)).toBe(false)
  })
})

describe('DisksView', () => {
  it('zeigt Systemplatte ohne Aktionen und fehlende Platten als „Fehlt“', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string) =>
        new Response(JSON.stringify(url.includes('/disks') ? disks : pools), { status: 200 }),
      ),
    )
    const w = mount(DisksView, { global: { plugins: plugins() } })
    await flushPromises()
    const row = (id: string) => w.find(`[data-test="disk-${id}"]`)
    expect(row('sys').findAll('button').length).toBe(0)
    expect(row('d4').text()).toContain('Fehlt')
    expect(row('d3').text()).toContain('Formatieren')
    expect(row('d2').text()).toContain('Freigeben')
    expect(row('d1').text()).not.toContain('Freigeben')
  })
})

describe('storage store', () => {
  it('lädt bei pools.changed neu', async () => {
    setActivePinia(createPinia())
    const fetchMock = vi.fn().mockImplementation(async () => new Response('[]', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    const store = useStorageStore()
    store.handleEvent({ type: 'pools.changed' })
    await flushPromises()
    expect(fetchMock.mock.calls.map((c) => c[0])).toEqual(expect.arrayContaining(['/api/v1/pools']))
    fetchMock.mockClear()
    store.handleEvent({ type: 'job.updated' })
    await flushPromises()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
