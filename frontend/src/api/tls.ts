import { api } from './client'

export type TlsMode = 'selfsigned' | 'localca' | 'acme' | 'custom'

export interface DnsField {
  key: string
  label: string
  secret: boolean
  optional?: boolean
}

export interface DnsProvider {
  id: string
  label: string
  help: string
  fields: DnsField[]
}

export interface TlsStatus {
  mode: TlsMode
  localCA: { exists: boolean; fingerprint?: string; notAfter?: string; names: string[] }
  acme: {
    configured: boolean
    provider: string
    domains: string[]
    email: string
    staging: boolean
    agreedTerms: boolean
    credentialsSet: Record<string, boolean>
    lastIssued?: string
    lastError?: string
    busy: boolean
  }
  providers: DnsProvider[]
}

export interface AcmeInput {
  provider: string
  domains: string[]
  email: string
  staging: boolean
  agreedTerms: boolean
  credentials: Record<string, string>
}

/** Download des Root-Zertifikats der lokalen CA (Sitzungs-Cookie genügt). */
export const CA_DOWNLOAD_URL = '/api/v1/system/tls/ca.crt'

export const tlsApi = {
  status: () => api.get<TlsStatus>('/api/v1/system/tls'),
  enableLocalCa: (names: string[], resetCA = false) => api.post<TlsStatus>('/api/v1/system/tls/localca', { names, resetCA }),
  saveAcme: (i: AcmeInput) => api.put<TlsStatus>('/api/v1/system/tls/acme', i),
  removeAcme: () => api.del<TlsStatus>('/api/v1/system/tls/acme'),
  issueAcme: () => api.post<{ jobId: string }>('/api/v1/system/tls/acme/issue'),
}

/** Zerlegt eine Eingabe wie „a.de, b.de“ in eine Liste. */
export function splitList(text: string): string[] {
  return text
    .split(/[\s,;]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}
