import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import ProfileView from '@/views/ProfileView.vue'
import DockerTile from '@/components/docker/DockerTile.vue'
import { useAuthStore } from '@/stores/auth'

afterEach(() => vi.unstubAllGlobals())

describe('ProfileView', () => {
  it('zeigt Account-Daten und ändert das Passwort über die API', async () => {
    const f = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') return new Response('', { status: 204 })
      const body = url.includes('/groups') ? [] : [{ name: 'anna', fullName: 'Anna', uid: 1000, groups: ['betternas-admin'], admin: true, system: false }]
      return new Response(JSON.stringify(body), { status: 200 })
    })
    vi.stubGlobal('fetch', f)
    const pinia = createPinia()
    setActivePinia(pinia)
    useAuthStore().user = 'anna'
    const w = mount(ProfileView, { global: { plugins: [pinia, PrimeVue, ToastService] } })
    await flushPromises()
    expect(w.find('[data-test="profile-name"]').text()).toBe('anna')
    expect(w.find('[data-test="profile-groups"]').text()).toContain('betternas-admin')
    await w.find('input#profile-pw').setValue('geheim1234')
    await w.find('input#profile-pw2').setValue('geheim1234')
    await w.find('form').trigger('submit')
    await flushPromises()
    const call = f.mock.calls.find((c) => c[1]?.method === 'PUT')!
    expect(call[0]).toBe('/api/v1/users/anna/password')
    expect(JSON.parse(call[1].body)).toEqual({ password: 'geheim1234' })
  })
})

describe('DockerTile', () => {
  it('listet Container mit Status und Uptime', async () => {
    const list = [
      { id: 'a', name: 'web', image: 'nginx', state: 'running', status: 'Up 3 hours', stack: 'site', ports: [], created: '' },
      { id: 'b', name: 'db', image: 'pg', state: 'exited', status: 'Exited (0) 1 day ago', stack: '', ports: [], created: '' },
    ]
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string) => {
        const body = url.includes('/docker/status') ? { installed: true, active: true, version: '27' } : url.includes('/containers') ? list : []
        return new Response(JSON.stringify(body), { status: 200 })
      }),
    )
    const pinia = createPinia()
    setActivePinia(pinia)
    const w = mount(DockerTile, { global: { plugins: [pinia, PrimeVue, ToastService] } })
    await flushPromises()
    expect(w.find('[data-test="dock-web"]').text()).toContain('running')
    expect(w.find('[data-test="dock-web"]').text()).toContain('Up 3 hours')
    expect(w.find('[data-test="dock-db"]').text()).toContain('stopped')
  })
})
