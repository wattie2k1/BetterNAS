import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import PoolDialog from '@/components/storage/PoolDialog.vue'
import BtrfsPanel from '@/components/storage/BtrfsPanel.vue'
import BtrfsActionDialog from '@/components/storage/BtrfsActionDialog.vue'
import BtrfsScheduleDialog from '@/components/storage/BtrfsScheduleDialog.vue'
import PoolsView from '@/views/storage/PoolsView.vue'
import { useStorageStore } from '@/stores/storage'
import { btrfsUsable, profileLabel, type BtrfsStatus, type PoolView } from '@/api/storage'
import { disk, disks, pools } from './fixtures'

const plugins = () => [createPinia(), PrimeVue, ToastService]
afterEach(() => vi.unstubAllGlobals())

const TB = 1e12
const part = { device: '/dev/sdx1', fsType: 'ntfs', uuid: 'u', label: 'Backup', sizeBytes: TB, mountpoints: [] }
const free = [
  disk({ id: 'f1', device: '/dev/sdd', model: 'Frei A', serial: 'F1', sizeBytes: 4 * TB, partitions: [part] }),
  disk({ id: 'f2', device: '/dev/sde', model: 'Frei B', serial: 'F2', sizeBytes: 4 * TB }),
  disk({ id: 'f3', device: '/dev/sdf', model: 'Frei C', serial: 'F3', sizeBytes: 1 * TB }),
]

function status(over: Partial<BtrfsStatus> = {}): BtrfsStatus {
  return {
    mounted: true,
    devices: [],
    missing: 0,
    degraded: false,
    dataProfile: 'RAID1',
    rawBytes: 8 * TB,
    usableBytes: 4 * TB,
    freeBytes: 3 * TB,
    errors: 0,
    ...over,
  }
}
const btrfsPool = (over: Partial<PoolView> = {}, st: Partial<BtrfsStatus> = {}): PoolView => ({
  name: 'tresor',
  type: 'btrfs',
  branches: ['f1', 'f2'],
  createPolicy: 'epmfs',
  minFree: '20G',
  profile: 'raid1',
  path: '/srv/pools/tresor',
  sizeBytes: 4 * TB,
  freeBytes: 3 * TB,
  mounted: true,
  btrfs: status(st),
  ...over,
})

describe('btrfsUsable', () => {
  it('berechnet die nutzbare Kapazität je Profil', () => {
    expect(btrfsUsable('single', [4 * TB, 2 * TB])).toEqual({ usable: 6 * TB, unused: 0 })
    expect(btrfsUsable('raid1', [4 * TB, 4 * TB])).toEqual({ usable: 4 * TB, unused: 0 })
    // 4 + 2: nur 2 TB lassen sich spiegeln, 2 TB der großen Platte bleiben ungenutzt
    expect(btrfsUsable('raid1', [4 * TB, 2 * TB])).toEqual({ usable: 2 * TB, unused: 2 * TB })
    expect(btrfsUsable('raid1c3', [4 * TB, 4 * TB, 4 * TB])).toEqual({ usable: 4 * TB, unused: 0 })
    expect(btrfsUsable('raid1', [])).toEqual({ usable: 0, unused: 0 })
  })
  it('beschriftet Profile', () => {
    expect(profileLabel('RAID1')).toBe('RAID1')
    expect(profileLabel('raid1c3')).toBe('RAID1c3')
    expect(profileLabel('single')).toBe('Single')
  })
})

