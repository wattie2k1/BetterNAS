import { api } from './client'

export type FirewallMode = 'all' | 'lan' | 'custom'

export interface FirewallRule {
  mode: FirewallMode
  networks: string[]
}

export interface FirewallConfig {
  enabled: boolean
  rules: Record<string, FirewallRule>
}

export interface FirewallService {
  id: string
  label: string
  ports: string[]
  required: boolean
}

export interface FirewallStatus {
  config: FirewallConfig
  services: FirewallService[]
  active: boolean
  pending: boolean
  deadline: number
  sshPort: number
}

export interface SshSettings {
  port: number
  passwordAuth: boolean
  permitRootLogin: 'no' | 'prohibit-password'
  socketActivated: boolean
  active: boolean
}

export interface SshKey {
  type: string
  fingerprint: string
  comment: string
}

const enc = encodeURIComponent

export const firewallApi = {
  status: () => api.get<FirewallStatus>('/api/v1/firewall'),
  apply: (c: FirewallConfig) => api.put<void>('/api/v1/firewall', c),
  confirm: () => api.post<void>('/api/v1/firewall/confirm'),
  revert: () => api.post<void>('/api/v1/firewall/revert'),
}

export const sshApi = {
  get: () => api.get<SshSettings>('/api/v1/settings/ssh'),
  set: (v: Pick<SshSettings, 'port' | 'passwordAuth' | 'permitRootLogin'>) => api.put<void>('/api/v1/settings/ssh', v),
  keys: (user: string) => api.get<SshKey[]>(`/api/v1/settings/ssh/keys/${enc(user)}`),
  addKey: (user: string, key: string) => api.post<void>(`/api/v1/settings/ssh/keys/${enc(user)}`, { key }),
  removeKey: (user: string, fingerprint: string) => api.del<void>(`/api/v1/settings/ssh/keys/${enc(user)}?fingerprint=${enc(fingerprint)}`),
}
