import { api } from './client'

export interface Partition {
  device: string
  fsType: string
  uuid: string
  label: string
  sizeBytes: number
  mountpoints: string[]
}

export interface SmartInfo {
  passed: boolean
  temperatureC: number
  powerOnHours: number
}

export interface DiskView {
  id: string
  device: string
  model: string
  serial: string
  wwn: string
  transport: string
  sizeBytes: number
  rotational: boolean
  system: boolean
  manageable: boolean
  reason?: string
  partitions: Partition[]
  role: 'unassigned' | 'data' | 'parity' | 'system'
  state: 'online' | 'missing'
  pool?: string
  fsType?: string
  smart?: SmartInfo
}

export interface FormatPreview {
  diskId: string
  device: string
  model: string
  serial: string
  sizeBytes: number
  partitions: Partition[]
  confirmToken: string
  expiresAt: string
}

export interface FormatRequest {
  fsType: 'ext4' | 'xfs'
  confirmSerial: string
  confirmToken: string
}

export interface PoolInput {
  name: string
  branches: string[]
  createPolicy: string
  minFree: string
}

export interface PoolView extends PoolInput {
  path: string
  sizeBytes: number
  freeBytes: number
  mounted: boolean
}

/** Identisch zur Backend-Regel (storage.PoolNameRe). */
export const POOL_NAME_RE = /^[a-z][a-z0-9-]{0,31}$/
export const MIN_FREE_RE = /^[0-9]{1,6}[KMG]$/

export const POLICIES = [
  { value: 'epmfs', label: 'Vorhandene Pfade, meist frei (empfohlen)', hint: 'Neue Dateien landen auf der Platte, auf der der Ordner schon liegt – mit dem meisten freien Platz.' },
  { value: 'mfs', label: 'Meist frei', hint: 'Neue Dateien landen immer auf der Platte mit dem meisten freien Platz.' },
  { value: 'lfs', label: 'Am wenigsten frei', hint: 'Füllt eine Platte nach der anderen; andere Platten können schlafen.' },
  { value: 'pfrd', label: 'Zufällig nach freiem Platz', hint: 'Verteilt zufällig, gewichtet nach freiem Platz.' },
]

type JobRef = { jobId: string }
const enc = encodeURIComponent

export const storageApi = {
  disks: () => api.get<DiskView[]>('/api/v1/disks'),
  pools: () => api.get<PoolView[]>('/api/v1/pools'),
  previewFormat: (id: string) => api.post<FormatPreview>(`/api/v1/disks/${enc(id)}/format/preview`),
  format: (id: string, req: FormatRequest) => api.post<JobRef>(`/api/v1/disks/${enc(id)}/format`, req),
  importDisk: (id: string) => api.post<JobRef>(`/api/v1/disks/${enc(id)}/import`),
  release: (id: string) => api.post<JobRef>(`/api/v1/disks/${enc(id)}/release`),
  createPool: (p: PoolInput) => api.post<JobRef>('/api/v1/pools', p),
  updatePool: (name: string, p: PoolInput) => api.patch<JobRef>(`/api/v1/pools/${enc(name)}`, p),
  deletePool: (name: string) => api.del<JobRef>(`/api/v1/pools/${enc(name)}`),
}
