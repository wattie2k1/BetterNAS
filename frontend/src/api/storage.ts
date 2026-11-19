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
  selfTest?: SelfTest
  attributes?: SmartAttr[]
  wear?: SsdWear
}

/** SMART-Attribut (ATA) bzw. Gesundheitswert (NVMe: id 0, nur Name und Rohwert). */
export interface SmartAttr {
  id: number
  name: string
  value: number
  worst: number
  threshold: number
  raw: string
  prefail?: boolean
  failing?: boolean
}

/** Verschleiß einer SSD; Felder fehlen, wenn die SSD sie nicht meldet. */
export interface SsdWear {
  percentUsed?: number
  spareLeftPercent?: number
  bytesWritten?: number
}

/** Ab diesem Verschleiß warnt auch das Backend. */
export const SSD_WEAR_WARN_PERCENT = 90

export interface SelfTestResult {
  type: 'short' | 'long' | 'other'
  result: 'passed' | 'aborted' | 'failed'
  status: string
  powerOnHours?: number
}

export interface SelfTest {
  running: boolean
  remainingPercent?: number
  last?: SelfTestResult
}

/** Belegung des Dateisystems einer Platte. */
export interface DiskUsage {
  sizeBytes: number
  usedBytes: number
  freeBytes: number
}

/** Eigene Angaben des Nutzers zu einer Platte; Datumsfelder als JJJJ-MM-TT oder leer. */
export interface DiskNote {
  name: string
  notes: string
  purchased: string
  warrantyUntil: string
  /** Warnschwelle dieser Platte in °C; 0 = globale Schwelle aus den Benachrichtigungen. */
  tempWarnC?: number
}

/** Zustand der (rein lesenden) Dateisystem-Prüfung einer Platte. */
export interface FsCheck {
  running: boolean
  startedAt?: string
  finishedAt?: string
  result?: 'ok' | 'errors' | 'failed'
  /** Das Dateisystem war während der Prüfung eingehängt; Meldungen können dann Fehlalarme sein. */
  online?: boolean
  tool?: string
  output?: string
}

/** Lese-Benchmark: Leserate an Stellen der Platte (0 % = Anfang, 100 % = Ende). */
export interface BenchRun {
  at: string
  avgMBps: number
  minMBps: number
  maxMBps: number
  samples: { percent: number; mbps: number }[]
}
export interface BenchState {
  running: boolean
  /** Der letzte Lauf ist fehlgeschlagen. */
  error?: string
  last?: BenchRun
  /** Durchschnitte früherer Läufe, ältester zuerst, inklusive des letzten. */
  history?: { at: string; avgMBps: number }[]
}

/** Aufsummierte I/O-Zähler aller Blockgeräte (Name ohne /dev/); Raten bildet die Oberfläche aus zwei Messungen. */
export interface DiskIOStats {
  at: string
  devices: Record<string, { readBytes: number; writeBytes: number; busyMs: number }>
}

export type EraseMethod = 'zero' | 'discard'
export interface EraseRequest {
  method: EraseMethod
  confirmSerial: string
  confirmToken: string
}
/** Zustand des Löschvorgangs einer Platte (nur im Speicher des Servers). */
export interface EraseState {
  running: boolean
  method: EraseMethod
  writtenBytes: number
  totalBytes: number
  result?: 'done' | 'failed' | 'aborted'
  error?: string
}

/** Tagesstatistik des Energiezustands; day als JJJJ-MM-TT (Ortszeit des Geräts). */
export interface PowerDay {
  day: string
  activeMin: number
  standbyMin: number
  spinups: number
}

/** APM-Stufen für `hdparm -B` (0 = nicht ändern, 255 = APM aus). */
export const APM_CHOICES = [
  { value: 0, label: 'Unverändert' },
  { value: 64, label: 'Sparsam (64)' },
  { value: 128, label: 'Ausgewogen (128)' },
  { value: 192, label: 'Leistung (192)' },
  { value: 254, label: 'Maximale Leistung (254)' },
  { value: 255, label: 'Aus (255)' },
] as const

export interface TuningSettings {
  apm: number
  writeCache: '' | 'on' | 'off'
}

export interface TuningState {
  saved: TuningSettings
  current: { standby?: boolean; apm?: number; writeCache?: boolean }
}

export type SelfTestKind = 'short' | 'long'

export interface SelfTestSchedule {
  short: 'off' | 'weekly' | 'monthly'
  long: 'off' | 'monthly' | 'quarterly'
  hour: number
}

/** Messpunkt im SMART-Verlauf; ts in Unix-Sekunden. */
export interface HistoryPoint {
  ts: number
  temperatureC: number
  reallocatedSectors: number
  pendingSectors: number
  powerOnHours: number
}

export const SELFTEST_KIND_LABEL: Record<string, string> = { short: 'Kurztest', long: 'Langtest', other: 'Test' }

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
  usage?: DiskUsage
  note?: DiskNote
  /** Gerade angeschlossen und noch nicht eingerichtet (Hinweis „Neue Platte erkannt“). */
  new?: boolean
  /** Die Aktivitäts-LED blinkt gerade, damit man die Platte findet. */
  identifying?: boolean
  fsCheck?: FsCheck
  erase?: EraseState
  bench?: BenchState
  /** Eingestellte Ruhezeit in Minuten (0 = aus). */
  spindown?: number
}

