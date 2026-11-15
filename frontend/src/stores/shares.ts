import { defineStore } from 'pinia'
import { ref } from 'vue'
import { sharesApi, type ShareInput, type ShareView } from '@/api/shares'
import type { EventMessage } from '@/api/ws'
import { useJobsStore } from './jobs'

export const useSharesStore = defineStore('shares', () => {
  const shares = ref<ShareView[]>([])
  const loading = ref(false)
  const error = ref('')

  async function refresh() {
    loading.value = true
    try {
      shares.value = await sharesApi.list()
      error.value = ''
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  function handleEvent(m: EventMessage) {
    if (m.type === 'shares.changed' || m.type === 'pools.changed') void refresh()
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
    shares,
    loading,
    error,
    refresh,
    handleEvent,
    create: (s: ShareInput) => runJob(() => sharesApi.create(s)),
    update: (name: string, s: ShareInput) => runJob(() => sharesApi.update(name, s)),
    remove: (name: string) => runJob(() => sharesApi.remove(name)),
  }
})