describe('PoolDialog: btrfs', () => {
  const mountDialog = () => mount(PoolDialog, { props: { visible: true, disks: [...disks, ...free], pools }, global: { plugins: plugins() } })
  async function chooseBtrfs(w: ReturnType<typeof mountDialog>) {
    const btn = w.findAll('.p-togglebutton').find((b) => b.text().includes('btrfs'))
    expect(btn).toBeTruthy()
    await btn!.trigger('click')
  }

  it('bietet erst mergerfs, nach Umschalten nur freie, nicht eingerichtete Platten an', async () => {
    const w = mountDialog()
    expect(w.findAll('[data-test="branch-option"]').map((o) => o.attributes('data-id'))).toEqual(['d2'])
    await chooseBtrfs(w)
    expect(w.findAll('[data-test="branch-option"]').map((o) => o.attributes('data-id'))).toEqual(['d3', 'f1', 'f2', 'f3'])
    expect(w.find('[data-test="btrfs-wipe"]').exists()).toBe(true)
  })

  it('wählt bei zwei Platten RAID1, zeigt die nutzbare Kapazität und verlangt den Poolnamen', async () => {
    const w = mountDialog()
    await chooseBtrfs(w)
    await w.find('input#pool-name').setValue('tresor')
    await w.find('[data-id="f1"] input').setValue(true)
    await w.find('[data-id="f2"] input').setValue(true)
    expect(w.find('[data-test="btrfs-capacity"]').text()).toContain('Nutzbar')
    expect(w.find('[data-test="btrfs-wipe"]').text()).toContain('ntfs Backup') // vorhandene Daten werden genannt
    const submit = () => w.find('button[data-test="pool-submit"]')
    expect(submit().attributes('disabled')).toBeDefined()
    await w.find('input#pool-confirm').setValue('falsch')
    expect(submit().attributes('disabled')).toBeDefined()
    await w.find('input#pool-confirm').setValue('tresor')
    expect(submit().attributes('disabled')).toBeUndefined()
    await submit().trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([
      { name: 'tresor', type: 'btrfs', branches: ['f1', 'f2'], createPolicy: 'epmfs', minFree: '20G', profile: 'raid1', compress: '', confirmName: 'tresor' },
    ])
  })

  it('warnt bei sehr unterschiedlich großen Platten', async () => {
    const w = mountDialog()
    await chooseBtrfs(w)
    await w.find('[data-id="f1"] input').setValue(true)
    await w.find('[data-id="f3"] input').setValue(true)
    expect(w.find('[data-test="btrfs-lopsided"]').exists()).toBe(true)
  })

  it('zeigt einen Hinweis, wenn keine freie Platte da ist', async () => {
    const w = mount(PoolDialog, { props: { visible: true, disks: [disks[1]], pools }, global: { plugins: plugins() } })
    await chooseBtrfs(w as never)
    expect(w.find('[data-test="btrfs-no-disks"]').exists()).toBe(true)
  })
})

