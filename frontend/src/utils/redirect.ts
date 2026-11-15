/** Erlaubt nur Ziele innerhalb der App: genau ein führendes "/" (kein "//host", kein "/\host", keine Schemata). */
export function safeNext(next: unknown): string {
  if (typeof next !== 'string' || !next.startsWith('/') || next.startsWith('//') || next.includes('\\')) return '/'
  return next
}
