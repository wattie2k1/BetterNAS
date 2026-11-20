import { api, apiFetch } from './client'

export interface BackupManifest {
  format: number
  createdAt: string
  version: string
  schemaVersion: number
  host: string
  counts: Record<string, number>
}

export type BackupKind = 'config' | 'appdata'

export interface BackupInfo {
  name: string
  kind: BackupKind
  size: number
  manifest: BackupManifest
}

export interface RestorePreview {
  token: string
  manifest: BackupManifest
  expires: string
  warnings: string[]
}

export interface BackupConfig {
  enabled: boolean
  time: string
  keep: number
  /** Ordner der Sicherungen; leer = Systemplatte (Standard). */
  dir: string
  /** appdata (Einstellungen der Docker-Apps) mitsichern. */
  appdata: boolean
}

export interface BackupStatus {
  dir: string
  default: boolean
  /** Ordner der App-Einstellungen; leer, solange Docker nicht eingerichtet ist. */
  appdata: string
  free: number
  problem: string
}

const enc = encodeURIComponent

export const backupApi = {
  getConfig: () => api.get<BackupConfig>('/api/v1/backups/config'),
  setConfig: (c: BackupConfig) => api.put<BackupConfig>('/api/v1/backups/config', c),
  status: () => api.get<BackupStatus>('/api/v1/backups/status'),
  createAppdata: () => api.post<{ jobId: string }>('/api/v1/backups/appdata'),
  list: () => api.get<BackupInfo[]>('/api/v1/backups'),
  create: () => api.post<BackupInfo>('/api/v1/backups'),
  remove: (name: string) => api.del<void>(`/api/v1/backups/${enc(name)}`),
  downloadUrl: (name: string) => `/api/v1/backups/${enc(name)}`,
  previewUpload: (file: Blob) =>
    apiFetch<RestorePreview>('/api/v1/backups/restore/preview', { method: 'POST', body: file, headers: { 'Content-Type': 'application/gzip' } }),
  previewStored: (name: string) => api.post<RestorePreview>(`/api/v1/backups/${enc(name)}/restore/preview`),
  restore: (token: string) => api.post<{ status: string }>('/api/v1/backups/restore', { token }),
}
