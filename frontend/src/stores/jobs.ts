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

/** Wie oft ein verfolgter Auftrag zusätzlich abgefragt wird, falls das WebSocket-Ereignis ausbleibt. */
export const TRACK_POLL_MS = 5000

type Waiter = { resolve: (j: Job) => void; reject: (e: Error) => void }

export const useJobsStore = defineStore('jobs', () => {
  const jobs = ref<Job[]>([])
  const waiters = new Map<string, Waiter[]>()
  const pollers = new Map<string, ReturnType<typeof setInterval>>()

  const running = computed(() => jobs.value.filter((j) => j.status === 'running' || j.status === 'queued'))

  async function load() {
    jobs.value = await api.get<Job[]>('/api/v1/jobs?limit=50')
    // Nach einer Unterbrechung (z. B. WebSocket-Reconnect) können Aufträge inzwischen fertig sein.
    for (const j of jobs.value) settle(j)
  }

  function settle(j: Job) {
    if (j.status !== 'succeeded' && j.status !== 'failed') return
    const list = waiters.get(j.id)
    if (!list) return
    waiters.delete(j.id)
    stopPoll(j.id)
    for (const w of list) {
      if (j.status === 'succeeded') w.resolve(j)
      else w.reject(new Error(j.log.trim().split('\n').pop() || 'Auftrag fehlgeschlagen'))
    }
  }

  function update(j: Job) {
    const i = jobs.value.findIndex((x) => x.id === j.id)
    if (i >= 0) jobs.value[i] = j
    else jobs.value.unshift(j)
    settle(j)
  }

  function stopPoll(id: string) {
    const t = pollers.get(id)
    if (t) clearInterval(t)
    pollers.delete(id)
  }

  /** Fragt einen Auftrag ab, solange jemand auf ihn wartet (Rückfall, falls ein Ereignis verloren ging). */
  function startPoll(id: string) {
    if (pollers.has(id)) return
    pollers.set(
      id,
      setInterval(() => {
        api
          .get<Job>(`/api/v1/jobs/${encodeURIComponent(id)}`)
          .then(update)
          .catch(() => {
            /* Server startet evtl. gerade neu: beim nächsten Takt erneut */
          })
      }, TRACK_POLL_MS),
    )
  }

  /** Wartet, bis ein Job abgeschlossen ist (Aktualisierung kommt per WebSocket, zusätzlich Abfrage als Rückfall). */
  function track(id: string): Promise<Job> {
    const known = jobs.value.find((j) => j.id === id)
    return new Promise((resolve, reject) => {
      const list = waiters.get(id) ?? []
      list.push({ resolve, reject })
      waiters.set(id, list)
      if (known) settle(known)
      if (waiters.has(id)) startPoll(id)
    })
  }

  return { jobs, running, load, update, track }
})
