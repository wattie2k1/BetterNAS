import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import {
  dockerApi,
  stackIconUrl,
  type ContainerAction,
  type ContainerInfo,
  type ContainerStats,
  type ImageInfo,
  type NetworkInfo,
  type ResourceKind,
  type AutoUpdateConfig,
  type RunOptions,
  type UpdateReport,
  type VolumeInfo,
  type DockerStatus,
  type DockerPaths,
  type StackAction,
  type StackFiles,
  type StackInfo,
} from '@/api/docker'
import type { EventMessage } from '@/api/ws'
import { pollWhileVisible } from '@/utils/poll'
import { setRoots } from '@/utils/appTemplates'
import { useJobsStore } from './jobs'

const message = (e: unknown) => (e instanceof Error ? e.message : String(e))

export const STATS_INTERVAL_MS = 5000

export const useDockerStore = defineStore('docker', () => {
  const status = ref<DockerStatus | null>(null)
  const paths = ref<DockerPaths | null>(null)
  /** Docker ist eingerichtet (Speicherorte gesetzt). Solange der Status unbekannt ist, gilt es als eingerichtet. */
  const configured = computed(() => status.value?.configured !== false)
  const stacks = ref<StackInfo[]>([])
  const containers = ref<ContainerInfo[]>([])
  const loading = ref(false)
  const error = ref('')
  const stats = ref<Record<string, ContainerStats>>({})
  const updates = ref<UpdateReport>({ checkedAt: '', stacks: {} })
  const autoUpdate = ref<AutoUpdateConfig>({ enabled: false, time: '04:00', stacks: [], backup: true })
  const images = ref<ImageInfo[]>([])
  const networks = ref<NetworkInfo[]>([])
  const volumes = ref<VolumeInfo[]>([])
  let stopStatsTimer: (() => void) | undefined

  async function refresh() {
    loading.value = true
    try {
      status.value = await dockerApi.status()
      if (status.value.configured === false) {
        // Ohne Speicherorte ist Docker gesperrt: nichts weiter laden.
        stacks.value = []
        containers.value = []
        error.value = ''
        return
      }
      if (!paths.value?.configured) await loadPaths()
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

  /** Lädt die Speicherorte und gibt sie an die Vorlagen weiter. */
  async function loadPaths() {
    try {
      paths.value = await dockerApi.paths()
      if (paths.value.configured) setRoots(paths.value)
    } catch {
      /* Die Speicherorte sind nur für Vorgaben nötig: Fehler nicht anzeigen */
    }
  }

  /** Speichert die Speicherorte (Ersteinrichtung oder Änderung) und lädt den Zustand neu. */
  async function savePaths(p: { appdata: string; dockerdata: string }): Promise<DockerPaths> {
    const r = await dockerApi.setPaths(p)
    paths.value = r
    if (r.configured) setRoots(r)
    await refresh()
    return r
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
    stopStatsTimer = pollWhileVisible(() => void loadStats(), STATS_INTERVAL_MS)
  }

  function stopStats() {
    stopStatsTimer?.()
    stopStatsTimer = undefined
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

  async function loadAutoUpdate() {
    try {
      autoUpdate.value = await dockerApi.autoUpdate()
    } catch {
      /* ohne Einstellungen bleibt der Standard (aus) */
    }
  }

  async function saveAutoUpdate(c: AutoUpdateConfig) {
    autoUpdate.value = await dockerApi.setAutoUpdate(c)
  }

  /** Führt die automatischen Updates sofort aus und wartet auf den Auftrag. */
  async function runAutoUpdate() {
    const { jobId } = await dockerApi.runAutoUpdate()
    try {
      await useJobsStore().track(jobId)
    } finally {
      await Promise.all([refresh(), loadUpdates()])
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
  async function run(name: string, action: StackAction, opts?: RunOptions) {
    const { jobId } = await dockerApi.run(name, action, opts)
    try {
      await useJobsStore().track(jobId)
    } finally {
      await refresh()
    }
  }

  /** Icon-Adresse eines Stacks (null = keins gesetzt). */
  function iconUrlOf(stack: string): string | null {
    const v = stacks.value.find((s) => s.name === stack)?.icon
    return v ? stackIconUrl(stack, v) : null
  }

  /** Icon-Adresse für einen Container: Container erben das Icon ihres Stacks. */
  function iconUrlFor(c: ContainerInfo): string | null {
    return c.stack ? iconUrlOf(c.stack) : null
  }

  /** Wendet die im Dialog gewählte Icon-Änderung an, nachdem der Stack gespeichert wurde. */
  async function applyIcon(s: StackFiles) {
    const ch = s.icon
    if (!ch) return
    try {
      if (ch.kind === 'file') await dockerApi.setIconFile(s.name, ch.file)
      else if (ch.kind === 'url') await dockerApi.setIconUrl(s.name, ch.url)
      else await dockerApi.removeIcon(s.name)
    } catch (e) {
      throw new Error(`Der Stack wurde gespeichert, das Icon aber nicht: ${message(e)}`)
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
    paths,
    configured,
    loadPaths,
    savePaths,
    stacks,
    containers,
    loading,
    error,
    refresh,
    stats,
    updates,
    autoUpdate,
    loadAutoUpdate,
    saveAutoUpdate,
    runAutoUpdate,
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
    create: (s: StackFiles) => guarded(async () => { await dockerApi.create(s); await applyIcon(s) }),
    update: (s: StackFiles) => guarded(async () => { await dockerApi.update(s); await applyIcon(s) }),
    iconUrlOf,
    iconUrlFor,
    remove: (name: string) => guarded(() => dockerApi.remove(name)),
    validate: (name: string) => dockerApi.validate(name),
    load: (name: string) => dockerApi.stack(name),
    containerAction: (id: string, action: ContainerAction) => guarded(() => dockerApi.containerAction(id, action)),
  }
})
