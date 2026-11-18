/**
 * Ruft fn regelmäßig auf, aber nur solange der Tab sichtbar ist. Wird er wieder sichtbar,
 * läuft fn sofort einmal, damit die Anzeige nicht veraltet ist. Liefert die Stop-Funktion.
 */
export function pollWhileVisible(fn: () => void, ms: number): () => void {
  const tick = () => {
    if (!document.hidden) fn()
  }
  const timer = setInterval(tick, ms)
  const onVisible = () => {
    if (!document.hidden) fn()
  }
  document.addEventListener('visibilitychange', onVisible)
  return () => {
    clearInterval(timer)
    document.removeEventListener('visibilitychange', onVisible)
  }
}