describe('BtrfsPanel', () => {
  const mountPanel = (pool: PoolView) => mount(BtrfsPanel, { props: { pool }, global: { plugins: plugins() } })

  it('zeigt Profil, Zustand und Kapazität', () => {
    const w = mountPanel(btrfsPool())
    expect(w.text()).toContain('RAID1')
    expect(w.find('[data-test="btrfs-state"]').text()).toBe('In Ordnung')
    expect(w.text()).toContain('Nutzbar')
    expect(w.find('[data-test="btrfs-degraded"]').exists()).toBe(false)
  })

  it('bietet bei unvollständigem, ausgehängtem Pool „Degradiert einhängen“ an', async () => {
    const w = mountPanel(btrfsPool({ mounted: false }, { mounted: false, degraded: true, missing: 1 }))
    expect(w.find('[data-test="btrfs-state"]').text()).toBe('Unvollständig')
    expect(w.find('[data-test="btrfs-degraded"]').text()).toContain('eine Platte')
    expect(w.find('[data-test="btrfs-scrub-start"]').attributes('disabled')).toBeDefined()
    await w.find('[data-test="btrfs-mount-degraded"]').trigger('click')
    expect(w.emitted('mountDegraded')).toBeTruthy()
  })

  it('zeigt gezählte Fehler', () => {
    const w = mountPanel(
      btrfsPool({}, { errors: 5, devices: [{ devId: 1, path: '/dev/sdd1', diskId: 'f1', sizeBytes: TB, usedBytes: 0, missing: false, errors: { write: 0, read: 3, flush: 0, corruption: 2, generation: 0 } }] }),
    )
    expect(w.find('[data-test="btrfs-state"]').text()).toBe('Fehler gezählt')
    expect(w.find('[data-test="btrfs-errors"]').text()).toContain('5')
  })

  it('zeigt Fortschritt von Wartung und Plattenersatz und sperrt dann die Prüfung', () => {
    const w = mountPanel(btrfsPool({}, { op: { op: 'balance', running: true, progress: 42 }, replace: { state: 'running', percent: 12.4, writeErrs: 0, readErrs: 0 } }))
    expect(w.find('[data-test="btrfs-op"]').text()).toContain('Balance läuft')
    expect(w.find('[data-test="btrfs-op"]').text()).toContain('42 %')
    expect(w.find('[data-test="btrfs-replace"]').text()).toContain('12 %')
    expect(w.find('[data-test="btrfs-state"]').text()).toBe('Wartung läuft')
    expect(w.find('[data-test="btrfs-scrub-start"]').attributes('disabled')).toBeDefined()
  })

  it('meldet eine fehlgeschlagene Wartung', () => {
    const w = mountPanel(btrfsPool({}, { op: { op: 'remove', running: false, result: 'failed', progress: 100, message: 'No space left' } }))
    expect(w.find('[data-test="btrfs-op-failed"]').text()).toContain('No space left')
  })

  it('startet und bricht die Prüfung ab', async () => {
    let w = mountPanel(btrfsPool())
    await w.find('[data-test="btrfs-scrub-start"]').trigger('click')
    expect(w.emitted('scrub')?.[0]).toEqual(['start'])
    w = mountPanel(btrfsPool({}, { scrub: { running: true, result: 'running', corrected: 0, uncorrectable: 0, errors: 0 } }))
    await w.find('[data-test="btrfs-scrub-cancel"]').trigger('click')
    expect(w.emitted('scrub')?.[0]).toEqual(['cancel'])
  })

  it('zeigt das Ergebnis der letzten Prüfung', () => {
    const ok = mountPanel(btrfsPool({}, { scrub: { running: false, result: 'finished', started: 'Fri Nov 20 10:00:00 2026', corrected: 0, uncorrectable: 0, errors: 0 } }))
    expect(ok.find('[data-test="btrfs-scrub"]').text()).toContain('OK')
    const bad = mountPanel(btrfsPool({}, { scrub: { running: false, result: 'finished', started: 'x', corrected: 1, uncorrectable: 2, errors: 3 } }))
    expect(bad.find('[data-test="btrfs-scrub"]').text()).toContain('2 nicht reparierbar')
  })
})

