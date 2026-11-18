// WebSocket-Verbindung für Live-Ereignisse mit automatischem Wiederverbinden.

export interface EventMessage {
  type: string
  data?: unknown
}

/** onReconnect wird nach jedem erneuten Verbinden aufgerufen: in der Lücke verpasste Ereignisse müssen nachgeladen werden. */
export function connectEvents(onMessage: (m: EventMessage) => void, onReconnect?: () => void): () => void {
  let ws: WebSocket | undefined
  let delay = 1000
  let wasConnected = false
  let stopped = false
  let timer: ReturnType<typeof setTimeout> | undefined

  const open = () => {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    ws = new WebSocket(`${proto}//${location.host}/api/v1/ws`)
    ws.onopen = () => {
      delay = 1000
      if (wasConnected) onReconnect?.()
      wasConnected = true
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
