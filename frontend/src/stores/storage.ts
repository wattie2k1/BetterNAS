import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { storageApi, type DiskNote, type DiskView, type EraseRequest, type FormatRequest, type SelfTestKind, type TuningSettings, type PoolInput, type PoolView, type SnapraidInput } from '@/api/storage'
import type { EventMessage } from '@/api/ws'
import { useJobsStore } from './jobs'

export const useStorageStore = defineStore('storage', () => {
  const disks = ref<DiskView[]>([])
  const pools = ref<PoolView[]>([])
  const loading = ref(false)
  /** Mindestens einmal erfolgreich geladen (damit beim ersten Laden vorhandene Platten nicht als „neu angeschlossen“ gelten). */
  const loaded = ref(false)
  const newDisks = computed(() => disks.value.filter((d) => d.new && d.state === 'online'))
  const error = ref('')

  async function refresh() {
    loading.value = true
    try {
      const [d, p] = await Promise.all([storageApi.disks(), storageApi.pools()])
      disks.value = d
      pools.value = p
      loaded.value = true
      error.value = ''
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  function handleEvent(m: EventMessage) {
    if (m.type === 'disks.changed' || m.type === 'pools.changed') void refresh()
  }

  /** Startet eine Änderung und wartet auf den zugehörigen Auftrag. */
  async function runJob(start: () => Promise<{ jobId: string }>) {
    const { jobId } = await start()
    try {
      await useJobsStore().track(jobId)
    } finally {
      await refresh()
    }
  }

  return {
    disks,
    pools,
    loading,
    loaded,
    newDisks,
    error,
    refresh,
    handleEvent,
    previewFormat: storageApi.previewFormat,
    setSpare: async (id: string, spare: boolean) => {
      await storageApi.setSpare(id, spare)
      await refresh()
    },
    previewErase: storageApi.previewErase,
    startErase: async (id: string, req: EraseRequest) => {
      await storageApi.startErase(id, req)
      await refresh()
    },
    abortErase: async (id: string) => {
      await storageApi.abortErase(id)
      await refresh()
    },
    format: (id: string, req: FormatRequest) => runJob(() => storageApi.format(id, req)),
    importDisk: (id: string, role?: 'data' | 'parity') => runJob(() => storageApi.importDisk(id, role)),
    setSpindown: async (id: string, minutes: number) => {
      await storageApi.setSpindown(id, minutes)
      await refresh()
    },
    setTuning: async (id: string, t: TuningSettings) => {
      await storageApi.setTuning(id, t)
      await refresh()
    },
    setNote: async (id: string, n: DiskNote) => {
      await storageApi.setNote(id, n)
      await refresh()
    },
    dismissNew: async (id: string) => {
      await storageApi.dismissNew(id)
      await refresh()
    },
    startBench: async (id: string) => {
      await storageApi.startBench(id)
      await refresh()
    },
    startFsCheck: async (id: string) => {
      await storageApi.startFsCheck(id)
      await refresh()
    },
    startIdentify: async (id: string) => {
      await storageApi.startIdentify(id)
      await refresh()
    },
    stopIdentify: async (id: string) => {
      await storageApi.stopIdentify(id)
      await refresh()
    },
    startSelfTest: async (id: string, kind: SelfTestKind) => {
      await storageApi.startSelfTest(id, kind)
      await refresh()
    },
    abortSelfTest: async (id: string) => {
      await storageApi.abortSelfTest(id)
      await refresh()
    },
    release: (id: string) => runJob(() => storageApi.release(id)),
    createPool: (p: PoolInput) => runJob(() => storageApi.createPool(p)),
    updatePool: (name: string, p: PoolInput) => runJob(() => storageApi.updatePool(name, p)),
    deletePool: (name: string) => runJob(() => storageApi.deletePool(name)),
    enableSnapraid: (pool: string, input: SnapraidInput) => runJob(() => storageApi.enableSnapraid(pool, input)),
    disableSnapraid: (pool: string) => runJob(() => storageApi.disableSnapraid(pool)),
    replaceDisk: (pool: string, oldDisk: string, newDisk: string) => runJob(() => storageApi.replaceDisk(pool, oldDisk, newDisk)),
    retryRecovery: (pool: string) => runJob(() => storageApi.retryRecovery(pool)),
    cancelRecovery: async (pool: string) => {
      await storageApi.cancelRecovery(pool)
      await refresh()
    },
    runSnapraid: (pool: string, action: 'sync' | 'scrub' | 'diff', force = false) =>
      runJob(() => storageApi.runSnapraid(pool, action, force)),
  }
})
