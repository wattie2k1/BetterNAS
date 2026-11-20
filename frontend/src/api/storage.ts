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
  /** Als Ersatzplatte (Hot-Spare) vorgemerkt. */
  spare?: boolean
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

export type PoolType = 'mergerfs' | 'btrfs'
export type BtrfsProfile = 'single' | 'raid1' | 'raid1c3' | 'raid10'

export interface PoolInput {
  name: string
  /** mergerfs (Standard) | btrfs */
  type?: PoolType
  branches: string[]
  createPolicy: string
  minFree: string
  /** Nur btrfs. */
  profile?: BtrfsProfile
  compress?: '' | 'zstd:1' | 'zstd:3'
  /** Bestätigung beim Anlegen eines btrfs-Pools (die Platten werden gelöscht). */
  confirmToken?: string
  confirmName?: string
}

export interface BtrfsErrors {
  write: number
  read: number
  flush: number
  corruption: number
  generation: number
}

export interface BtrfsDevice {
  devId: number
  path?: string
  diskId?: string
  sizeBytes: number
  usedBytes: number
  missing: boolean
  errors: BtrfsErrors
}

export interface BtrfsScrub {
  running: boolean
  result?: string
  started?: string
  duration?: string
  corrected: number
  uncorrectable: number
  errors: number
}

export interface BtrfsOp {
  op: 'balance' | 'remove' | 'convert' | ''
  running: boolean
  result?: 'ok' | 'failed'
  /** 0–100, -1 = unbekannt */
  progress: number
  message?: string
}

export interface BtrfsReplace {
  state: 'never' | 'running' | 'finished' | 'canceled'
  percent: number
  writeErrs: number
  readErrs: number
}

export interface BtrfsStatus {
  mounted: boolean
  devices: BtrfsDevice[]
  missing: number
  degraded: boolean
  dataProfile?: string
  metadataProfile?: string
  rawBytes: number
  usableBytes: number
  freeBytes: number
  errors: number
  scrub?: BtrfsScrub
  op?: BtrfsOp
  replace?: BtrfsReplace
}

export interface PoolPreview {
  disks: FormatPreview[]
  confirmToken: string
  expiresAt: string
}

/** Eingabe zum Ersetzen, Hinzufügen oder Entfernen einer Platte eines btrfs-Pools. */
export interface BtrfsDiskInput {
  diskId: string
  oldDiskId?: string
  confirmToken?: string
  confirmName?: string
  balance?: boolean
}

export interface BtrfsScrubSchedule {
  interval: 'off' | 'weekly' | 'monthly' | 'quarterly'
  hour: number
}

export const SCRUB_INTERVALS = [
  { value: 'off', label: 'Aus' },
  { value: 'weekly', label: 'Wöchentlich (sonntags)' },
  { value: 'monthly', label: 'Monatlich (am 1.)' },
  { value: 'quarterly', label: 'Vierteljährlich' },
] as const

export const BTRFS_PROFILES: { value: BtrfsProfile; label: string; min: number; copies: number; hint: string }[] = [
  { value: 'single', label: 'Single – keine Ausfallsicherheit', min: 1, copies: 1, hint: 'Nutzt den ganzen Platz. Fällt eine Platte aus, gehen deren Daten verloren. Prüfsummen erkennen Fehler.' },
  { value: 'raid1', label: 'RAID1 – eine Platte darf ausfallen', min: 2, copies: 2, hint: 'Jede Datei liegt auf zwei Platten. Nutzbar ist etwa die Hälfte.' },
  { value: 'raid1c3', label: 'RAID1c3 – zwei Platten dürfen ausfallen', min: 3, copies: 3, hint: 'Jede Datei liegt auf drei Platten. Nutzbar ist etwa ein Drittel.' },
  { value: 'raid10', label: 'RAID10 – schneller, eine Platte pro Paar darf ausfallen', min: 4, copies: 2, hint: 'Zwei Kopien, über mehrere Platten verteilt (schneller). Nutzbar ist etwa die Hälfte.' },
]

