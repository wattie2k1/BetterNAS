import { defineStore } from 'pinia'
import { ref } from 'vue'
import { storageApi, type DiskView, type FormatRequest, type PoolInput, type PoolView, type SnapraidInput } from '@/api/storage'
import type { EventMessage } from '@/api/ws'
import { useJobsStore } from './jobs'

export const useStorageStore = defineStore('storage', () => {
  const disks = ref<DiskView[]>([])
  const pools = ref<PoolView[]>([])
  const loading = ref(false)
  const error = ref('')

  async function refresh() {
    loading.value = true
    try {
      const [d, p] = await Promise.all([storageApi.disks(), storageApi.pools()])
      disks.value = d
      pools.value = p
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
    error,
    refresh,
    handleEvent,
    previewFormat: storageApi.previewFormat,
    format: (id: string, req: FormatRequest) => runJob(() => storageApi.format(id, req)),
    importDisk: (id: string, role?: 'data' | 'parity') => runJob(() => storageApi.importDisk(id, role)),
    release: (id: string) => runJob(() => storageApi.release(id)),
    createPool: (p: PoolInput) => runJob(() => storageApi.createPool(p)),
    updatePool: (name: string, p: PoolInput) => runJob(() => storageApi.updatePool(name, p)),
    deletePool: (name: string) => runJob(() => storageApi.deletePool(name)),
    enableSnapraid: (pool: string, input: SnapraidInput) => runJob(() => storageApi.enableSnapraid(pool, input)),
    disableSnapraid: (pool: string) => runJob(() => storageApi.disableSnapraid(pool)),
    runSnapraid: (pool: string, action: 'sync' | 'scrub' | 'diff', force = false) =>
      runJob(() => storageApi.runSnapraid(pool, action, force)),
  }
})
