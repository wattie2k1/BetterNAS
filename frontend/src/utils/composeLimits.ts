// Ressourcen-Limits (Arbeitsspeicher, CPU) einzelner Dienste in einer compose.yaml lesen und ändern.
// Die Datei wird als YAML-Dokument bearbeitet, damit Kommentare, Reihenfolge und Leerzeilen erhalten bleiben.
import { isMap, isScalar, parseDocument, type Document, type YAMLMap } from 'yaml'

export interface ServiceLimits {
  service: string
  /** Wert, wie er in der Datei steht (z. B. „512m“ oder „${MEM}“); null = kein Limit. */
  memory: string | null
  cpus: string | null
  /** Wo die Werte stehen: direkt am Dienst (mem_limit/cpus) oder unter deploy.resources.limits. */
  where: 'service' | 'deploy'
}

export type LimitsRead = { ok: true; services: ServiceLimits[] } | { ok: false; error: string }

export interface LimitsPatch {
  /** undefined = unverändert, null = Limit entfernen */
  memory?: string | null
  cpus?: string | null
}

const text = (v: unknown): string | null => (isScalar(v) && v.value !== null && v.value !== undefined ? String(v.value) : null)

function parse(compose: string): Document.Parsed | null {
  const doc = parseDocument(compose)
  return doc.errors.length ? null : doc
}

function servicesOf(doc: Document): YAMLMap | null {
  const s = doc.get('services', true)
  return isMap(s) ? s : null
}

function limitsMap(svc: YAMLMap): YAMLMap | null {
  const l = svc.getIn(['deploy', 'resources', 'limits'], true)
  return isMap(l) ? l : null
}

function whereOf(svc: YAMLMap): 'service' | 'deploy' {
  const l = limitsMap(svc)
  return l && (l.has('memory') || l.has('cpus')) ? 'deploy' : 'service'
}

/** Liest die Limits aller Dienste. Bei ungültigem YAML kommt eine Fehlermeldung. */
export function readLimits(compose: string): LimitsRead {
  const doc = parseDocument(compose)
  if (doc.errors.length) return { ok: false, error: doc.errors[0].message.split('\n')[0] }
  const services = servicesOf(doc)
  if (!services) return { ok: true, services: [] }
  const out: ServiceLimits[] = []
  for (const pair of services.items) {
    const name = text(pair.key)
    if (name === null || !isMap(pair.value)) continue
    const svc = pair.value
    const where = whereOf(svc)
    const l = limitsMap(svc)
    out.push({
      service: name,
      memory: text(where === 'deploy' ? l!.get('memory', true) : svc.get('mem_limit', true)),
      cpus: text(where === 'deploy' ? l!.get('cpus', true) : svc.get('cpus', true)),
      where,
    })
  }
  return { ok: true, services: out }
}

function prune(svc: YAMLMap) {
  const deploy = svc.get('deploy', true)
  if (!isMap(deploy)) return
  const res = deploy.get('resources', true)
  if (isMap(res)) {
    const lim = res.get('limits', true)
    if (isMap(lim) && lim.items.length === 0) res.delete('limits')
    if (res.items.length === 0) deploy.delete('resources')
  }
  if (deploy.items.length === 0) svc.delete('deploy')
}

/**
 * Setzt oder entfernt Limits eines Dienstes. Vorhandene Werte werden dort geändert, wo sie stehen
 * (deploy bleibt deploy), neue kommen als mem_limit/cpus an den Dienst. Ist nichts zu tun oder die Datei
 * ungültig, kommt der Text unverändert zurück.
 */
export function setLimits(compose: string, service: string, patch: LimitsPatch): string {
  if (patch.memory === undefined && patch.cpus === undefined) return compose
  const doc = parse(compose)
  const services = doc && servicesOf(doc)
  const svc = services?.get(service, true)
  if (!doc || !isMap(svc)) return compose
  const where = whereOf(svc)
  const base = where === 'deploy' ? ['deploy', 'resources', 'limits'] : []
  const apply = (key: string, serviceKey: string, value: string | number | null | undefined) => {
    if (value === undefined) return
    const path = where === 'deploy' ? [...base, key] : [serviceKey]
    if (value === null) svc.deleteIn(path)
    else svc.setIn(path, value)
  }
  apply('memory', 'mem_limit', patch.memory)
  apply('cpus', 'cpus', patch.cpus == null ? patch.cpus : Number(patch.cpus))
  prune(svc)
  return doc.toString({ lineWidth: 0, flowCollectionPadding: false })
}

const UNITS: Record<string, number> = { '': 1, b: 1, k: 1024, m: 1024 ** 2, g: 1024 ** 3, t: 1024 ** 4 }

/** Liest eine compose-Größe („512m“, „2G“, „1.5gb“, „1024“) in Bytes; Variablen und anderes ergeben null. */
export function parseMemory(raw: string): number | null {
  const m = /^(\d+(?:\.\d+)?)\s*([kmgtb]?)b?$/i.exec(raw.trim())
  return m ? Math.round(Number(m[1]) * UNITS[m[2].toLowerCase()]) : null
}

/** Zerlegt Bytes in Wert und Einheit für das Formular (ganze GB als „g“, sonst MB). */
export function formatMemory(bytes: number): { value: number; unit: 'm' | 'g' } {
  return bytes >= 1024 ** 3 && bytes % 1024 ** 3 === 0 ? { value: bytes / 1024 ** 3, unit: 'g' } : { value: Math.round(bytes / 1024 ** 2), unit: 'm' }
}

/** Baut den compose-Wert aus Formularwerten; Bruchteile von GB werden als MB geschrieben. */
export function memoryString(value: number, unit: 'm' | 'g'): string {
  if (unit === 'g') return Number.isInteger(value) ? `${value}g` : `${Math.round(value * 1024)}m`
  return `${Math.round(value)}m`
}