export interface FormatPreview {
  diskId: string
  transport?: string
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
  unrecoverable?: number
}

/** Offene Wiederherstellung einer ersetzten Platte (Sync/Prüfung sind gesperrt). */
export interface SnapraidRecovery {
  disk: string
  state: 'open'
  since: string
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
  lastFix?: SnapraidRun
  recovery?: SnapraidRecovery
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
  diskIO: () => api.get<DiskIOStats>('/api/v1/disks/io'),
  pools: () => api.get<PoolView[]>('/api/v1/pools'),
  previewErase: (id: string) => api.post<FormatPreview>(`/api/v1/disks/${enc(id)}/erase/preview`),
  startErase: (id: string, req: EraseRequest) => api.post<void>(`/api/v1/disks/${enc(id)}/erase`, req),
  abortErase: (id: string) => api.del<void>(`/api/v1/disks/${enc(id)}/erase`),
  previewFormat: (id: string) => api.post<FormatPreview>(`/api/v1/disks/${enc(id)}/format/preview`),
  format: (id: string, req: FormatRequest) => api.post<JobRef>(`/api/v1/disks/${enc(id)}/format`, req),
  importDisk: (id: string, role?: 'data' | 'parity') =>
    api.post<JobRef>(`/api/v1/disks/${enc(id)}/import`, role ? { role } : undefined),
  powerStats: (id: string, days: number) => api.get<PowerDay[]>(`/api/v1/disks/${enc(id)}/power?days=${days}`),
  tuning: (id: string) => api.get<TuningState>(`/api/v1/disks/${enc(id)}/tuning`),
  setTuning: (id: string, t: TuningSettings) => api.put<void>(`/api/v1/disks/${enc(id)}/tuning`, t),
  setNote: (id: string, n: DiskNote) => api.put<void>(`/api/v1/disks/${enc(id)}/note`, n),
  dismissNew: (id: string) => api.post<void>(`/api/v1/disks/${enc(id)}/dismiss`),
  startBench: (id: string) => api.post<void>(`/api/v1/disks/${enc(id)}/bench`),
  startFsCheck: (id: string) => api.post<void>(`/api/v1/disks/${enc(id)}/fscheck`),
  startIdentify: (id: string) => api.post<void>(`/api/v1/disks/${enc(id)}/identify`),
  stopIdentify: (id: string) => api.del<void>(`/api/v1/disks/${enc(id)}/identify`),
  startSelfTest: (id: string, kind: SelfTestKind) => api.post<void>(`/api/v1/disks/${enc(id)}/selftest`, { kind }),
  abortSelfTest: (id: string) => api.del<void>(`/api/v1/disks/${enc(id)}/selftest`),
  selfTestSchedule: () => api.get<SelfTestSchedule>('/api/v1/disks/selftest/schedule'),
  saveSelfTestSchedule: (s: SelfTestSchedule) => api.put<void>('/api/v1/disks/selftest/schedule', s),
  history: (id: string, days: number) => api.get<HistoryPoint[]>(`/api/v1/disks/${enc(id)}/history?days=${days}`),
  setSpindown: (id: string, minutes: number) => api.put<void>(`/api/v1/disks/${enc(id)}/spindown`, { minutes }),
  release: (id: string) => api.post<JobRef>(`/api/v1/disks/${enc(id)}/release`),
  createPool: (p: PoolInput) => api.post<JobRef>('/api/v1/pools', p),
  updatePool: (name: string, p: PoolInput) => api.patch<JobRef>(`/api/v1/pools/${enc(name)}`, p),
  deletePool: (name: string) => api.del<JobRef>(`/api/v1/pools/${enc(name)}`),
  enableSnapraid: (pool: string, in_: SnapraidInput) => api.put<JobRef>(`/api/v1/pools/${enc(pool)}/snapraid`, in_),
  disableSnapraid: (pool: string) => api.del<JobRef>(`/api/v1/pools/${enc(pool)}/snapraid`),
  replaceDisk: (pool: string, oldDisk: string, newDisk: string) =>
    api.post<JobRef>(`/api/v1/pools/${enc(pool)}/snapraid/replace`, { oldDisk, newDisk }),
  retryRecovery: (pool: string) => api.post<JobRef>(`/api/v1/pools/${enc(pool)}/snapraid/recovery/retry`),
  cancelRecovery: (pool: string) => api.del<void>(`/api/v1/pools/${enc(pool)}/snapraid/recovery`),
  snapraidStatus: (pool: string) => api.get<SnapraidStatus>(`/api/v1/pools/${enc(pool)}/snapraid/status`),
  runSnapraid: (pool: string, action: 'sync' | 'scrub' | 'diff', force = false) =>
    api.post<JobRef>(`/api/v1/pools/${enc(pool)}/snapraid/${action}`, { force }),
}
