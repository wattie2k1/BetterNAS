import { defineStore } from 'pinia'
import { ref } from 'vue'
import { backupApi, type BackupConfig, type BackupInfo, type BackupStatus, type RestorePreview } from '@/api/backup'
import { useJobsStore } from '@/stores/jobs'
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

  const config = ref<BackupConfig>({ enabled: true, time: '02:30', keep: 7, dir: '', appdata: false })
  const status = ref<BackupStatus | null>(null)

  async function loadStatus() {
    try {
      status.value = await backupApi.status()
    } catch (e) {
      error.value = message(e)
    }
  }

  async function loadConfig() {
    try {
      config.value = await backupApi.getConfig()
    } catch (e) {
      error.value = message(e)
    }
  }

  async function saveConfig(c: BackupConfig) {
    config.value = await backupApi.setConfig(c)
    await Promise.all([load(), loadStatus()])
  }

  /** Sichert appdata als Auftrag und wartet auf das Ende. */
  async function createAppdata() {
    const { jobId } = await backupApi.createAppdata()
    await useJobsStore().track(jobId)
    await load()
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

  return { backups, config, status, loading, error, restarting, load, loadConfig, loadStatus, saveConfig, create, createAppdata, remove, previewUpload, previewStored, restore, handleEvent }
})
