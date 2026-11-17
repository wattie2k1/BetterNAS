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
  standby?: boolean
  reallocatedSectors?: number
  pendingSectors?: number
}

/** Erlaubte Ruhezeiten in Minuten (0 = aus). */
export const SPINDOWN_CHOICES = [0, 5, 10, 20, 30, 60, 120, 240] as const
export const spindownLabel = (m: number) => (m === 0 ? 'Aus' : m < 60 ? `${m} Min.` : `${m / 60} Std.`)

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
  /** Eingestellte Ruhezeit in Minuten (0 = aus). */
  spindown?: number
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
  role?: 'data' | 'parity'
  confirmSerial: string
  confirmToken: string
}

export interface PoolInput {
  name: string
  branches: string[]
  createPolicy: string
  minFree: string
}

export interface SnapraidSettings {
  syncSchedule: string
  scrubSchedule: string
  scrubPercent: number
  scrubOlderDays: number
  deleteThreshold: number
}

export interface SnapraidInput extends SnapraidSettings {
  parity: string[]
}

export interface SnapraidRun {
  action: string
  result: 'ok' | 'errors' | 'failed' | 'blocked'
  message?: string
  at: string
  removed?: number
}

export interface SnapraidDiff {
  equal: number
  added: number
  removed: number
  updated: number
  moved: number
  copied: number
}

export interface SnapraidStatus {
  enabled: boolean
  running: boolean
  lastSync?: SnapraidRun
  lastScrub?: SnapraidRun
  diff?: SnapraidDiff
  diffAt?: string
  log?: string
}

export const SNAPRAID_DEFAULTS: SnapraidSettings = {
  syncSchedule: '*-*-* 03:00:00',
  scrubSchedule: 'Sun *-*-* 05:00:00',
  scrubPercent: 8,
  scrubOlderDays: 10,
  deleteThreshold: 500,
}

/** Anzahl der noch nicht durch Parität gesicherten Änderungen (wie Diff.Unprotected im Backend). */
export const unprotected = (d: SnapraidDiff) => d.added + d.removed + d.updated + d.moved + d.copied

export interface PoolView extends PoolInput {
  path: string
  sizeBytes: number
  freeBytes: number
  mounted: boolean
  snapraid?: SnapraidInput
}

/** Identisch zur Backend-Regel (storage.PoolNameRe). */
export const POOL_NAME_RE = /^[a-z][a-z0-9-]{0,31}$/
export const MIN_FREE_RE = /^[0-9]{1,6}[KMG]$/

/** Wandelt "20G" in Bytes um (1024er-Einheiten wie mergerfs); 0 bei ungültiger Eingabe. */
export function minFreeBytes(v: string): number {
  const m = /^([0-9]{1,6})([KMG])$/.exec(v)
  if (!m) return 0
  return Number(m[1]) * { K: 1024, M: 1024 ** 2, G: 1024 ** 3 }[m[2] as 'K' | 'M' | 'G']
}

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
  importDisk: (id: string, role?: 'data' | 'parity') =>
    api.post<JobRef>(`/api/v1/disks/${enc(id)}/import`, role ? { role } : undefined),
  setSpindown: (id: string, minutes: number) => api.put<void>(`/api/v1/disks/${enc(id)}/spindown`, { minutes }),
  release: (id: string) => api.post<JobRef>(`/api/v1/disks/${enc(id)}/release`),
  createPool: (p: PoolInput) => api.post<JobRef>('/api/v1/pools', p),
  updatePool: (name: string, p: PoolInput) => api.patch<JobRef>(`/api/v1/pools/${enc(name)}`, p),
  deletePool: (name: string) => api.del<JobRef>(`/api/v1/pools/${enc(name)}`),
  enableSnapraid: (pool: string, in_: SnapraidInput) => api.put<JobRef>(`/api/v1/pools/${enc(pool)}/snapraid`, in_),
  disableSnapraid: (pool: string) => api.del<JobRef>(`/api/v1/pools/${enc(pool)}/snapraid`),
  snapraidStatus: (pool: string) => api.get<SnapraidStatus>(`/api/v1/pools/${enc(pool)}/snapraid/status`),
  runSnapraid: (pool: string, action: 'sync' | 'scrub' | 'diff', force = false) =>
    api.post<JobRef>(`/api/v1/pools/${enc(pool)}/snapraid/${action}`, { force }),
}
