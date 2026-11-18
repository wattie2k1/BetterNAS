import { defineStore } from 'pinia'
import { ref } from 'vue'
import { systemApi, type RebootStatus } from '@/api/system'

/** Wie oft der Server gefragt wird, ob ein Neustart nötig ist. */
export const REBOOT_POLL_MS = 60_000

export const useRebootStore = defineStore('reboot', () => {
  const status = ref<RebootStatus>({ required: false, packages: [] })
  let timer: ReturnType<typeof setInterval> | undefined

  /** Fragt den Zustand ab. Schlägt das fehl (z. B. API startet gerade neu), bleibt der letzte Stand stehen. */
  async function load() {
    try {
      status.value = await systemApi.rebootRequired()
    } catch {
      /* letzten Stand behalten */
    }
  }

  function start() {
    void load()
    if (!timer) timer = setInterval(() => void load(), REBOOT_POLL_MS)
  }

  function stop() {
    if (timer) clearInterval(timer)
    timer = undefined
  }

  return { status, load, start, stop }
})
