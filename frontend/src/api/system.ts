import { api } from './client'

export interface SystemSettings {
  hostname: string
  timezone: string
  ntp: boolean
  ntpSynced: boolean
  ntpServers: string[]
  time: string
}

export type SystemSettingsChange = Partial<Pick<SystemSettings, 'hostname' | 'timezone' | 'ntp' | 'ntpServers'>>

export interface CertInfo {
  subject: string
  issuer: string
  dnsNames: string[]
  ipAddresses: string[]
  notBefore: string
  notAfter: string
  fingerprint: string
  selfSigned: boolean
  expired: boolean
}

export type PowerAction = 'reboot' | 'poweroff'

export const systemApi = {
  settings: () => api.get<SystemSettings>('/api/v1/system/settings'),
  timezones: () => api.get<string[]>('/api/v1/system/timezones'),
  update: (c: SystemSettingsChange) => api.put<void>('/api/v1/system/settings', c),
  power: (action: PowerAction, confirm: string) => api.post<void>('/api/v1/system/power', { action, confirm }),
  certificate: () => api.get<CertInfo>('/api/v1/system/certificate'),
  regenerateCertificate: () => api.post<CertInfo>('/api/v1/system/certificate/regenerate'),
  installCertificate: (cert: string, key: string) => api.put<CertInfo>('/api/v1/system/certificate', { cert, key }),
}