export const profileLabel = (p?: string) => (p ? (p === 'single' ? 'Single' : p.toUpperCase().replace('C3', 'c3')) : '')
export const profileMin = (p: string) => BTRFS_PROFILES.find((x) => x.value === p)?.min ?? 1
/** Wie viele Platten ausfallen dürfen (zum Erkennen einer Verringerung). */
export const profileRedundancy = (p: string) => ({ single: 0, raid1: 1, raid10: 1, raid1c3: 2 })[p as BtrfsProfile] ?? 0

/**
 * Nutzbare Kapazität (Bytes) eines btrfs-Pools aus den Plattengrößen und was wegen unterschiedlicher Größen ungenutzt bleibt.
 * Bei k Kopien: min(Summe / k, (Summe − größte) / (k − 1)); RAID10 wird wie RAID1 genähert.
 */
export function btrfsUsable(profile: BtrfsProfile, sizes: number[]): { usable: number; unused: number } {
  const sum = sizes.reduce((a, b) => a + b, 0)
  const k = BTRFS_PROFILES.find((p) => p.value === profile)?.copies ?? 1
  if (sizes.length === 0) return { usable: 0, unused: 0 }
  if (k === 1) return { usable: sum, unused: 0 }
  const largest = Math.max(...sizes)
  const usable = Math.max(0, Math.floor(Math.min(sum / k, (sum - largest) / (k - 1))))
  return { usable, unused: Math.max(0, sum - usable * k) }
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
  /** Zustand eines btrfs-Pools; fehlt bei mergerfs oder wenn der Agent nicht antwortet. */
  btrfs?: BtrfsStatus
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
  setSpare: (id: string, spare: boolean) => api.put<void>(`/api/v1/disks/${enc(id)}/spare`, { spare }),
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
  deletePool: (name: string, confirm?: string) =>
    api.del<JobRef>(`/api/v1/pools/${enc(name)}${confirm ? `?confirm=${enc(confirm)}` : ''}`),
  previewPool: (branches: string[]) => api.post<PoolPreview>('/api/v1/pools/preview', { branches }),
  btrfsScrub: (pool: string, action: 'start' | 'cancel') => api.post<void>(`/api/v1/pools/${enc(pool)}/btrfs/scrub`, { action }),
  btrfsReplace: (pool: string, in_: BtrfsDiskInput) => api.post<JobRef>(`/api/v1/pools/${enc(pool)}/btrfs/replace`, in_),
  btrfsAddDisk: (pool: string, in_: BtrfsDiskInput) => api.post<JobRef>(`/api/v1/pools/${enc(pool)}/btrfs/devices`, in_),
  btrfsRemoveDisk: (pool: string, disk: string, confirm: string) =>
    api.del<JobRef>(`/api/v1/pools/${enc(pool)}/btrfs/devices/${enc(disk)}?confirm=${enc(confirm)}`),
  btrfsBalance: (pool: string, usage: number) => api.post<JobRef>(`/api/v1/pools/${enc(pool)}/btrfs/balance`, { usage }),
  btrfsConvert: (pool: string, profile: BtrfsProfile, confirmName: string) =>
    api.post<JobRef>(`/api/v1/pools/${enc(pool)}/btrfs/convert`, { profile, confirmName }),
  btrfsMountDegraded: (pool: string) => api.post<void>(`/api/v1/pools/${enc(pool)}/btrfs/mount-degraded`),
  btrfsResetErrors: (pool: string) => api.post<void>(`/api/v1/pools/${enc(pool)}/btrfs/reset-errors`),
  scrubSchedule: () => api.get<BtrfsScrubSchedule>('/api/v1/btrfs/scrub-schedule'),
  saveScrubSchedule: (s: BtrfsScrubSchedule) => api.put<BtrfsScrubSchedule>('/api/v1/btrfs/scrub-schedule', s),
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
