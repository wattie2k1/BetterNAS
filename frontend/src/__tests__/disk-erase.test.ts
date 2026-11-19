import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import EraseDialog from '@/components/storage/EraseDialog.vue'
import DisksView from '@/views/storage/DisksView.vue'

const preview = { diskId: 'd1', device: '/dev/sdb', model: 'WDC', serial: 'SER1', sizeBytes: 2e12, partitions: [{ device: '/dev/sdb1', fsType: 'ext4', label: 'alt', sizeBytes: 1e12, uuid: 'u', mountpoints: [] }], confirmToken: 'tok', expiresAt: '' }
const plugins = () => [createPinia(), PrimeVue, ToastService]

afterEach(() => vi.unstubAllGlobals())

describe('EraseDialog', () => {
  it('erlaubt das Löschen erst nach Eingabe der Seriennummer', async () => {
    const w = mount(EraseDialog, { props: { visible: true, preview }, global: { plugins: plugins() } })
    expect(w.find('[data-test="erase-submit"]').attributes('disabled')).toBeDefined()
    await w.find('[data-test="erase-serial"]').setValue('FALSCH')
    expect(w.find('[data-test="erase-submit"]').attributes('disabled')).toBeDefined()
    await w.find('[data-test="erase-serial"]').setValue('SER1')
    await w.find('[data-test="erase-submit"]').trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([{ method: 'zero', confirmSerial: 'SER1', confirmToken: 'tok' }])
  })

  it('bietet TRIM nur für SSDs an', async () => {
    const hdd = mount(EraseDialog, { props: { visible: true, preview }, global: { plugins: plugins() } })
    expect(hdd.find('input#erase-discard').exists()).toBe(false)
    hdd.unmount()
    const ssd = mount(EraseDialog, { props: { visible: true, preview, ssd: true }, global: { plugins: plugins() } })
    expect(ssd.find('input#erase-discard').exists()).toBe(true)
  })
})

describe('DisksView: Löschen', () => {
  const base = { id: 'd1', device: '/dev/sdb', model: 'WDC', serial: 'SER1', sizeBytes: 2e12, role: 'unassigned', state: 'online', manageable: true, rotational: true, partitions: [] }
  function mountWith(disk: object) {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async (url: string) => new Response(url.endsWith('/disks') ? JSON.stringify([disk]) : '[]', { status: 200 })))
    return mount(DisksView, { global: { plugins: plugins() } })
  }

  it('zeigt Fortschritt und Abbrechen während des Löschens', async () => {
    const w = mountWith({ ...base, erase: { running: true, method: 'zero', writtenBytes: 5e11, totalBytes: 2e12 } })
    await flushPromises()
    expect(w.find('[data-test="erase-running-d1"]').text()).toContain('25 %')
    expect(w.find('[data-test="erase-abort-d1"]').exists()).toBe(true)
    expect(w.find('[data-test="erase-d1"]').attributes('disabled')).toBeDefined()
  })

  it('zeigt das Ergebnis und bietet das Löschen nur nicht zugewiesenen Platten an', async () => {
    const done = mountWith({ ...base, erase: { running: false, method: 'zero', writtenBytes: 2e12, totalBytes: 2e12, result: 'done' } })
    await flushPromises()
    expect(done.find('[data-test="erase-result-d1"]').text()).toContain('Platte gelöscht')
    done.unmount()
    const data = mountWith({ ...base, role: 'data' })
    await flushPromises()
    expect(data.find('[data-test="erase-d1"]').exists()).toBe(false)
  })
})
