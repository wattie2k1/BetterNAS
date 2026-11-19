import type { DiskIOStats } from '@/api/storage'

export interface IORate {
  readBps: number
  writeBps: number
  /** Anteil der Zeit, in der das Gerät beschäftigt war (0–100). */
  busyPct: number
}

/** Berechnet je Gerät die Raten aus zwei Messungen; Geräte ohne Vorwert oder mit zurückgesetzten Zählern entfallen. */
export function ioRates(prev: DiskIOStats, cur: DiskIOStats): Record<string, IORate> {
  const sec = (Date.parse(cur.at) - Date.parse(prev.at)) / 1000
  const out: Record<string, IORate> = {}
  if (!(sec > 0)) return out
  for (const [name, c] of Object.entries(cur.devices)) {
    const p = prev.devices[name]
    if (!p || c.readBytes < p.readBytes || c.writeBytes < p.writeBytes || c.busyMs < p.busyMs) continue
    out[name] = {
      readBps: (c.readBytes - p.readBytes) / sec,
      writeBps: (c.writeBytes - p.writeBytes) / sec,
      busyPct: Math.min(100, ((c.busyMs - p.busyMs) / (sec * 1000)) * 100),
    }
  }
  return out
}

/** „12,3 MB/s“ bzw. „0 B/s“. */
export function formatRate(bps: number, fmt: (n: number) => string): string {
  return `${fmt(Math.round(bps))}/s`
}
