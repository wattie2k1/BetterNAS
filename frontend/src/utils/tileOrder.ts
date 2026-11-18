import { computed, ref } from 'vue'

/** Gespeicherte Reihenfolge mit den bekannten Kacheln abgleichen: Unbekannte fallen weg, neue kommen ans Ende. */
export function mergeOrder(stored: unknown, defaults: readonly string[]): string[] {
  const known = new Set(defaults)
  const out: string[] = []
  if (Array.isArray(stored)) {
    for (const id of stored) if (typeof id === 'string' && known.has(id) && !out.includes(id)) out.push(id)
  }
  for (const id of defaults) if (!out.includes(id)) out.push(id)
  return out
}

/** Verschiebt `id` auf die Position von `target` (die übrigen rücken nach). */
export function moveTile(order: readonly string[], id: string, target: string): string[] {
  const from = order.indexOf(id)
  const to = order.indexOf(target)
  if (from < 0 || to < 0 || from === to) return [...order]
  const next = [...order]
  next.splice(from, 1)
  next.splice(to, 0, id)
  return next
}

/** Verschiebt `id` um `delta` Plätze innerhalb der sichtbaren Kacheln (Tastaturbedienung). */
export function shiftTile(order: readonly string[], visible: readonly string[], id: string, delta: number): string[] {
  const i = visible.indexOf(id)
  const target = visible[i + delta]
  return i < 0 || target === undefined ? [...order] : moveTile(order, id, target)
}

/** Kachelreihenfolge, die im Browser (localStorage) pro Schlüssel gemerkt wird. */
export function useTileOrder(key: () => string, defaults: readonly string[] | (() => readonly string[])) {
  const read = (): unknown => {
    try {
      return JSON.parse(localStorage.getItem(key()) ?? 'null')
    } catch {
      return null
    }
  }
  const stored = ref<unknown>(read())
  const order = computed(() => mergeOrder(stored.value, typeof defaults === 'function' ? defaults() : defaults))

  function set(next: string[]) {
    stored.value = next
    try {
      localStorage.setItem(key(), JSON.stringify(next))
    } catch {
      // Speicher gesperrt oder voll: die Reihenfolge gilt dann nur für diese Sitzung.
    }
  }
  function reset() {
    stored.value = null
    try {
      localStorage.removeItem(key())
    } catch {
      // siehe oben
    }
  }
  /** Liest erneut (z. B. nachdem der Benutzername bekannt ist). */
  const reload = () => (stored.value = read())
  return { order, set, reset, reload }
}
