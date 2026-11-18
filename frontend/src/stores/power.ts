import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { PowerAction } from '@/api/system'

export const PROBE_INTERVAL_MS = 2000
const PROBE_TIMEOUT_MS = 3000
/** Nach dieser Zeit ohne Antwort wird ein Hinweis angezeigt (gewartet wird weiter). */
const SLOW_AFTER_MS = 180_000

/** Lädt die Startseite neu; austauschbar für Tests. */
export const hooks = { reload: () => window.location.assign('/') }

export type PowerPhase = 'idle' | 'rebooting' | 'shuttingDown' | 'off'

/** Erreichbar = irgendeine HTTP-Antwort (auch 401); Netzwerkfehler und Zeitüberschreitung = nicht erreichbar. */
async function reachable(): Promise<boolean> {
  const ctl = new AbortController()
  const t = setTimeout(() => ctl.abort(), PROBE_TIMEOUT_MS)
  try {
    await fetch('/api/v1/system/info', { cache: 'no-store', credentials: 'same-origin', signal: ctl.signal })
    return true
  } catch {
    return false
  } finally {
    clearTimeout(t)
  }
}

export const usePowerStore = defineStore('power', () => {
  const phase = ref<PowerPhase>('idle')
  const sawDown = ref(false)
  const slow = ref(false)
  let timer: ReturnType<typeof setTimeout> | undefined
  let startedAt = 0

  async function probe() {
    const ok = await reachable()
    if (phase.value === 'idle') return
    if (!ok) {
      sawDown.value = true
      if (phase.value === 'shuttingDown') {
        phase.value = 'off'
        return
      }
    } else if (phase.value === 'rebooting' && sawDown.value) {
      stop()
      hooks.reload()
      return
    }
    if (Date.now() - startedAt > SLOW_AFTER_MS) slow.value = true
    if (phase.value !== 'off') timer = setTimeout(() => void probe(), PROBE_INTERVAL_MS)
  }

  /** Startet das Warten nach einer erfolgreich angeforderten Energie-Aktion. */
  function begin(action: PowerAction) {
    stop()
    phase.value = action === 'reboot' ? 'rebooting' : 'shuttingDown'
    sawDown.value = false
    slow.value = false
    startedAt = Date.now()
    timer = setTimeout(() => void probe(), PROBE_INTERVAL_MS)
  }

  function stop() {
    if (timer) clearTimeout(timer)
    timer = undefined
    phase.value = 'idle'
  }

  return { phase, sawDown, slow, begin, stop }
})
