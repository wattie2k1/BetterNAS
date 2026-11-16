import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import UserDialog from '@/components/users/UserDialog.vue'
import PasswordDialog from '@/components/users/PasswordDialog.vue'
import GroupDialog from '@/components/users/GroupDialog.vue'
import { useUsersStore } from '@/stores/users'
import { NAME_RE, type GroupView, type UserView } from '@/api/users'

const users: UserView[] = [
  { name: 'wattie', fullName: 'André', uid: 1000, groups: [], admin: true, system: true },
  { name: 'anna', fullName: '', uid: 10000, groups: ['familie'], admin: false, system: false },
]
const groups: GroupView[] = [{ name: 'familie', gid: 10000, members: ['anna'] }]
const plugins = () => [createPinia(), PrimeVue]

afterEach(() => vi.unstubAllGlobals())

describe('UserDialog', () => {
  const mountDialog = (props = {}) => mount(UserDialog, { props: { visible: true, groups, users, ...props }, global: { plugins: plugins() } })
  const submit = (w: ReturnType<typeof mountDialog>) => w.find('button[data-test="user-submit"]')

  it('verlangt Passwort mit mindestens 8 Zeichen und passender Wiederholung', async () => {
    const w = mountDialog()
    await w.find('input#user-name').setValue('bob')
    expect(submit(w).attributes('disabled')).toBeDefined()
    await w.find('input#user-pw').setValue('kurz')
    await w.find('input#user-pw2').setValue('kurz')
    expect(submit(w).attributes('disabled')).toBeDefined()
    expect(w.text()).toContain('Mindestens 8 Zeichen')
    await w.find('input#user-pw').setValue('geheim123')
    await w.find('input#user-pw2').setValue('geheim124')
    expect(submit(w).attributes('disabled')).toBeDefined()
    expect(w.text()).toContain('stimmen nicht überein')
    await w.find('input#user-pw2').setValue('geheim123')
    expect(submit(w).attributes('disabled')).toBeUndefined()
  })

  it('sendet Name, Gruppen und Rolle', async () => {
    const w = mountDialog()
    await w.find('input#user-name').setValue('bob')
    await w.find('input#user-full').setValue('Bob B')
    await w.find('input#user-pw').setValue('geheim123')
    await w.find('input#user-pw2').setValue('geheim123')
    await w.find('input#g-familie').setValue(true)
    await w.find('input#user-admin').setValue(true)
    await submit(w).trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([{ name: 'bob', fullName: 'Bob B', password: 'geheim123', groups: ['familie'], admin: true }])
  })

  it('lehnt vorhandene und ungültige Namen ab', async () => {
    const w = mountDialog()
    await w.find('input#user-name').setValue('anna')
    expect(w.text()).toContain('existiert bereits')
    await w.find('input#user-name').setValue('Bob')
    expect(w.text()).toContain('Kleinbuchstaben')
  })

  it('verlangt beim Bearbeiten kein Passwort', () => {
    const w = mountDialog({ user: users[1] })
    expect(w.find('input#user-pw').exists()).toBe(false)
    expect(submit(w).attributes('disabled')).toBeUndefined()
  })
})

describe('PasswordDialog', () => {
  it('aktiviert Ändern nur bei gültigem, wiederholtem Passwort', async () => {
    const w = mount(PasswordDialog, { props: { visible: true, user: 'anna' }, global: { plugins: plugins() } })
    const btn = () => w.find('button[data-test="password-submit"]')
    expect(btn().attributes('disabled')).toBeDefined()
    await w.find('input#pw-new').setValue('neues-passwort')
    await w.find('input#pw-repeat').setValue('neues-passwort')
    expect(btn().attributes('disabled')).toBeUndefined()
    await btn().trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual(['neues-passwort'])
  })
})

describe('GroupDialog', () => {
  it('sendet Name und Mitglieder', async () => {
    const w = mount(GroupDialog, { props: { visible: true, users, groups }, global: { plugins: plugins() } })
    await w.find('input#group-name').setValue('kinder')
    await w.find('input#m-anna').setValue(true)
    await w.find('button[data-test="group-submit"]').trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([{ name: 'kinder', members: ['anna'] }])
  })
})

describe('Store', () => {
  it('lädt bei users.changed neu', async () => {
    setActivePinia(createPinia())
    const fetchMock = vi.fn().mockImplementation((url: string) =>
      Promise.resolve(new Response(JSON.stringify(url.endsWith('/users') ? users : groups), { status: 200 })),
    )
    vi.stubGlobal('fetch', fetchMock)
    const store = useUsersStore()
    store.handleEvent({ type: 'disks.changed' })
    await flushPromises()
    expect(fetchMock).not.toHaveBeenCalled()
    store.handleEvent({ type: 'users.changed' })
    await flushPromises()
    expect(store.users).toHaveLength(2)
    expect(store.groups[0].name).toBe('familie')
  })

  it('lädt auch nach einem Fehler neu und reicht ihn weiter', async () => {
    setActivePinia(createPinia())
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (init?.method === 'DELETE') return Promise.resolve(new Response(JSON.stringify({ error: { code: 'conflict', message: 'Letzter Administrator' } }), { status: 409 }))
      return Promise.resolve(new Response(JSON.stringify(url.endsWith('/users') ? users : groups), { status: 200 }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const store = useUsersStore()
    await expect(store.deleteUser('wattie')).rejects.toThrow('Letzter Administrator')
    expect(store.users).toHaveLength(2)
  })
})

describe('Namensregel', () => {
  it('entspricht dem Backend', () => {
    for (const ok of ['anna', '_svc', 'a-b_1']) expect(NAME_RE.test(ok)).toBe(true)
    for (const bad of ['Anna', '1a', 'a:b', 'a b', 'a'.repeat(33)]) expect(NAME_RE.test(bad)).toBe(false)
  })
})
