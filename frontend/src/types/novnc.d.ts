declare module '@novnc/novnc' {
  export default class RFB extends EventTarget {
    constructor(target: HTMLElement, urlOrChannel: string | WebSocket, options?: { wsProtocols?: string[]; credentials?: Record<string, string> })
    scaleViewport: boolean
    resizeSession: boolean
    clipViewport: boolean
    viewOnly: boolean
    focusOnClick: boolean
    background: string
    disconnect(): void
    focus(): void
    sendCtrlAltDel(): void
    sendCredentials(credentials: Record<string, string>): void
  }
}
