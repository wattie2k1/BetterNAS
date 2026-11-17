import { defineStore } from 'pinia'
import { ref } from 'vue'
import { notificationsApi, type ActiveAlert, type HistoryEntry, type NotifyConfig } from '@/api/notifications'
import type { EventMessage } from '@/api/ws'

export const useNotificationsStore = defineStore('notifications', () => {
  const config = ref<NotifyConfig | null>(null)
  const history = ref<HistoryEntry[]>([])
  const alerts = ref<ActiveAlert[]>([])
  const error = ref('')

  const message = (e: unknown) => (e instanceof Error ? e.message : String(e))

  async function loadConfig() {
    try {
      config.value = await notificationsApi.config()
      error.value = ''
    } catch (e) {
      error.value = message(e)
    }
  }

  async function loadHistory() {
    try {
      history.value = await notificationsApi.history()
    } catch {
      /* Verlauf ist optional */
    }
  }

  async function loadAlerts() {
    try {
      alerts.value = await notificationsApi.alerts()
    } catch {
      alerts.value = []
    }
  }

  async function save(c: NotifyConfig) {
    config.value = await notificationsApi.saveConfig(c)
  }

  function handleEvent(m: EventMessage) {
    if (m.type === 'alerts.changed') void loadAlerts()
    if (m.type === 'notifications.changed') void loadHistory()
  }

  return { config, history, alerts, error, loadConfig, loadHistory, loadAlerts, save, handleEvent, test: notificationsApi.test }
})
