import { api } from './client'

export interface BridgeStatus {
  active: boolean
  interface: string
  /** Eingerichtet, aber noch nicht bestätigt (automatischer Rückfall läuft). */
  pending: boolean
  /** Frist der Bestätigung (Unix-Sekunden). */
  deadline: number
  /** IPv4-Adressen von br0, wenn die Bridge aktiv ist. */
  addresses: string[]
  hostname: string
  candidates: string[]
}

export interface IpConfig {
  mode: 'dhcp' | 'static'
  /** IPv4 als CIDR, z. B. 192.168.1.10/24 */
  address: string
  gateway: string
  dns: string[]
  domains: string[]
}

export interface IpStatus {
  interface: string
  bridgeActive: boolean
  config: IpConfig
  current: { addresses: string[]; gateway: string; dns: string[]; domains: string[] }
  interfaces: { name: string; state: string; mac: string; addresses: string[] }[]
  /** Geändert, aber noch nicht bestätigt (automatischer Rückfall läuft). */
  pending: boolean
  deadline: number
  hostname: string
}

export const networkApi = {
  status: () => api.get<IpStatus>('/api/v1/network'),
  configure: (cfg: IpConfig) => api.put<void>('/api/v1/network', cfg),
  confirm: () => api.post<void>('/api/v1/network/confirm'),
  revert: () => api.post<void>('/api/v1/network/revert'),
  bridge: () => api.get<BridgeStatus>('/api/v1/network/bridge'),
  enableBridge: (iface: string) => api.post<void>('/api/v1/network/bridge/enable', { interface: iface }),
  confirmBridge: () => api.post<void>('/api/v1/network/bridge/confirm'),
  disableBridge: () => api.post<void>('/api/v1/network/bridge/disable'),
}
