import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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

describe('QuickTiles', () => {
  it('zeigt je Container und VM eine Kachel', async () => {
    const { default: QuickTiles } = await import('@/components/QuickTiles.vue')
    const containers = [{ id: 'a', name: 'web', image: 'nginx', state: 'running', status: 'Up 3 hours', stack: 'site', ports: ['0.0.0.0:8080->80/tcp'], created: '' }]
    const vms = [{ name: 'win11', uuid: 'u1', state: 'shutoff', vcpus: 2, memoryMiB: 4096, autostart: false, disks: [] }]
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string) => {
        const body = url.includes('/docker/status')
          ? { installed: true, active: true, version: '27' }
          : url.includes('/vms/status')
            ? { installed: true, active: true, kvm: true }
            : url.includes('/containers')
              ? containers
              : url.includes('/vms')
                ? vms
                : []
        return new Response(JSON.stringify(body), { status: 200 })
      }),
    )
    const pinia = createPinia()
    setActivePinia(pinia)
    const w = mount(QuickTiles, { global: { plugins: [pinia, PrimeVue, ToastService] } })
    await flushPromises()
    expect(w.find('[data-test="quick-container-web"]').text()).toContain('running')
    expect(w.find('[data-test="quick-vm-win11"]').text()).toContain('aus')
    const link = w.find('[data-test="quick-container-open-web"]')
    expect(link.element.tagName).toBe('A')
    expect(link.attributes('href')).toBe(`http://${window.location.hostname}:8080/`)
    expect(w.find('[data-test="quick-container-menu-web"]').exists()).toBe(true)
    expect(w.find('[data-test="quick-vm-menu-win11"]').exists()).toBe(true)
    expect(w.find('[data-test="quick-vm-open-win11"]').element.tagName).toBe('DIV')
  })
})

describe('QuickTiles: sortieren', () => {
  const containers = [
    { id: 'a', name: 'web', image: 'nginx', state: 'running', status: 'Up', stack: 's', ports: ['0.0.0.0:8080->80/tcp'], created: '' },
    { id: 'b', name: 'db', image: 'pg', state: 'running', status: 'Up', stack: 's', ports: [], created: '' },
  ]
  const vms = [{ name: 'win11', uuid: 'u1', state: 'shutoff', vcpus: 2, memoryMiB: 4096, autostart: false, disks: [] }]
  async function mountTiles(editing: boolean) {
    const { default: QuickTiles } = await import('@/components/QuickTiles.vue')
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string) => {
        const body = url.includes('/docker/status')
          ? { installed: true, active: true, version: '27' }
          : url.includes('/vms/status')
            ? { installed: true, active: true, kvm: true }
            : url.includes('/containers')
              ? containers
              : url.includes('/vms')
                ? vms
                : []
        return new Response(JSON.stringify(body), { status: 200 })
      }),
    )
    const pinia = createPinia()
    setActivePinia(pinia)
    useAuthStore().user = 'anna'
    const w = mount(QuickTiles, { props: { editing }, global: { plugins: [pinia, PrimeVue, ToastService] } })
    await flushPromises()
    return w
  }
  const order = (w: Awaited<ReturnType<typeof mountTiles>>) => w.findAll('[data-test^="quick-container-"].app, [data-test^="quick-vm-"].app').map((e) => e.attributes('data-test')!.replace('quick-', ''))
  const drag = async (w: Awaited<ReturnType<typeof mountTiles>>, from: string, to: string) => {
    await w.find(`[data-test="quick-${from}"]`).trigger('dragstart', { dataTransfer: { setData: vi.fn(), effectAllowed: '' } })
    await w.find(`[data-test="quick-${to}"]`).trigger('drop')
  }
  beforeEach(() => localStorage.clear())

  it('ist ohne Bearbeiten-Modus nicht verschiebbar', async () => {
    const w = await mountTiles(false)
    expect(order(w)).toEqual(['container-db', 'container-web', 'vm-win11'])
    expect(w.find('[data-test="quick-container-web"]').attributes('draggable')).toBe('false')
    expect(w.find('[data-test="quick-handle-container-web"]').exists()).toBe(false)
    await drag(w, 'vm-win11', 'container-db')
    expect(order(w)).toEqual(['container-db', 'container-web', 'vm-win11'])
    expect(localStorage.getItem('bn-quicktiles:anna')).toBeNull()
  })

  it('ordnet im Bearbeiten-Modus per Ziehen und Pfeiltasten um und merkt es sich', async () => {
    const w = await mountTiles(true)
    expect(w.find('[data-test="quick-container-web"]').attributes('draggable')).toBe('true')
    await drag(w, 'vm-win11', 'container-db')
    expect(order(w)).toEqual(['vm-win11', 'container-db', 'container-web'])
    await w.find('[data-test="quick-handle-container-web"]').trigger('keydown', { key: 'ArrowLeft' })
    expect(order(w)).toEqual(['vm-win11', 'container-web', 'container-db'])
    expect(JSON.parse(localStorage.getItem('bn-quicktiles:anna')!)).toEqual(['v:win11', 'c:web', 'c:db'])
    w.unmount()

    const w2 = await mountTiles(false)
    expect(order(w2)).toEqual(['vm-win11', 'container-web', 'container-db'])
    ;(w2.vm as unknown as { reset: () => void }).reset()
    await flushPromises()
    expect(order(w2)).toEqual(['container-db', 'container-web', 'vm-win11'])
    expect(localStorage.getItem('bn-quicktiles:anna')).toBeNull()
  })
})
