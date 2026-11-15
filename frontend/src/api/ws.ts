// WebSocket-Verbindung für Live-Ereignisse mit automatischem Wiederverbinden.

export interface EventMessage {
  type: string
  data?: unknown
}

export function connectEvents(onMessage: (m: EventMessage) => void): () => void {
  let ws: WebSocket | undefined
  let delay = 1000
  let stopped = false
  let timer: ReturnType<typeof setTimeout> | undefined

  const open = () => {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    ws = new WebSocket(`${proto}//${location.host}/api/v1/ws`)
    ws.onopen = () => {
      delay = 1000
    }
    ws.onmessage = (ev) => {
      try {
        onMessage(JSON.parse(ev.data as string))
      } catch {
        /* ungültige Nachricht ignorieren */
      }
    }
    ws.onclose = () => {
      if (stopped) return
      timer = setTimeout(open, delay)
      delay = Math.min(delay * 2, 30000)
    }
  }
  open()
  return () => {
    stopped = true
    if (timer) clearTimeout(timer)
    ws?.close()
  }
}
