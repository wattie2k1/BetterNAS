import { api } from './client'

export interface UpdatePackage {
  name: string
  current: string
  candidate: string
}

export interface UpdateRun {
  state: 'none' | 'running' | 'succeeded' | 'failed'
  log: string
}

export type UpdateChannel = 'stable' | 'testing'

export interface AutoRun {
  /** Zeitpunkt (RFC 3339) */
  at: string
  packages: string[]
  result: 'ok' | 'failed' | 'unknown'
}

export interface AutoUpdates {
  enabled: boolean
  lastRun: AutoRun | null
}

export interface UpdateOverview {
  packages: UpdatePackage[]
  checkedAt: string
  run: UpdateRun
  /** Kanal der BetterNAS-Paketquelle; leer = keine Quelle eingerichtet. */
  channel: '' | UpdateChannel
  /** Automatische Updates (unattended-upgrades). */
  auto: AutoUpdates
}

export const updatesApi = {
  get: () => api.get<UpdateOverview>('/api/v1/updates'),
  run: () => api.get<UpdateRun>('/api/v1/updates/run'),
  check: () => api.post<{ jobId: string }>('/api/v1/updates/check'),
  setChannel: (channel: UpdateChannel) => api.put<void>('/api/v1/updates/channel', { channel }),
  setAuto: (enabled: boolean) => api.put<void>('/api/v1/updates/auto', { enabled }),
  install: () => api.post<{ unit: string }>('/api/v1/updates/install'),
  version: async () => (await api.get<{ version: string }>('/api/v1/system/info')).version,
}
