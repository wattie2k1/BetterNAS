import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '@/api/client'

export interface Job {
  id: string
  kind: string
  target: string
  status: 'queued' | 'running' | 'succeeded' | 'failed'
  progress: number
  log: string
  startedBy: string
  startedAt: string
  finishedAt?: string
}

export const useJobsStore = defineStore('jobs', () => {
  const jobs = ref<Job[]>([])
  const waiters = new Map<string, { resolve: (j: Job) => void; reject: (e: Error) => void }>()

  const running = computed(() => jobs.value.filter((j) => j.status === 'running' || j.status === 'queued'))

  async function load() {
    jobs.value = await api.get<Job[]>('/api/v1/jobs?limit=50')
  }

  function update(j: Job) {
    const i = jobs.value.findIndex((x) => x.id === j.id)
    if (i >= 0) jobs.value[i] = j
    else jobs.value.unshift(j)
    const w = waiters.get(j.id)
    if (w && (j.status === 'succeeded' || j.status === 'failed')) {
      waiters.delete(j.id)
      if (j.status === 'succeeded') w.resolve(j)
      else w.reject(new Error(j.log.trim().split('\n').pop() ?? 'Auftrag fehlgeschlagen'))
    }
  }

  /** Wartet, bis ein Job abgeschlossen ist (Aktualisierung kommt per WebSocket). */
  function track(id: string): Promise<Job> {
    const known = jobs.value.find((j) => j.id === id)
    if (known?.status === 'succeeded') return Promise.resolve(known)
    return new Promise((resolve, reject) => {
      waiters.set(id, { resolve, reject })
      if (known) update(known)
    })
  }

  return { jobs, running, load, update, track }
})
