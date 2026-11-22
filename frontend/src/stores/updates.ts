import { defineStore } from 'pinia'
import { ref } from 'vue'
import { updatesApi, type UpdateChannel, type UpdateOverview } from '@/api/updates'
import { useJobsStore } from './jobs'
import { useRebootStore } from './reboot'

export const POLL_INTERVAL_MS = 2000

/** So lange darf BetterNAS beim Update nicht antworten, bevor die Abfrage aufgibt. */
export const MAX_UNREACHABLE_MS = 15 * 60_000

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms))

export const useUpdatesStore = defineStore('updates', () => {
  const overview = ref<UpdateOverview>({ packages: [], checkedAt: '', run: { state: 'none', log: '' }, channel: '', auto: { enabled: false, lastRun: null } })
  const loading = ref(false)
  const checking = ref(false)
  const installing = ref(false)
  /** true, solange BetterNAS während des Updates nicht antwortet (API/Agent starten neu). */
  const restarting = ref(false)
  /** Nach einem Update mit neuer Version muss die Seite neu geladen werden. */
  const reloadNeeded = ref(false)
  const error = ref('')
  const changingChannel = ref(false)
  const changingAuto = ref(false)
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
    let downSince = 0
    try {
      for (;;) {
        try {
          const run = await updatesApi.run()
          downSince = 0
          restarting.value = false
          overview.value = { ...overview.value, run }
          if (run.state !== 'running') break
        } catch {
          restarting.value = true
          downSince ||= Date.now()
          if (Date.now() - downSince > MAX_UNREACHABLE_MS) {
            error.value = 'BetterNAS antwortet seit längerer Zeit nicht mehr. Prüfe den Server und lade die Seite neu.'
            break
          }
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
    void useRebootStore().load()
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

  /** Stellt die Paketquelle auf einen anderen Kanal um und lädt die Übersicht neu. Fehler werden geworfen. */
  async function setChannel(channel: UpdateChannel) {
    changingChannel.value = true
    try {
      await updatesApi.setChannel(channel)
    } finally {
      changingChannel.value = false
      await load()
    }
  }

  /** Schaltet die automatischen Updates ein oder aus und lädt die Übersicht neu. Fehler werden geworfen. */
  async function setAuto(enabled: boolean) {
    changingAuto.value = true
    try {
      await updatesApi.setAuto(enabled)
    } finally {
      changingAuto.value = false
      await load()
    }
  }

  return { overview, loading, checking, installing, restarting, reloadNeeded, error, changingChannel, changingAuto, setAuto, load, check, install, setChannel }
})
