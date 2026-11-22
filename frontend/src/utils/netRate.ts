export interface NetIface {
  name: string
  rxBytes: number
  txBytes: number
  up: boolean
  speedMbps: number
}
export interface NetRates {
  rx: number
  tx: number
  ifaces: { name: string; rx: number; tx: number; up: boolean; speedMbps: number }[]
}

/**
 * Datenrate in Byte/s aus zwei Zählerständen. Zähler, die zurückgesprungen sind (Neustart der Karte),
 * und nicht verfügbare Vorwerte ergeben 0 statt eines Ausreißers.
 */
export function netRates(prev: { at: number; net: NetIface[] } | null, cur: { at: number; net: NetIface[] }): NetRates | null {
  if (!prev || cur.at <= prev.at) return null
  const secs = (cur.at - prev.at) / 1000
  const ifaces = cur.net.map((n) => {
    const p = prev.net.find((x) => x.name === n.name)
    const rate = (a: number, b?: number) => (b === undefined || a < b ? 0 : (a - b) / secs)
    return { name: n.name, rx: rate(n.rxBytes, p?.rxBytes), tx: rate(n.txBytes, p?.txBytes), up: n.up, speedMbps: n.speedMbps }
  })
  return { rx: ifaces.reduce((a, i) => a + i.rx, 0), tx: ifaces.reduce((a, i) => a + i.tx, 0), ifaces }
}

/** Auslastung einer Schnittstelle in Prozent der Leitungsgeschwindigkeit (größere Richtung), null ohne bekannte Geschwindigkeit. */
export function ifaceLoad(i: { rx: number; tx: number; speedMbps: number }): number | null {
  if (i.speedMbps <= 0) return null
  return Math.min(100, (Math.max(i.rx, i.tx) * 8) / (i.speedMbps * 1e6) * 100)
}

export const formatRate = (bytesPerSec: number): string => {
  const units = ['B/s', 'kB/s', 'MB/s', 'GB/s']
  let v = bytesPerSec
  let i = 0
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  return `${v.toLocaleString('de-DE', { minimumFractionDigits: i ? 1 : 0, maximumFractionDigits: i ? 1 : 0 })} ${units[i]}`
}

/** Eine gemeinsame Einheit für eine ganze Skala (nach dem größten Wert gewählt) und eine kompakte Zahlenschreibweise. */
export const rateScale = (peak: number): { unit: string; format: (bytesPerSec: number) => string } => {
  const units = ['B/s', 'kB/s', 'MB/s', 'GB/s']
  let div = 1
  let i = 0
  while (peak / div >= 1000 && i < units.length - 1) {
    div *= 1000
    i++
  }
  const format = (v: number) =>
    (v / div).toLocaleString('de-DE', { minimumFractionDigits: 0, maximumFractionDigits: v / div < 10 ? 1 : 0 })
  return { unit: units[i], format }
}
