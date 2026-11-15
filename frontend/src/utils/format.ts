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
