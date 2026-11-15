import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ShareDialog from '@/components/shares/ShareDialog.vue'
import { useSharesStore } from '@/stores/shares'
import { isReservedShareName, SHARE_NAME_RE, subdirError, type ShareView } from '@/api/shares'
import type { GroupView, UserView } from '@/api/users'
import { pools } from './fixtures'

const users: UserView[] = [
  { name: 'anna', fullName: 'Anna', uid: 1000, groups: [], admin: true, system: true },
  { name: 'bob', fullName: '', uid: 10000, groups: [], admin: false, system: false },
]
const groups: GroupView[] = [{ name: 'familie', gid: 10000, members: [] }]
const existing: ShareView = {
  name: 'Filme', pool: 'media', subdir: 'Filme', comment: 'Alle Filme', browsable: true, guest: false, recycle: true, fruit: false,
  acl: [{ type: 'user', name: 'bob', access: 'ro' }], path: '/srv/pools/media/Filme', poolMounted: true, uncPath: '\\\\nas\\Filme',
}
const plugins = () => [createPinia(), PrimeVue]

afterEach(() => vi.unstubAllGlobals())

describe('ShareDialog', () => {
  const mountDialog = (props = {}) =>
    mount(ShareDialog, { props: { visible: true, pools, users, groups, shares: [existing], ...props }, global: { plugins: plugins() } })
  const submit = (w: ReturnType<typeof mountDialog>) => w.find('button[data-test="share-submit"]')

  it('verlangt ein Recht oder Gastzugriff', async () => {
    const w = mountDialog()
    await w.find('input#share-name').setValue('Fotos')
    expect(submit(w).attributes('disabled')).toBeDefined()
    expect(w.text()).toContain('Mindestens ein Benutzer')
    await w.find('input#opt-guest').setValue(true)
    expect(submit(w).attributes('disabled')).toBeUndefined()
  })

  it('belegt den Schreibzugriff des angemeldeten Administrators vor', async () => {
    const w = mountDialog({ defaultWriter: 'anna' })
    await w.find('input#share-name').setValue('Fotos')
    expect(submit(w).attributes('disabled')).toBeUndefined()
    await submit(w).trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([
      {
        name: 'Fotos', pool: pools[0].name, subdir: 'Fotos', comment: '', browsable: true, guest: false, recycle: true, fruit: true,
        acl: [{ type: 'user', name: 'anna', access: 'rw' }],
      },
    ])
  })

  it('übernimmt den Namen als Ordner, bis der Ordner von Hand geändert wird', async () => {
    const w = mountDialog({ defaultWriter: 'anna' })
    const folder = () => (w.find('input#share-subdir').element as HTMLInputElement).value
    await w.find('input#share-name').setValue('Fot')
    await w.find('input#share-name').setValue('Fotos')
    expect(folder()).toBe('Fotos')
    await w.find('input#share-subdir').setValue('Medien/Bilder')
    await w.find('input#share-subdir').trigger('input')
    await w.find('input#share-name').setValue('Fotos2')
    expect(folder()).toBe('Medien/Bilder')
  })

  it('lehnt vorhandene, reservierte und ungültige Namen ab', async () => {
    const w = mountDialog({ defaultWriter: 'anna' })
    await w.find('input#share-name').setValue('filme')
    expect(w.text()).toContain('existiert bereits')
    expect(submit(w).attributes('disabled')).toBeDefined()
    await w.find('input#share-name').setValue('homes')
    expect(w.text()).toContain('reserviert')
    await w.find('input#share-name').setValue('a b')
    expect(w.text()).toContain('Buchstaben, Ziffern')
  })

  it('verbietet Ordner außerhalb des Pools', async () => {
    const w = mountDialog({ defaultWriter: 'anna' })
    await w.find('input#share-name').setValue('Fotos')
    await w.find('input#share-subdir').setValue('../etc')
    await w.find('input#share-subdir').trigger('input')
    expect(submit(w).attributes('disabled')).toBeDefined()
    expect(w.text()).toContain('Ordnernamen')
  })

  it('zeigt beim Bearbeiten die vorhandenen Rechte und sperrt den Namen', () => {
    const w = mountDialog({ share: existing })
    expect((w.find('input#share-name').element as HTMLInputElement).disabled).toBe(true)
    expect(submit(w).attributes('disabled')).toBeUndefined()
    expect(w.text()).toContain('Lesen')
  })
})

describe('Regeln', () => {
  it('entsprechen dem Backend', () => {
    for (const ok of ['Filme', 'a', 'a-b_1']) expect(SHARE_NAME_RE.test(ok)).toBe(true)
    for (const bad of ['', '-a', 'a b', 'a'.repeat(33)]) expect(SHARE_NAME_RE.test(bad)).toBe(false)
    expect(isReservedShareName('HOMES')).toBe(true)
    expect(subdirError('a/b')).toBe('')
    expect(subdirError('a/../b')).not.toBe('')
    expect(subdirError('a/b/c/d/e')).not.toBe('')
    expect(subdirError('.versteckt')).not.toBe('')
  })
})

describe('Store', () => {
  it('lädt bei shares.changed und pools.changed neu, sonst nicht', async () => {
    setActivePinia(createPinia())
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify([existing]), { status: 200 })))
    vi.stubGlobal('fetch', fetchMock)
    const store = useSharesStore()
    store.handleEvent({ type: 'users.changed' })
    await flushPromises()
    expect(fetchMock).not.toHaveBeenCalled()
    store.handleEvent({ type: 'shares.changed' })
    await flushPromises()
    expect(store.shares[0].name).toBe('Filme')
    store.handleEvent({ type: 'pools.changed' })
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