describe('BtrfsActionDialog', () => {
  const dlg = (kind: 'replace' | 'add' | 'remove' | 'balance' | 'convert', pool = btrfsPool()) =>
    mount(BtrfsActionDialog, { props: { visible: true, kind, pool, disks: [...disks, ...free] }, global: { plugins: plugins() } })

  it('Entfernen warnt, wenn das Profil keine weitere Platte erlaubt', () => {
    const w = dlg('remove')
    expect(w.find('[data-test="btrfs-remove-min"]').exists()).toBe(true) // RAID1 mit 2 Platten
    expect(w.find('button[data-test="btrfs-action-submit"]').attributes('disabled')).toBeDefined()
  })

  it('Entfernen ist bei genug Platten erlaubt und verlangt Platte und Poolnamen', async () => {
    const w = dlg('remove', btrfsPool({ branches: ['f1', 'f2', 'f3'] }))
    expect(w.find('[data-test="btrfs-remove-min"]').exists()).toBe(false)
    const submit = () => w.find('button[data-test="btrfs-action-submit"]')
    expect(submit().attributes('disabled')).toBeDefined()
    ;(w.vm as unknown as { member: string }).member = 'f3'
    await w.find('input#a-confirm').setValue('tresor')
    expect(submit().attributes('disabled')).toBeUndefined()
    await submit().trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([{ kind: 'remove', disk: 'f3', confirmName: 'tresor' }])
  })

  it('Ersetzen warnt vor dem Löschen der neuen Platte und sendet beide Platten', async () => {
    const w = dlg('replace')
    expect(w.find('[data-test="btrfs-wipe"]').text()).toContain('vollständig gelöscht')
    const vm = w.vm as unknown as { member: string; newDisk: string }
    vm.member = 'f1'
    vm.newDisk = 'f3'
    await w.find('input#a-confirm').setValue('tresor')
    await w.find('button[data-test="btrfs-action-submit"]').trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([{ kind: 'replace', oldDisk: 'f1', newDisk: 'f3', confirmName: 'tresor' }])
  })

  it('Hinzufügen bietet Balance an', async () => {
    const w = dlg('add')
    ;(w.vm as unknown as { newDisk: string }).newDisk = 'f3'
    await w.find('input#a-confirm').setValue('tresor')
    await w.find('button[data-test="btrfs-action-submit"]').trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([{ kind: 'add', newDisk: 'f3', balance: true, confirmName: 'tresor' }])
  })

  it('Schutz ändern: Verringerung verlangt den Poolnamen, Erhöhung nicht', async () => {
    const w = dlg('convert', btrfsPool({ profile: 'raid1', branches: ['f1', 'f2', 'f3'] }))
    const vm = w.vm as unknown as { profile: string }
    vm.profile = 'single'
    await w.vm.$nextTick()
    expect(w.find('[data-test="btrfs-reduces"]').exists()).toBe(true)
    expect(w.find('button[data-test="btrfs-action-submit"]').attributes('disabled')).toBeDefined()
    await w.find('input#a-confirm').setValue('tresor')
    await w.find('button[data-test="btrfs-action-submit"]').trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([{ kind: 'convert', profile: 'single', confirmName: 'tresor' }])

    const up = dlg('convert', btrfsPool({ profile: 'single', branches: ['f1', 'f2'] }))
    ;(up.vm as unknown as { profile: string }).profile = 'raid1'
    await up.vm.$nextTick()
    expect(up.find('[data-test="btrfs-reduces"]').exists()).toBe(false)
    expect(up.find('input#a-confirm').exists()).toBe(false)
    expect(up.find('button[data-test="btrfs-action-submit"]').attributes('disabled')).toBeUndefined()
  })

  it('Balance braucht keine Bestätigung', async () => {
    const w = dlg('balance')
    expect(w.find('input#a-confirm').exists()).toBe(false)
    await w.find('button[data-test="btrfs-action-submit"]').trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([{ kind: 'balance', usage: 50 }])
  })
})

describe('BtrfsScheduleDialog', () => {
  it('speichert Intervall und Uhrzeit', async () => {
    const w = mount(BtrfsScheduleDialog, { props: { visible: true, schedule: { interval: 'weekly', hour: 5 } }, global: { plugins: plugins() } })
    await w.find('button[data-test="scrub-schedule-save"]').trigger('click')
    expect(w.emitted('submit')?.[0]).toEqual([{ interval: 'weekly', hour: 5 }])
  })
})

