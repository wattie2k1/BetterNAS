import { api } from './client'

export type Access = 'none' | 'ro' | 'rw'

export interface ShareAcl {
  type: 'user' | 'group'
  name: string
  access: Access
}

export interface ShareInput {
  name: string
  pool: string
  subdir: string
  comment: string
  browsable: boolean
  guest: boolean
  recycle: boolean
  fruit: boolean
  acl: ShareAcl[]
}

export interface ShareView extends ShareInput {
  path: string
  poolMounted: boolean
  uncPath: string
}

/** Identisch zu den Backend-Regeln (shares.nameRe, shares.subdirRe). */
export const SHARE_NAME_RE = /^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$/
export const SUBDIR_SEGMENT_RE = /^[A-Za-z0-9][A-Za-z0-9 _.-]{0,63}$/
const RESERVED = ['global', 'homes', 'printers', 'print$', 'ipc$']

export const isReservedShareName = (n: string) => RESERVED.includes(n.toLowerCase())

/** Liefert eine Fehlermeldung oder '' für einen gültigen Ordnerpfad innerhalb des Pools. */
export function subdirError(s: string): string {
  const parts = s.split('/')
  if (parts.length > 4) return 'Höchstens vier Ordnerebenen.'
  if (!parts.every((p) => SUBDIR_SEGMENT_RE.test(p))) return 'Ordnernamen: Buchstaben, Ziffern, Leerzeichen, _ . und -, nicht mit Punkt beginnend.'
  return ''
}

type JobRef = { jobId: string }
const enc = encodeURIComponent

export const sharesApi = {
  list: () => api.get<ShareView[]>('/api/v1/shares'),
  create: (s: ShareInput) => api.post<JobRef>('/api/v1/shares', s),
  update: (name: string, s: ShareInput) => api.put<JobRef>(`/api/v1/shares/${enc(name)}`, s),
  remove: (name: string) => api.del<JobRef>(`/api/v1/shares/${enc(name)}`),
}
