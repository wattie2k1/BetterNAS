import { defineStore } from 'pinia'
import { ref } from 'vue'
import { vmIconUrl, vmsApi, type VmAction, type VmInfo, type VmSpec, type VmStatus } from '@/api/vms'
import type { EventMessage } from '@/api/ws'
import { pollWhileVisible } from '@/utils/poll'

export const VM_POLL_MS = 5000

export const useVmsStore = defineStore('vms', () => {
  const status = ref<VmStatus | null>(null)
  const vms = ref<VmInfo[]>([])
  const loading = ref(false)
  const error = ref('')
  let stopTimer: (() => void) | undefined

  async function refresh() {
    loading.value = true
    try {
      status.value = await vmsApi.status()
      vms.value = status.value.active ? await vmsApi.list() : []
      error.value = ''
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  function handleEvent(m: EventMessage) {
    if (m.type === 'vm.changed') void refresh()
  }

  /** Aktualisiert die Liste regelmäßig, solange die VM-Seite offen ist. */
  function startPolling() {
    stopPolling()
    stopTimer = pollWhileVisible(() => void refresh(), VM_POLL_MS)
  }
  function stopPolling() {
    stopTimer?.()
    stopTimer = undefined
  }

  async function act(name: string, action: VmAction) {
    await vmsApi.action(name, action)
    await refresh()
  }
  async function remove(name: string, disks: boolean) {
    await vmsApi.remove(name, disks)
    await refresh()
  }
  async function create(spec: VmSpec, start: boolean) {
    await vmsApi.create(spec, start)
    await refresh()
  }

  async function clone(name: string, newName: string) {
    await vmsApi.clone(name, newName)
    await refresh()
  }

  /** Icon-Adresse einer VM (null = keins gesetzt). */
  function iconUrlOf(name: string): string | null {
    const v = vms.value.find((x) => x.name === name)?.icon
    return v ? vmIconUrl(name, v) : null
  }

  return { iconUrlOf, status, vms, loading, error, refresh, handleEvent, startPolling, stopPolling, act, remove, create, clone }
})
