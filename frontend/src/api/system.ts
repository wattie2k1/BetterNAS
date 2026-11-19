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
  /** Herkunft des Zertifikats (fehlt in alten Servern). */
  mode?: 'selfsigned' | 'localca' | 'acme' | 'custom'
}

export interface RebootStatus {
  required: boolean
  reason?: string
  runningKernel?: string
  installedKernel?: string
  packages: string[]
}

export type PowerAction = 'reboot' | 'poweroff'

export const systemApi = {
  settings: () => api.get<SystemSettings>('/api/v1/system/settings'),
  timezones: () => api.get<string[]>('/api/v1/system/timezones'),
  update: (c: SystemSettingsChange) => api.put<void>('/api/v1/system/settings', c),
  rebootRequired: () => api.get<RebootStatus>('/api/v1/system/reboot-required'),
  power: (action: PowerAction, confirm: string) => api.post<void>('/api/v1/system/power', { action, confirm }),
  certificate: () => api.get<CertInfo>('/api/v1/system/certificate'),
  regenerateCertificate: () => api.post<CertInfo>('/api/v1/system/certificate/regenerate'),
  installCertificate: (cert: string, key: string) => api.put<CertInfo>('/api/v1/system/certificate', { cert, key }),
}

export interface ServiceState {
  unit: string
  label: string
  active: boolean
  enabled: boolean
  installed: boolean
}
export type ServiceAction = 'start' | 'stop' | 'restart'

export interface SmbGlobal {
  workgroup: string
  serverName: string
  minProtocol: string
}

export interface DockerSettings {
  dataRoot: string
  logMaxSize: string
  logMaxFile: number
  registryMirrors: string[]
  liveRestore: boolean
}

export const servicesApi = {
  list: () => api.get<ServiceState[]>('/api/v1/services'),
  act: (unit: string, action: ServiceAction) => api.post<void>(`/api/v1/services/${unit}/${action}`),
  smb: () => api.get<SmbGlobal>('/api/v1/settings/smb'),
  setSmb: (v: SmbGlobal) => api.put<void>('/api/v1/settings/smb', v),
  docker: () => api.get<DockerSettings>('/api/v1/settings/docker'),
  setDocker: (v: Omit<DockerSettings, 'dataRoot'>) => api.put<void>('/api/v1/settings/docker', v),
}
