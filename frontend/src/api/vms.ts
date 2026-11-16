import { api } from './client'

export interface VmStatus {
  installed: boolean
  active: boolean
  kvm: boolean
}

export type VmState = 'running' | 'shutoff' | 'paused' | 'shuttingdown' | 'crashed' | 'other'

export interface VmInfo {
  name: string
  uuid: string
  state: VmState
  vcpus: number
  memoryMiB: number
  autostart: boolean
  disks: string[]
}

export type VmAction = 'start' | 'shutdown' | 'destroy' | 'reboot' | 'autostart' | 'noautostart'

export interface VmSpec {
  name: string
  os: 'linux' | 'windows'
  firmware: 'uefi' | 'bios'
  network: string
  iso: string
  driverIso: string
  vcpus: number
  memoryMiB: number
  diskGiB: number
  tpm: boolean
}

/** Wie `vm.NameRe` im Backend. */
export const VM_NAME_RE = /^[a-z0-9][a-z0-9_-]{0,39}$/

const enc = encodeURIComponent

export const vmsApi = {
  status: () => api.get<VmStatus>('/api/v1/vms/status'),
  list: () => api.get<VmInfo[]>('/api/v1/vms'),
  networks: () => api.get<string[]>('/api/v1/vms/networks'),
  isos: () => api.get<string[]>('/api/v1/vms/isos'),
  create: (spec: VmSpec, start: boolean) => api.post<{ name: string }>('/api/v1/vms', { ...spec, start }),
  action: (name: string, action: VmAction) => api.post<void>(`/api/v1/vms/${enc(name)}/${action}`),
  remove: (name: string, disks: boolean) => api.del<void>(`/api/v1/vms/${enc(name)}${disks ? '?disks=1' : ''}`),
}
