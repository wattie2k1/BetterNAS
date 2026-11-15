import { defineStore } from 'pinia'
import { ref } from 'vue'
import { setupApi } from '@/api/setup'

export const SETUP_PATH = '/einrichtung'

export const useSetupStore = defineStore('setup', () => {
  /** null = noch nicht geladen. */
  const completed = ref<boolean | null>(null)
  const hostname = ref('betternas')

  async function load() {
    const s = await setupApi.status()
    completed.value = s.completed
    hostname.value = s.hostname
  }

  async function complete() {
    await setupApi.complete()
    completed.value = true
  }

  /**
   * Leitet nach dem Anmelden einmal in den Einrichtungsassistenten, solange er nicht abgeschlossen ist.
   * Liefert den Zielpfad oder null. Fehler beim Laden blockieren die Navigation nicht.
   */
  async function firstRunRedirect(toPath: string): Promise<string | null> {
    if (completed.value !== null) return null
    try {
      await load()
    } catch {
      return null
    }
    return completed.value === false && toPath !== SETUP_PATH ? SETUP_PATH : null
  }

  function reset() {
    completed.value = null
  }

  return { completed, hostname, load, complete, firstRunRedirect, reset }
})
