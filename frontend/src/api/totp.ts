import { api } from './client'

export interface TotpSetup {
  secret: string
  uri: string
}

export const totpApi = {
  status: () => api.get<{ enabled: boolean }>('/api/v1/auth/totp'),
  setup: () => api.post<TotpSetup>('/api/v1/auth/totp/setup'),
  enable: (code: string) => api.post<{ recoveryCodes: string[] }>('/api/v1/auth/totp/enable', { code }),
  disable: (code: string) => api.post<void>('/api/v1/auth/totp/disable', { code }),
  reset: (user: string) => api.del<void>(`/api/v1/users/${encodeURIComponent(user)}/totp`),
}
