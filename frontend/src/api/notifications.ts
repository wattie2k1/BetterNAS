import { api } from './client'

export interface MailConfig {
  enabled: boolean
  host: string
  port: number
  security: 'none' | 'starttls' | 'tls'
  username: string
  /** Nur beim Speichern gesendet; leer = gespeichertes Passwort behalten. */
  password?: string
  passwordSet?: boolean
  from: string
  to: string[]
}

export interface NtfyConfig {
  enabled: boolean
  server: string
  topic: string
  /** Nur beim Speichern gesendet; leer = gespeichertes Token behalten. */
  token?: string
  tokenSet?: boolean
}

export interface NotifyConfig {
  mail: MailConfig
  ntfy: NtfyConfig
  tempWarnC: number
}

export interface ChannelResult {
  channel: 'mail' | 'ntfy'
  ok: boolean
  error?: string
}

export interface HistoryEntry {
  id: number
  ts: string
  level: 'info' | 'warn' | 'crit'
  title: string
  body: string
  results: ChannelResult[]
}

export interface ActiveAlert {
  key: string
  level: 'info' | 'warn' | 'crit'
  title: string
  body: string
  since: string
}

export const notificationsApi = {
  config: () => api.get<NotifyConfig>('/api/v1/notifications/config'),
  saveConfig: (c: NotifyConfig) => api.put<NotifyConfig>('/api/v1/notifications/config', c),
  test: (channel: 'mail' | 'ntfy') => api.post<void>('/api/v1/notifications/test', { channel }),
  history: (limit = 50) => api.get<HistoryEntry[]>(`/api/v1/notifications/history?limit=${limit}`),
  alerts: () => api.get<ActiveAlert[]>('/api/v1/alerts'),
}
