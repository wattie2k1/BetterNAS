import { api } from './client'

export type Access = 'none' | 'ro' | 'rw'

export interface ShareAcl {
  type: 'user' | 'group'
  name: string
  access: Access
}

export interface NfsInput {
  enabled: boolean
  clients: string[]
  readOnly: boolean
  /** root = nur root wird abgebildet, all = alle Zugriffe laufen als anonymer Benutzer. */
  squash: 'root' | 'all'
}

export interface ShareInput {
  name: string
  pool: string
  subdir: string
  comment: string
  browsable: boolean
  guest: boolean
  /** Gäste dürfen auch schreiben (nur mit guest). */
  guestWrite: boolean
  recycle: boolean
  fruit: boolean
  acl: ShareAcl[]
  nfs: NfsInput
}

export interface ShareView extends ShareInput {
  path: string
  poolMounted: boolean
  uncPath: string
  /** host:/pfad, wenn NFS aktiv ist. */
  nfsMount?: string
}

/** Identisch zu den Backend-Regeln (shares.nameRe, shares.subdirRe). */
export const SHARE_NAME_RE = /^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$/
export const SUBDIR_SEGMENT_RE = /^[A-Za-z0-9][A-Za-z0-9 _.-]{0,63}$/
const RESERVED = ['global', 'homes', 'printers', 'print$', 'ipc$']

const HOST_LABEL_RE = /^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$/
const IPV4_RE = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/
const IPV6_RE = /^[0-9A-Fa-f:]+$/

function validIp(s: string): boolean {
  if (IPV4_RE.test(s)) return true
  return s.includes(':') && IPV6_RE.test(s) && s.split(':').length >= 3 && s.split(':').length <= 8 && (s.match(/::/g) ?? []).length <= 1
}

/** Wie shares.ValidClient im Backend: IP-Adresse, CIDR oder Rechnername; nie * und keine Exportoptionen. */
export function validNfsClient(c: string): boolean {
  if (!/^[A-Za-z0-9.:/_-]{1,253}$/.test(c)) return false
  if (c.includes('/')) {
    const [ip, bits, ...rest] = c.split('/')
    if (rest.length || !/^\d{1,3}$/.test(bits)) return false
    const n = Number(bits)
    return IPV4_RE.test(ip) ? n <= 32 : validIp(ip) && n <= 128
  }
  if (validIp(c)) return true
  if (c.includes(':') || c.includes('_')) return false
  const labels = c.split('.')
  return labels.every((l) => HOST_LABEL_RE.test(l)) && !labels.every((l) => /^\d+$/.test(l))
}

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