describe('PoolsView: btrfs', () => {
  function mountPools(list: PoolView[]) {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string) => new Response(JSON.stringify(url.includes('/disks') ? [...disks, ...free] : list), { status: 200 })),
    )
    const p = createPinia()
    setActivePinia(p)
    const store = useStorageStore()
    const spies = {
      preview: vi.spyOn(store, 'previewPool').mockResolvedValue({ disks: [], confirmToken: 'tok1', expiresAt: '' }),
      create: vi.spyOn(store, 'createPool').mockResolvedValue(undefined),
      replace: vi.spyOn(store, 'btrfsReplace').mockResolvedValue({ done: Promise.resolve() }),
      remove: vi.spyOn(store, 'btrfsRemoveDisk').mockResolvedValue({ done: Promise.resolve() }),
      del: vi.spyOn(store, 'deletePool').mockResolvedValue(undefined),
    }
    const w = mount(PoolsView, { global: { plugins: [p, PrimeVue, ToastService] } })
    return { w, ...spies }
  }

  it('zeigt btrfs-Pools mit eigenem Panel, ohne SnapRAID und ohne „Bearbeiten“', async () => {
    const { w } = mountPools([btrfsPool(), pools[0]])
    await flushPromises()
    const cards = w.findAll('.p-card')
    expect(cards).toHaveLength(2)
    expect(cards[0].find('[data-test="btrfs-panel"]').exists()).toBe(true)
    expect(cards[0].find('[data-test="snapraid-panel"]').exists()).toBe(false)
    expect(cards[0].text()).not.toContain('Bearbeiten')
    expect(cards[1].find('[data-test="btrfs-panel"]').exists()).toBe(false)
    expect(cards[1].find('[data-test="snapraid-panel"]').exists()).toBe(true)
    expect(w.find('[data-test="scrub-schedule-open"]').exists()).toBe(true)
  })

  it('zeigt den Prüfzeitplan nur, wenn es einen btrfs-Pool gibt', async () => {
    const { w } = mountPools([pools[0]])
    await flushPromises()
    expect(w.find('[data-test="scrub-schedule-open"]').exists()).toBe(false)
  })

  it('legt einen btrfs-Pool erst nach der Vorschau mit Token an', async () => {
    const { w, preview, create } = mountPools([])
    await flushPromises()
    const input = { name: 'tresor', type: 'btrfs', branches: ['f1', 'f2'], createPolicy: 'epmfs', minFree: '20G', profile: 'raid1', compress: '', confirmName: 'tresor' }
    w.findComponent(PoolDialog).vm.$emit('submit', input)
    await flushPromises()
    expect(preview).toHaveBeenCalledWith(['f1', 'f2'])
    expect(create).toHaveBeenCalledWith({ ...input, confirmToken: 'tok1' })
  })

  it('startet Plattenersatz und Entfernen als lange Aufträge', async () => {
    const { w, preview, replace, remove } = mountPools([btrfsPool({ branches: ['f1', 'f2', 'f3'] })])
    await flushPromises()
    const dialog = w.findComponent(BtrfsActionDialog)
    await w.find('[data-test="btrfs-menu"]').trigger('click') // Menü öffnet sich; die Aktion kommt über das Panel
    w.findComponent(BtrfsPanel).vm.$emit('action', 'replace')
    await flushPromises()
    dialog.vm.$emit('submit', { kind: 'replace', oldDisk: 'f1', newDisk: 'f3', confirmName: 'tresor' })
    await flushPromises()
    expect(preview).toHaveBeenCalledWith(['f3'])
    expect(replace).toHaveBeenCalledWith('tresor', { diskId: 'f3', oldDiskId: 'f1', confirmToken: 'tok1', confirmName: 'tresor' })

    w.findComponent(BtrfsPanel).vm.$emit('action', 'remove')
    await flushPromises()
    dialog.vm.$emit('submit', { kind: 'remove', disk: 'f3', confirmName: 'tresor' })
    await flushPromises()
    expect(remove).toHaveBeenCalledWith('tresor', 'f3', 'tresor')
  })

  it('löscht einen btrfs-Pool nur mit Bestätigung des Namens', async () => {
    const { w, del } = mountPools([btrfsPool()])
    await flushPromises()
    await w.findAll('button').find((b) => b.text() === 'Löschen')!.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Platten werden freigegeben')
    await w.find('input#confirm-name').setValue('tresor')
    await w.find('button[data-test="delete-submit"]').trigger('click')
    await flushPromises()
    expect(del).toHaveBeenCalledWith('tresor', 'tresor')
  })
})
