/** Präfixlänge (0–32) in eine Netzmaske umrechnen, z. B. 24 → 255.255.255.0. */
export function prefixToMask(prefix: number): string {
  const bits = prefix <= 0 ? 0 : (0xffffffff << (32 - prefix)) >>> 0
  return [24, 16, 8, 0].map((s) => (bits >>> s) & 255).join('.')
}

/** Netzmaske (255.255.255.0) oder Präfixlänge (24, /24) in eine Präfixlänge umrechnen; ungültig → null. */
export function maskToPrefix(input: string): number | null {
  const s = input.trim().replace(/^\//, '')
  if (/^\d{1,2}$/.test(s)) {
    const n = Number(s)
    return n <= 32 ? n : null
  }
  const parts = s.split('.')
  if (parts.length !== 4 || parts.some((p) => !/^\d{1,3}$/.test(p) || Number(p) > 255)) return null
  const bits = parts.reduce((acc, p) => ((acc << 8) | Number(p)) >>> 0, 0)
  const ones = bits.toString(2).replace(/0+$/, '')
  if (/0/.test(ones)) return null // Einsen müssen zusammenhängen
  return ones.length
}

/** „192.168.1.10/24“ → IP und Netzmaske (Standard 255.255.255.0, wenn kein Präfix angegeben ist). */
export function splitCidr(cidr: string): { ip: string; mask: string } {
  const [ip = '', p] = cidr.split('/')
  const prefix = p === undefined || p === '' ? 24 : Number(p)
  return { ip, mask: prefixToMask(Number.isFinite(prefix) ? prefix : 24) }
}
