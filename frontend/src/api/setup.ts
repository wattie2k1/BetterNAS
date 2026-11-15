import { api } from './client'

export interface SetupStatus {
  completed: boolean
  hostname: string
}

export const setupApi = {
  status: () => api.get<SetupStatus>('/api/v1/setup'),
  complete: () => api.post<void>('/api/v1/setup/complete'),
}
