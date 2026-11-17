import { defineStore } from 'pinia'
import { ref } from 'vue'
import { backupApi, type BackupInfo, type RestorePreview } from '@/api/backup'
import type { EventMessage } from '@/api/ws'

export const RESTART_POLL_MS = 2000

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms))

export const useBackupStore = defineStore('backup', () => {
  const backups = ref<BackupInfo[]>([])
  const loading = ref(false)
  const error = ref('')
  /** true, sobald eine Wiederherstellung angestoßen wurde und BetterNAS neu startet. */
  const restarting = ref(false)

  const message = (e: unknown) => (e instanceof Error ? e.message : String(e))

  async function load() {
    loading.value = true
    try {
      backups.value = await backupApi.list()
      error.value = ''
    } catch (e) {
      error.value = message(e)
    } finally {
      loading.value = false
    }
  }

  async function create() {
    await backupApi.create()
    await load()
  }

  async function remove(name: string) {
    await backupApi.remove(name)
    await load()
  }

  const previewUpload = (file: Blob): Promise<RestorePreview> => backupApi.previewUpload(file)
  const previewStored = (name: string): Promise<RestorePreview> => backupApi.previewStored(name)

  /** Stößt die Wiederherstellung an und wartet, bis BetterNAS wieder antwortet (auch mit 401: dann ist es neu gestartet). */
  async function restore(token: string) {
    await backupApi.restore(token)
    restarting.value = true
    await sleep(3000) // der Agent startet die API erst in 2 s neu
    for (;;) {
      try {
        const res = await fetch('/api/v1/system/info', { credentials: 'same-origin' })
        if (res) break
      } catch {
        /* noch im Neustart */
      }
      await sleep(RESTART_POLL_MS)
    }
    window.location.assign('/')
  }

  function handleEvent(m: EventMessage) {
    if (m.type === 'backups.changed') void load()
  }

  return { backups, loading, error, restarting, load, create, remove, previewUpload, previewStored, restore, handleEvent }
})
