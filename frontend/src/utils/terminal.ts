// xterm.js wird nur für die Container-Shell gebraucht und deshalb erst beim Öffnen geladen (eigener Chunk).
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

export interface TerminalHandle {
  term: Terminal
  fit: () => void
  dispose: () => void
}

export function createTerminal(el: HTMLElement): TerminalHandle {
  const term = new Terminal({
    cursorBlink: true,
    fontSize: 13,
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
    theme: { background: '#0b1020' },
    scrollback: 5000,
  })
  const fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(el)
  const fit = () => {
    try {
      fitAddon.fit()
    } catch {
      /* Element noch nicht sichtbar */
    }
  }
  fit()
  return { term, fit, dispose: () => term.dispose() }
}
