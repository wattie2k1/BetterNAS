import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import NewDiskNotice from '@/components/storage/NewDiskNotice.vue'
import { useStorageStore } from '@/stores/storage'
import { disk } from './fixtures'

const toasts: { severity: string; summary: string; detail: string }[] = []
vi.mock('primevue/usetoast', () => ({ useToast: () => ({ add: (m: { severity: string; summary: string; detail: string }) => toasts.push(m) }) }))

describe('NewDiskNotice', () => {
  const fresh = disk({ id: 'n1', device: '/dev/sdz', model: 'WD Red', serial: 'N1', sizeBytes: 8e12, role: 'unassigned', new: true })

  it('meldet nur Platten, die nach dem ersten Laden neu auftauchen, und jede nur einmal', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const store = useStorageStore()
    mount(NewDiskNotice, { global: { plugins: [pinia] } })
    store.disks = [fresh]
    store.loaded = true // erstes Laden: schon vorhandene neue Platte → kein Hinweis
    await flushPromises()
    expect(toasts.length).toBe(0)
    store.disks = [fresh, { ...fresh, id: 'n2', model: 'Seagate', new: true }]
    await flushPromises()
    expect(toasts.length).toBe(1)
    expect(toasts[0].summary).toBe('Neue Platte erkannt')
    expect(toasts[0].detail).toContain('Seagate')
    expect(toasts[0].detail).toContain('8,0 TB')
    store.disks = [{ ...fresh, new: false }, { ...fresh, id: 'n2', model: 'Seagate', new: true }]
    await flushPromises()
    expect(toasts.length).toBe(1)
  })

  it('schweigt, solange nichts geladen ist, und bei fehlenden Platten', async () => {
    toasts.length = 0
    const pinia = createPinia()
    setActivePinia(pinia)
    const store = useStorageStore()
    mount(NewDiskNotice, { global: { plugins: [pinia] } })
    store.disks = [fresh]
    await flushPromises()
    expect(toasts.length).toBe(0)
    store.loaded = true
    await flushPromises()
    store.disks = [{ ...fresh, id: 'x', state: 'missing' }]
    await flushPromises()
    expect(toasts.length).toBe(0)
  })
})
