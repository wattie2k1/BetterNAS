import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  dockerApi,
  type ContainerAction,
  type ContainerInfo,
  type DockerStatus,
  type StackAction,
  type StackFiles,
  type StackInfo,
} from '@/api/docker'
import type { EventMessage } from '@/api/ws'
import { useJobsStore } from './jobs'

const message = (e: unknown) => (e instanceof Error ? e.message : String(e))

export const useDockerStore = defineStore('docker', () => {
  const status = ref<DockerStatus | null>(null)
  const stacks = ref<StackInfo[]>([])
  const containers = ref<ContainerInfo[]>([])
  const loading = ref(false)
  const error = ref('')

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

  function handleEvent(m: EventMessage) {
    if (m.type === 'docker.changed') void refresh()
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
