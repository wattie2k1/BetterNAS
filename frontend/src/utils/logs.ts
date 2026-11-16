/** Hängt neuen Text an ein Protokoll an und behält höchstens die letzten maxLines Zeilen. */
export function appendLog(current: string, chunk: string, maxLines = 5000): string {
  const text = current + chunk
  let count = 0
  for (let i = text.length - 1; i >= 0; i--) {
    if (text.charCodeAt(i) === 10 && ++count > maxLines) return text.slice(i + 1)
  }
  return text
}
