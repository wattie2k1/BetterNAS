import { defineStore } from 'pinia'
import { ref } from 'vue'
import { updatesApi, type UpdateOverview } from '@/api/updates'
import { useJobsStore } from './jobs'

export const POLL_INTERVAL_MS = 2000

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms))

export const useUpdatesStore = defineStore('updates', () => {
  const overview = ref<UpdateOverview>({ packages: [], checkedAt: '', run: { state: 'none', log: '' } })
  const loading = ref(false)
  const checking = ref(false)
  const installing = ref(false)
  /** true, solange BetterNAS während des Updates nicht antwortet (API/Agent starten neu). */
  const restarting = ref(false)
  /** Nach einem Update mit neuer Version muss die Seite neu geladen werden. */
  const reloadNeeded = ref(false)
  const error = ref('')
  let polling = false
  /** Version der laufenden Oberfläche; wird beim Start der Installation gemerkt. */
  let startVersion = ''

  async function load() {
    loading.value = true
    try {
      overview.value = await updatesApi.get()
      error.value = ''
      if (overview.value.run.state === 'running') void pollUntilDone()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  async function check() {
    checking.value = true
    if (overview.value.run.state !== 'running') overview.value = { ...overview.value, run: { state: 'none', log: '' } }
    try {
      const { jobId } = await updatesApi.check()
      await useJobsStore().track(jobId)
    } finally {
      checking.value = false
      await load()
    }
  }

  /** Fragt den Lauf ab, bis er endet. Verbindungsfehler (Neustart von API/Agent) werden überbrückt. */
  async function pollUntilDone() {
    if (polling) return
    polling = true
    installing.value = true
    try {
      for (;;) {
        try {
          const run = await updatesApi.run()
          restarting.value = false
          overview.value = { ...overview.value, run }
          if (run.state !== 'running') break
        } catch {
          restarting.value = true
        }
        await sleep(POLL_INTERVAL_MS)
      }
    } finally {
      polling = false
      installing.value = false
    }
    let after = ''
    try {
      after = await updatesApi.version()
    } catch {
      /* Version nicht lesbar: nicht neu laden */
    }
    await load()
    if (after && startVersion && after !== startVersion) reloadNeeded.value = true
  }

  async function install() {
    try {
      startVersion = await updatesApi.version()
    } catch {
      startVersion = ''
    }
    await updatesApi.install()
    overview.value = { ...overview.value, run: { state: 'running', log: '' } }
    void pollUntilDone()
  }

  return { overview, loading, checking, installing, restarting, reloadNeeded, error, load, check, install }
})
