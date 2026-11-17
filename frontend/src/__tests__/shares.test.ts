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
  name: 'Filme', pool: 'media', subdir: 'Filme', comment: 'Alle Filme', browsable: true, guest: false, guestWrite: false, recycle: true, fruit: false,
  acl: [{ type: 'user', name: 'bob', access: 'ro' }], path: '/srv/pools/media/Filme', poolMounted: true, uncPath: '\\\\nas\\Filme',
  nfs: { enabled: false, clients: [], readOnly: false, squash: 'root' },
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
        name: 'Fotos', pool: pools[0].name, subdir: 'Fotos', comment: '', browsable: true, guest: false, guestWrite: false, recycle: true, fruit: true,
        acl: [{ type: 'user', name: 'anna', access: 'rw' }],
        nfs: { enabled: false, clients: [], readOnly: false, squash: 'root' },
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

describe('ShareDialog: NFS', () => {
  const mountDialog = (props = {}) =>
    mount(ShareDialog, { props: { visible: true, pools, users, groups, shares: [existing], defaultWriter: 'anna', ...props }, global: { plugins: plugins() } })
  const submit = (w: ReturnType<typeof mountDialog>) => w.find('button[data-test="share-submit"]')

  async function withName(w: ReturnType<typeof mountDialog>) {
    await w.find('input#share-name').setValue('Fotos')
  }

  it('blendet die NFS-Felder erst nach dem Einschalten ein und erklärt die Rechte', async () => {
    const w = mountDialog()
    expect(w.find('textarea#nfs-clients').exists()).toBe(false)
    await w.find('input#opt-nfs').setValue(true)
    expect(w.find('textarea#nfs-clients').exists()).toBe(true)
    expect(w.text()).toContain('Samba-Rechte gelten hier nicht')
  })

  it('sendet bereinigte Clients, Nur-Lesen und Abbildungsmodus', async () => {
    const w = mountDialog()
    await withName(w)
    await w.find('input#opt-nfs').setValue(true)
    await w.find('textarea#nfs-clients').setValue('192.168.1.0/24\n  nas.local \n\n')
    await w.find('input#nfs-readonly').setValue(true)
    await w.find('select#nfs-squash').setValue('all')
    expect(submit(w).attributes('disabled')).toBeUndefined()
    await submit(w).trigger('click')
    expect((w.emitted('submit')?.[0][0] as { nfs: unknown }).nfs).toEqual({
      enabled: true, clients: ['192.168.1.0/24', 'nas.local'], readOnly: true, squash: 'all',
    })
  })

  it('sperrt Speichern bei * , Optionen oder fehlenden Clients', async () => {
    const w = mountDialog()
    await withName(w)
    await w.find('input#opt-nfs').setValue(true)
    expect(submit(w).attributes('disabled')).toBeDefined() // keine Clients
    for (const bad of ['*', '10.0.0.1(rw)', '10.0.0.1,10.0.0.2', '999.1.1.1', 'a b']) {
      await w.find('textarea#nfs-clients').setValue(bad)
      expect(submit(w).attributes('disabled'), bad).toBeDefined()
      expect(w.find('[data-test="nfs-error"]').text(), bad).toContain(bad.split(/\s/)[0].slice(0, 3))
    }
    for (const good of ['fe80::1', '2001:db8::/32', 'host-1.example.org']) {
      await w.find('textarea#nfs-clients').setValue(good)
      expect(submit(w).attributes('disabled'), good).toBeUndefined()
    }
  })

  it('füllt beim Bearbeiten vor', () => {
    const w = mountDialog({
      share: { ...existing, nfs: { enabled: true, clients: ['pc.local', '10.0.0.0/8'], readOnly: false, squash: 'all' }, nfsMount: 'nas:/srv/pools/media/Filme' },
    })
    expect((w.find('textarea#nfs-clients').element as HTMLTextAreaElement).value).toBe('pc.local\n10.0.0.0/8')
    expect((w.find('select#nfs-squash').element as HTMLSelectElement).value).toBe('all')
  })
})

describe('Freigabenliste: NFS', () => {
  it('zeigt Tag und Mount-Zeile nur bei aktivem NFS', async () => {
    const withNfs = { ...existing, name: 'Mit', nfs: { enabled: true, clients: ['10.0.0.1'], readOnly: false, squash: 'root' }, nfsMount: 'nas:/srv/pools/media/Mit' }
    const without = { ...existing, name: 'Ohne' }
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string) => {
        const body = url === '/api/v1/shares' ? [withNfs, without] : url.includes('/pools') ? pools : url.includes('/disks') ? [] : []
        return new Response(JSON.stringify(body), { status: 200 })
      }),
    )
    const SharesView = (await import('@/views/shares/SharesView.vue')).default
    const ToastService = (await import('primevue/toastservice')).default
    const w = mount(SharesView, { global: { plugins: [createPinia(), PrimeVue, ToastService] } })
    await flushPromises()
    expect(w.find('[data-test="share-Mit"]').text()).toContain('NFS')
    expect(w.find('[data-test="share-Mit"] [data-test="nfs-mount"]').text()).toContain('mount -t nfs nas:/srv/pools/media/Mit /mnt')
    expect(w.find('[data-test="share-Ohne"]').text()).not.toContain('NFS')
    expect(w.find('[data-test="share-Ohne"] [data-test="nfs-mount"]').exists()).toBe(false)
  })
})

describe('SharesView', () => {
  it('erklärt, warum eine Freigabe ohne eingehängten Pool nicht erreichbar ist', async () => {
    const { default: SharesView } = await import('@/views/shares/SharesView.vue')
    const ToastService = (await import('primevue/toastservice')).default
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url: string) =>
        Promise.resolve(new Response(JSON.stringify(url.endsWith('/shares') ? [{ ...existing, poolMounted: false }] : []), { status: 200 })),
      ),
    )
    const w = mount(SharesView, { global: { plugins: [createPinia(), PrimeVue, ToastService] } })
    await flushPromises()
    expect(w.find('[data-test="pool-missing"]').text()).toContain('„media“ ist nicht eingehängt')
  })
})
