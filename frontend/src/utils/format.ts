const units = ['B', 'kB', 'MB', 'GB', 'TB', 'PB']

/** Formatiert Bytes mit dezimalen Einheiten (wie Plattenhersteller) und deutschem Komma. */
export function formatBytes(n: number): string {
  if (n < 1000) return `${n} B`
  let i = 0
  let v = n
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  return `${v.toLocaleString('de-DE', { minimumFractionDigits: 1, maximumFractionDigits: 1 })} ${units[i]}`
}

/** Formatiert eine Laufzeit in Sekunden als „2 Tage 1 Std.“ bzw. „3 Std. 5 Min.“. */
export function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d} ${d === 1 ? 'Tag' : 'Tage'} ${h} Std.`
  if (h > 0) return `${h} Std. ${m} Min.`
  return `${m} Min.`
}

/** Formatiert einen ISO-Zeitstempel als „16.11.2026, 03:00“. */
export function formatDateTime(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString('de-DE', { dateStyle: 'medium', timeStyle: 'short' })
}

/** Laufzeit im Stil der Docker-Statuszeile („Up 3 hours“). */
export function formatUpFor(seconds: number): string {
  const unit = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`
  if (seconds < 60) return `Up ${unit(Math.floor(seconds), 'second')}`
  const min = Math.floor(seconds / 60)
  if (min < 60) return `Up ${unit(min, 'minute')}`
  const h = Math.floor(min / 60)
  if (h < 48) return `Up ${unit(h, 'hour')}`
  return `Up ${unit(Math.floor(h / 24), 'day')}`
}
