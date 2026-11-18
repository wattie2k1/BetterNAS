import type { ContainerInfo, ProxySite } from '@/api/docker'

/** Gleiche Regel wie im Backend: Kleinbuchstaben, mindestens zwei Teile, Endung nur Buchstaben. */
export const DOMAIN_RE = /^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/

export const CADDY_CONTAINER = 'reverse-proxy-caddy-1'

/** 10.x.x.x, 172.16–31.x.x, 192.168.x.x */
export function isPrivateIPv4(s: string): boolean {
  const p = s.split('.')
  if (p.length !== 4 || !p.every((x) => /^\d{1,3}$/.test(x) && Number(x) <= 255 && String(Number(x)) === x)) return false
  const [a, b] = p.map(Number)
  return a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168)
}

export interface PublishedPort {
  container: string
  port: number
  label: string
}

/** Auf dem NAS veröffentlichte TCP-Ports laufender Container (ohne den Reverse-Proxy selbst), nach Name sortiert. */
export function publishedPorts(containers: ContainerInfo[]): PublishedPort[] {
  const out: PublishedPort[] = []
  for (const c of containers) {
    if (c.state !== 'running' || c.name === CADDY_CONTAINER) continue
    const seen = new Set<number>()
    for (const p of c.ports) {
      const m = /^(?:[^:]*:)?(\d+)->(\d+)\/tcp$/.exec(p) // Bereiche („8000-8002“) passen bewusst nicht
      if (!m) continue
      const host = Number(m[1])
      if (seen.has(host)) continue
      seen.add(host)
      out.push({ container: c.name, port: host, label: `${c.name} – Port ${host}` })
    }
  }
  return out.sort((a, b) => a.container.localeCompare(b.container) || a.port - b.port)
}

/** Prüft einen Eintrag; liefert eine Fehlermeldung oder ''. */
export function siteError(s: ProxySite, others: ProxySite[] = []): string {
  const domain = s.domain.trim().toLowerCase()
  if (!domain) return 'Domain angeben.'
  if (domain.length > 253 || !DOMAIN_RE.test(domain)) return 'Keine gültige Domain, z. B. jellyfin.meinedomain.de (ohne http:// und ohne Pfad, Umlaute als xn--…).'
  if (others.some((o) => o.domain.trim().toLowerCase() === domain)) return 'Diese Domain ist schon eingetragen.'
  const port = Number(s.port)
  if (!Number.isInteger(port) || port < 1 || port > 65535) return 'Ziel-Port zwischen 1 und 65535 angeben.'
  if (s.host && !isPrivateIPv4(s.host.trim())) return 'Als Ziel sind nur Adressen aus dem Heimnetz erlaubt (192.168.x.x, 10.x.x.x, 172.16–31.x.x).'
  return ''
}
