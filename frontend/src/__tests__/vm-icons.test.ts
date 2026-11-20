import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useVmsStore } from '@/stores/vms'
import { vmIconUrl, vmsApi } from '@/api/vms'

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => vi.unstubAllGlobals())

describe('VM-Icons', () => {
  it('leitet die Icon-Adresse aus der Version der VM ab', () => {
    const s = useVmsStore()
    s.vms = [
      { name: 'debian', uuid: 'a', state: 'running', vcpus: 1, memoryMiB: 512, autostart: false, disks: [], icon: 'abc' },
      { name: 'plain', uuid: 'b', state: 'shutoff', vcpus: 1, memoryMiB: 512, autostart: false, disks: [] },
    ]
    expect(s.iconUrlOf('debian')).toBe(vmIconUrl('debian', 'abc'))
    expect(s.iconUrlOf('debian')).toBe('/api/v1/vm-icons/debian?v=abc')
    expect(s.iconUrlOf('plain')).toBeNull()
  })

  it('lädt Dateien mit ihrem Typ hoch und ruft für Adressen JSON auf', async () => {
    const fn = vi.fn().mockImplementation(() => Promise.resolve(new Response(null, { status: 204 })))
    vi.stubGlobal('fetch', fn)
    await vmsApi.setIconFile('debian', new File([new Uint8Array([1])], 'i.png', { type: 'image/png' }))
    await vmsApi.setIconUrl('debian', 'https://x/y.png')
    await vmsApi.removeIcon('debian')
    const [file, url, del] = fn.mock.calls as [string, RequestInit][]
    expect(file[0]).toBe('/api/v1/vm-icons/debian')
    expect(new Headers(file[1].headers).get('Content-Type')).toBe('image/png')
    expect(JSON.parse(url[1].body as string)).toEqual({ url: 'https://x/y.png' })
    expect(del[1].method).toBe('DELETE')
  })
})
