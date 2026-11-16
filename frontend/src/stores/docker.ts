import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  dockerApi,
  type ContainerAction,
  type ContainerInfo,
  type ContainerStats,
  type ImageInfo,
  type NetworkInfo,
  type ResourceKind,
  type UpdateReport,
  type VolumeInfo,
  type DockerStatus,
  type StackAction,
  type StackFiles,
  type StackInfo,
} from '@/api/docker'
import type { EventMessage } from '@/api/ws'
import { useJobsStore } from './jobs'

const message = (e: unknown) => (e instanceof Error ? e.message : String(e))

export const STATS_INTERVAL_MS = 5000

export const useDockerStore = defineStore('docker', () => {
  const status = ref<DockerStatus | null>(null)
  const stacks = ref<StackInfo[]>([])
  const containers = ref<ContainerInfo[]>([])
  const loading = ref(false)
  const error = ref('')
  const stats = ref<Record<string, ContainerStats>>({})
  const updates = ref<UpdateReport>({ checkedAt: '', stacks: {} })
  const images = ref<ImageInfo[]>([])
  const networks = ref<NetworkInfo[]>([])
  const volumes = ref<VolumeInfo[]>([])
  let statsTimer: ReturnType<typeof setInterval> | undefined

  async function refresh() {
    loading.value = true
    try {
      status.value = await dockerApi.status()
      if (status.value.active) {
        ;[stacks.value, containers.value] = await Promise.all([dockerApi.stacks(), dockerApi.containers()])
      } else {
        // Ohne laufenden Docker-Dienst sind Stacks trotzdem als Dateien vorhanden.
        stacks.value = await dockerApi.stacks()
        containers.value = []
      }
      error.value = ''
    } catch (e) {
      error.value = message(e)
    } finally {
      loading.value = false
    }
  }

  async function loadStats() {
    try {
      const list = await dockerApi.stats()
      stats.value = Object.fromEntries(list.map((s) => [s.id, s]))
    } catch {
      /* Auslastung ist nur eine Zugabe: Fehler nicht anzeigen */
    }
  }

  /** Fragt die Auslastung regelmäßig ab, solange die Container-Seite offen ist. */
  function startStats() {
    stopStats()
    void loadStats()
    statsTimer = setInterval(() => void loadStats(), STATS_INTERVAL_MS)
  }

  function stopStats() {
    if (statsTimer) clearInterval(statsTimer)
    statsTimer = undefined
  }

  async function loadUpdates() {
    try {
      updates.value = await dockerApi.updates()
    } catch {
      /* ohne Ergebnis keine Markierungen */
    }
  }

  async function checkUpdates() {
    const { jobId } = await dockerApi.checkUpdates()
    try {
      await useJobsStore().track(jobId)
    } finally {
      await loadUpdates()
    }
  }

  async function loadResources() {
    try {
      ;[images.value, networks.value, volumes.value] = await Promise.all([dockerApi.images(), dockerApi.networks(), dockerApi.volumes()])
      error.value = ''
    } catch (e) {
      error.value = message(e)
    }
  }

  function handleEvent(m: EventMessage) {
    if (m.type === 'docker.changed') {
      void refresh()
      void loadUpdates()
    }
  }

  /** Startet eine Stack-Aktion und wartet auf den zugehörigen Auftrag. */
  async function run(name: string, action: StackAction) {
    const { jobId } = await dockerApi.run(name, action)
    try {
      await useJobsStore().track(jobId)
    } finally {
      await refresh()
    }
  }

  async function guarded<T>(fn: () => Promise<T>): Promise<T> {
    try {
      return await fn()
    } finally {
      await refresh()
    }
  }

  return {
    status,
    stacks,
    containers,
    loading,
    error,
    refresh,
    stats,
    updates,
    images,
    networks,
    volumes,
    startStats,
    stopStats,
    loadStats,
    loadUpdates,
    checkUpdates,
    loadResources,
    removeResource: async (kind: ResourceKind, ref: string) => {
      try {
        await dockerApi.removeResource(kind, ref)
      } finally {
        await loadResources()
      }
    },
    prune: async (kind: ResourceKind, all: boolean) => {
      try {
        return await dockerApi.prune(kind, all)
      } finally {
        await loadResources()
      }
    },
    handleEvent,
    run,
    create: (s: StackFiles) => guarded(() => dockerApi.create(s)),
    update: (s: StackFiles) => guarded(() => dockerApi.update(s)),
    remove: (name: string) => guarded(() => dockerApi.remove(name)),
    validate: (name: string) => dockerApi.validate(name),
    load: (name: string) => dockerApi.stack(name),
    containerAction: (id: string, action: ContainerAction) => guarded(() => dockerApi.containerAction(id, action)),
  }
})
