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

export const networkApi = {
  bridge: () => api.get<BridgeStatus>('/api/v1/network/bridge'),
  enableBridge: (iface: string) => api.post<void>('/api/v1/network/bridge/enable', { interface: iface }),
  confirmBridge: () => api.post<void>('/api/v1/network/bridge/confirm'),
  disableBridge: () => api.post<void>('/api/v1/network/bridge/disable'),
}
