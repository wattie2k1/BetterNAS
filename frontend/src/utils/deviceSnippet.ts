import type { HostDevice } from '@/api/docker'

/**
 * Erzeugt die compose-Zeilen, die Geräte an einen Dienst durchreichen: `devices:` plus `group_add:` mit den
 * numerischen Gruppen der Gerätedateien (der Container-Benutzer braucht sie, um das Gerät öffnen zu dürfen).
 * `indent` ist die Einrückung der Schlüssel (Eigenschaften eines Dienstes). Das Ergebnis endet mit einem Zeilenumbruch.
 */
export function deviceSnippet(devs: HostDevice[], indent = '    '): string {
  if (!devs.length) return ''
  const gids = [...new Set(devs.map((d) => d.gid))]
  const lines = [`${indent}devices:`, ...devs.map((d) => `${indent}  - ${d.path}:${d.path}`), `${indent}group_add:`, ...gids.map((g) => `${indent}  - "${g}"`)]
  return lines.join('\n') + '\n'
}
