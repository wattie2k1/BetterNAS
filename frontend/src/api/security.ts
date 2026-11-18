import { api } from './client'

export interface AuthPolicy {
  sessionHours: number
  maxFailures: number
  lockoutMinutes: number
  requireTotp: boolean
}

export interface SessionInfo {
  id: string
  username: string
  createdAt: string
  expiresAt: string
  ip: string
  current: boolean
}

export interface AuditEntry {
  id: number
  time: string
  actor: string
  action: string
  target: string
  result: string
  details?: Record<string, unknown>
}

export type LogSource = 'api' | 'agent' | 'samba' | 'docker' | 'libvirt'

export const securityApi = {
  policy: () => api.get<AuthPolicy>('/api/v1/security/policy'),
  setPolicy: (p: AuthPolicy) => api.put<void>('/api/v1/security/policy', p),
  sessions: () => api.get<SessionInfo[]>('/api/v1/security/sessions'),
  revoke: (id: string) => api.del<void>(`/api/v1/security/sessions/${id}`),
  revokeOthers: () => api.post<{ revoked: number }>('/api/v1/security/sessions/revoke-others'),
}

export const maintenanceApi = {
  audit: (limit = 200) => api.get<AuditEntry[]>(`/api/v1/logs/audit?limit=${limit}`),
  factoryReset: (confirm: string) => api.post<void>('/api/v1/system/factory-reset', { confirm }),
}

/** Protokolle sind Klartext, nicht JSON. */
export async function fetchLog(source: LogSource, lines: number): Promise<string> {
  const res = await fetch(`/api/v1/logs/${source}?lines=${lines}`, { credentials: 'same-origin' })
  if (!res.ok) {
    let msg = `Fehler ${res.status}`
    try {
      msg = (await res.json()).error?.message ?? msg
    } catch {
      /* Klartext-Fehler */
    }
    throw new Error(msg)
  }
  return res.text()
}

export const logDownloadUrl = (source: LogSource, lines: number) => `/api/v1/logs/${source}?lines=${lines}&download=1`
export const SUPPORT_BUNDLE_URL = '/api/v1/support-bundle'
