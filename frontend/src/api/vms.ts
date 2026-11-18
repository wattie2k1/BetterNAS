import { api, ApiError, getCsrfToken } from './client'
import { wsUrl } from './docker'

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

export interface VmConfig {
  name: string
  state: VmState
  vcpus: number
  memoryMiB: number
  autostart: boolean
  firmware: 'uefi' | 'bios'
  secureBoot: boolean
  tpm: boolean
  guestAgent: boolean
  network: string
  hasCdrom: boolean
  media: string
  disks: string[]
}

/** Änderbare Werte (die VM muss ausgeschaltet sein). */
export interface VmConfigUpdate {
  vcpus: number
  memoryMiB: number
  secureBoot: boolean
  network: string
  media: string
  guestAgent: boolean
}

export interface VmDisk {
  path: string
  sizeGiB: number
  usedBytes: number
  virtualSize: number
}

export interface VmDiskResize {
  path: string
  sizeGiB: number
  deleteSnapshots: boolean
}

export interface VmSnapshot {
  name: string
  created: string
  state: string
}

/** Wie `vm.NameRe` im Backend. */
export const VM_NAME_RE = /^[a-z0-9][a-z0-9_-]{0,39}$/
/** Wie `vm.SnapshotRe` im Backend. */
export const SNAPSHOT_NAME_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$/
/** Wie `vm.ISORe` / `vm.ImageRe` im Backend. */
export const ISO_FILE_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,99}\.(iso|qcow2)$/
/** Größte erlaubte Upload-Datei (wie `vm.MaxUploadBytes`). */
export const MAX_UPLOAD_BYTES = 50 * 1024 ** 3

const enc = encodeURIComponent

export const vmsApi = {
  status: () => api.get<VmStatus>('/api/v1/vms/status'),
  list: () => api.get<VmInfo[]>('/api/v1/vms'),
  networks: () => api.get<string[]>('/api/v1/vms/networks'),
  isos: () => api.get<string[]>('/api/v1/vms/isos'),
  create: (spec: VmSpec, start: boolean) => api.post<{ name: string }>('/api/v1/vms', { ...spec, start }),
  action: (name: string, action: VmAction) => api.post<void>(`/api/v1/vms/${enc(name)}/${action}`),
  interfaces: () => api.get<string[]>('/api/v1/vms/interfaces'),
  snapshots: (name: string) => api.get<VmSnapshot[]>(`/api/v1/vms/${enc(name)}/snapshots`),
  snapshotCreate: (name: string, snap: string) => api.post<{ name: string }>(`/api/v1/vms/${enc(name)}/snapshots`, { name: snap }),
  snapshotRevert: (name: string, snap: string) => api.post<void>(`/api/v1/vms/${enc(name)}/snapshots/${enc(snap)}/revert`),
  snapshotDelete: (name: string, snap: string) => api.del<void>(`/api/v1/vms/${enc(name)}/snapshots/${enc(snap)}`),
  clone: (name: string, newName: string) => api.post<{ name: string }>(`/api/v1/vms/${enc(name)}/clone`, { name: newName }),
  isoDelete: (file: string) => api.del<void>(`/api/v1/vms/isos/${enc(file)}`),
  consoleUrl: (name: string) => wsUrl(`/api/v1/vms/${enc(name)}/console`),
  /** Lädt eine ISO-/qcow2-Datei hoch (XMLHttpRequest wegen des Fortschritts). */
  isoUpload: (file: File, onProgress: (done: number, total: number) => void) =>
    new Promise<void>((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('PUT', `/api/v1/vms/isos/${enc(file.name)}`)
      xhr.setRequestHeader('X-CSRF-Token', getCsrfToken())
      xhr.upload.onprogress = (e) => e.lengthComputable && onProgress(e.loaded, e.total)
      xhr.onerror = () => reject(new ApiError(0, 'network', 'BetterNAS ist nicht erreichbar'))
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) return resolve()
        let e: { code?: string; message?: string } | undefined
        try {
          e = JSON.parse(xhr.responseText).error
        } catch {
          /* keine JSON-Antwort */
        }
        reject(new ApiError(xhr.status, e?.code ?? 'internal', e?.message ?? `Fehler ${xhr.status}`))
      }
      xhr.send(file)
    }),
  config: (name: string) => api.get<VmConfig>(`/api/v1/vms/${enc(name)}/config`),
  configure: (name: string, u: VmConfigUpdate) => api.post<void>(`/api/v1/vms/${enc(name)}/config`, u),
  disks: (name: string) => api.get<VmDisk[]>(`/api/v1/vms/${enc(name)}/disks`),
  diskResize: (name: string, r: VmDiskResize) => api.post<void>(`/api/v1/vms/${enc(name)}/disks/resize`, r),
  remove: (name: string, disks: boolean) => api.del<void>(`/api/v1/vms/${enc(name)}${disks ? '?disks=1' : ''}`),
}
