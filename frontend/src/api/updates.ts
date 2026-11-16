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

export interface UpdateOverview {
  packages: UpdatePackage[]
  checkedAt: string
  run: UpdateRun
}

export const updatesApi = {
  get: () => api.get<UpdateOverview>('/api/v1/updates'),
  run: () => api.get<UpdateRun>('/api/v1/updates/run'),
  check: () => api.post<{ jobId: string }>('/api/v1/updates/check'),
  install: () => api.post<{ unit: string }>('/api/v1/updates/install'),
  version: async () => (await api.get<{ version: string }>('/api/v1/system/info')).version,
}
