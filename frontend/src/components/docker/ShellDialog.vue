<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import { wsUrl } from '@/api/docker'
import type { TerminalHandle } from '@/utils/terminal'

const props = defineProps<{ visible: boolean; container: { id: string; name: string } | null }>()
const emit = defineEmits<{ 'update:visible': [boolean] }>()

const host = ref<HTMLElement>()
const state = ref<'connecting' | 'live' | 'ended'>('connecting')
let handle: TerminalHandle | undefined
let ws: WebSocket | undefined
let onWindowResize: (() => void) | undefined

function stop() {
  if (onWindowResize) window.removeEventListener('resize', onWindowResize)
  onWindowResize = undefined
  if (ws) {
    ws.onclose = ws.onmessage = ws.onerror = null
    ws.close()
    ws = undefined
  }
  handle?.dispose()
  handle = undefined
}

async function start() {
  stop()
  if (!props.container || !host.value) return
  state.value = 'connecting'
  const { createTerminal } = await import('@/utils/terminal')
  if (!host.value || !props.visible) return
  host.value.replaceChildren()
  const h = createTerminal(host.value)
  handle = h
  const { term } = h
  const sock = new WebSocket(wsUrl(`/api/v1/containers/${encodeURIComponent(props.container.id)}/shell?rows=${term.rows}&cols=${term.cols}`))
  sock.binaryType = 'arraybuffer'
  ws = sock
  const enc = new TextEncoder()
  sock.onopen = () => {
    state.value = 'live'
    term.focus()
  }
  sock.onmessage = (ev) => term.write(typeof ev.data === 'string' ? ev.data : new Uint8Array(ev.data as ArrayBuffer))
  sock.onclose = () => {
    state.value = 'ended'
    term.write('\r\n\x1b[90m[Verbindung beendet]\x1b[0m\r\n')
  }
  term.onData((d) => {
    if (sock.readyState === WebSocket.OPEN) sock.send(enc.encode(d))
  })
  term.onResize(({ rows, cols }) => {
    if (sock.readyState === WebSocket.OPEN) sock.send(JSON.stringify({ type: 'resize', rows, cols }))
  })
  onWindowResize = () => h.fit()
  window.addEventListener('resize', onWindowResize)
}

/** Der Dialoginhalt wird erst nach dem Öffnen gerendert; auf das Hostelement warten. */
async function whenHost(): Promise<boolean> {
  for (let i = 0; i < 50; i++) {
    await nextTick()
    if (host.value) return true
    await new Promise((r) => setTimeout(r, 10))
  }
  return false
}

watch(
  () => [props.visible, props.container?.id] as const,
  async ([v]) => {
    if (!v) return stop()
    if (await whenHost()) await start()
  },
  { immediate: true },
)
onBeforeUnmount(stop)
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="`Shell: ${container?.name ?? ''}`"
    :style="{ width: 'min(980px, 96vw)' }"
    @update:visible="emit('update:visible', $event)"
    @hide="stop"
  >
    <div ref="host" class="term" data-test="shell-host" />
    <small class="hint" data-test="shell-state">{{ state === 'live' ? 'Verbunden' : state === 'ended' ? 'Beendet' : 'Verbinde …' }} · Beim Schließen des Fensters wird die Shell beendet.</small>
  </Dialog>
</template>

<style scoped>
.term {
  height: 460px;
  padding: 6px;
  border-radius: 8px;
  background: #0b1020;
}
.hint {
  display: block;
  margin-top: 0.4rem;
  color: var(--p-text-muted-color);
}
</style>
