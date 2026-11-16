import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import LogsDialog from '@/components/docker/LogsDialog.vue'
import ShellDialog from '@/components/docker/ShellDialog.vue'
import ContainersView from '@/views/docker/ContainersView.vue'
import { STATS_INTERVAL_MS, useDockerStore } from '@/stores/docker'
import { appendLog } from '@/utils/logs'

class FakeWS {
  static instances: FakeWS[] = []
  static OPEN = 1
  OPEN = 1
  readyState = 1
  binaryType = ''
  sent: unknown[] = []
  onopen: (() => void) | null = null
  onmessage: ((e: { data: unknown }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  constructor(public url: string) {
    FakeWS.instances.push(this)
  }
  send(d: unknown) {
    this.sent.push(d)
  }
  close() {
    this.closed = true
  }
}
const last = () => FakeWS.instances[FakeWS.instances.length - 1]

// xterm braucht einen echten Browser; ein einfacher Ersatz genügt, um Ein-/Ausgabe zu prüfen.
const fakeTerm = {
  rows: 30,
  cols: 100,
  written: [] as unknown[],
  dataCb: (_d: string) => {},
  resizeCb: (_s: { rows: number; cols: number }) => {},
  write(d: unknown) {
    this.written.push(d)
  },
  focus() {},
  onData(cb: (d: string) => void) {
    this.dataCb = cb
  },
  onResize(cb: (s: { rows: number; cols: number }) => void) {
    this.resizeCb = cb
  },
}
vi.mock('@/utils/terminal', () => ({
  createTerminal: () => ({ term: fakeTerm, fit: () => {}, dispose: () => {} }),
}))

const plugins = () => [createPinia(), PrimeVue, ToastService]

beforeEach(() => {
  FakeWS.instances = []
  fakeTerm.written = []
  vi.stubGlobal('WebSocket', FakeWS)
})
afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('appendLog', () => {
  it('behält nur die letzten Zeilen', () => {
    expect(appendLog('a\nb\n', 'c\n', 10)).toBe('a\nb\nc\n')
    expect(appendLog('1\n2\n3\n', '4\n5\n', 3)).toBe('3\n4\n5\n')
    expect(appendLog('', 'x', 3)).toBe('x')
  })
})

describe('LogsDialog', () => {
  const mountDialog = (visible = true) =>
    mount(LogsDialog, { props: { visible, container: { id: '0123456789ab', name: 'web' } }, global: { plugins: plugins() }, attachTo: document.body })

  it('verbindet sich, zeigt Text und kann pausieren', async () => {
    const w = mountDialog()
    await flushPromises()
    expect(last().url).toContain('/api/v1/containers/0123456789ab/logs?tail=200')
    last().onmessage!({ data: 'zeile 1\n' })
    await flushPromises()
    expect(document.querySelector('[data-test="logs-text"]')!.textContent).toContain('zeile 1')
    expect(document.querySelector('[data-test="logs-state"]')!.textContent).toBe('Live')

    ;(document.querySelector('[data-test="logs-pause"]') as HTMLElement).click()
    await flushPromises()
    last().onmessage!({ data: 'zeile 2\n' })
    await flushPromises()
    expect(document.querySelector('[data-test="logs-text"]')!.textContent).not.toContain('zeile 2')
    ;(document.querySelector('[data-test="logs-pause"]') as HTMLElement).click()
    await flushPromises()
    expect(document.querySelector('[data-test="logs-text"]')!.textContent).toContain('zeile 2')
    w.unmount()
  })

  it('zeigt das Ende und schließt die Verbindung beim Schließen', async () => {
    const w = mountDialog()
    await flushPromises()
    const sock = last()
    sock.onclose!()
    await flushPromises()
    expect(document.querySelector('[data-test="logs-state"]')!.textContent).toBe('Beendet')
    await w.setProps({ visible: false })
    expect(sock.closed).toBe(true)
    w.unmount()
  })
})

describe('ShellDialog', () => {
  it('verbindet mit Terminalgröße, sendet Eingabe und Größenänderung und zeigt Ausgabe', async () => {
    const w = mount(ShellDialog, { props: { visible: true, container: { id: '0123456789ab', name: 'web' } }, global: { plugins: plugins() }, attachTo: document.body })
    await vi.waitFor(() => expect(FakeWS.instances.length).toBe(1))
    const sock = last()
    expect(sock.url).toContain('/api/v1/containers/0123456789ab/shell?rows=30&cols=100')
    expect(sock.binaryType).toBe('arraybuffer')
    sock.onopen!()
    sock.onmessage!({ data: new TextEncoder().encode('$ ').buffer })
    expect(Array.from(fakeTerm.written[0] as Uint8Array)).toEqual([36, 32])

    fakeTerm.dataCb('ls\n')
    expect(Array.from(sock.sent[0] as Uint8Array)).toEqual([108, 115, 10])
    fakeTerm.resizeCb({ rows: 40, cols: 120 })
    expect(JSON.parse(sock.sent[1] as string)).toEqual({ type: 'resize', rows: 40, cols: 120 })

    await w.setProps({ visible: false })
    expect(sock.closed).toBe(true)
    w.unmount()
  })
})

describe('Container-Auslastung', () => {
  it('Store fragt Stats regelmäßig ab und stoppt wieder', async () => {
    setActivePinia(createPinia())
    vi.useFakeTimers()
    const fn = vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify([{ id: 'a', name: 'web', cpu: 1.5, memUsage: 1, memLimit: 2, memPercent: 50 }]), { status: 200 })))
    vi.stubGlobal('fetch', fn)
    const s = useDockerStore()
    s.startStats()
    await vi.advanceTimersByTimeAsync(0)
    expect(s.stats.a.cpu).toBe(1.5)
    await vi.advanceTimersByTimeAsync(STATS_INTERVAL_MS)
    expect(fn).toHaveBeenCalledTimes(2)
    s.stopStats()
    await vi.advanceTimersByTimeAsync(STATS_INTERVAL_MS * 3)
    expect(fn).toHaveBeenCalledTimes(2)
  })

  it('Tabelle zeigt CPU/RAM laufender Container und Logs-/Shell-Knöpfe', async () => {
    const c = { id: '0123456789ab', name: 'web', image: 'nginx', state: 'running', status: 'Up', stack: '', ports: [], created: '' }
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url: string) => {
        const body: Record<string, unknown> = {
          '/api/v1/docker/status': { installed: true, active: true, version: '26' },
          '/api/v1/stacks': [],
          '/api/v1/containers': [c, { ...c, id: 'ba9876543210', name: 'off', state: 'exited' }],
          '/api/v1/containers/stats': [{ id: '0123456789ab', name: 'web', cpu: 2.5, memUsage: 10485760, memLimit: 1073741824, memPercent: 1 }],
        }
        return Promise.resolve(new Response(JSON.stringify(body[url] ?? {}), { status: 200 }))
      }),
    )
    const w = mount(ContainersView, { global: { plugins: plugins() } })
    await vi.waitFor(() => expect(w.find('[data-test="container-web"] [data-test="usage"]').text()).toContain('CPU 2.5 %'))
    expect(w.find('[data-test="container-web"] button[aria-label="Logs"]').exists()).toBe(true)
    expect(w.find('[data-test="container-web"] button[aria-label="Shell"]').exists()).toBe(true)
    expect(w.find('[data-test="container-off"] button[aria-label="Logs"]').exists()).toBe(false)
    w.unmount()
  })
})
