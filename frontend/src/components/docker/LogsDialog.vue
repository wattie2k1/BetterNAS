<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import { wsUrl } from '@/api/docker'
import { appendLog } from '@/utils/logs'

const props = defineProps<{ visible: boolean; container: { id: string; name: string } | null }>()
const emit = defineEmits<{ 'update:visible': [boolean] }>()

const MAX_LINES = 5000
const text = ref('')
const state = ref<'connecting' | 'live' | 'ended' | 'error'>('connecting')
const paused = ref(false)
const view = ref<HTMLElement>()
let pending = ''
let ws: WebSocket | undefined

function scrollDown() {
  void nextTick(() => {
    if (view.value) view.value.scrollTop = view.value.scrollHeight
  })
}

function push(chunk: string) {
  if (paused.value) {
    pending = appendLog(pending, chunk, MAX_LINES)
    return
  }
  const el = view.value
  const atBottom = !el || el.scrollHeight - el.scrollTop - el.clientHeight < 40
  text.value = appendLog(text.value, chunk, MAX_LINES)
  if (atBottom) scrollDown()
}

function close() {
  if (ws) {
    ws.onclose = ws.onmessage = ws.onerror = null
    ws.close()
    ws = undefined
  }
}

function open() {
  close()
  if (!props.container) return
  text.value = ''
  pending = ''
  paused.value = false
  state.value = 'connecting'
  const sock = new WebSocket(wsUrl(`/api/v1/containers/${encodeURIComponent(props.container.id)}/logs?tail=200`))
  ws = sock
  sock.onopen = () => (state.value = 'live')
  sock.onmessage = (ev) => {
    state.value = 'live'
    if (typeof ev.data === 'string') push(ev.data)
  }
  sock.onerror = () => (state.value = 'error')
  sock.onclose = () => {
    if (state.value !== 'error') state.value = 'ended'
  }
}

function togglePause() {
  paused.value = !paused.value
  if (!paused.value && pending) {
    text.value = appendLog(text.value, pending, MAX_LINES)
    pending = ''
    scrollDown()
  }
}

watch(
  () => [props.visible, props.container?.id] as const,
  ([v]) => (v ? open() : close()),
  { immediate: true },
)
onBeforeUnmount(close)

const labels = { connecting: 'Verbinde …', live: 'Live', ended: 'Beendet', error: 'Verbindungsfehler' }
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="`Logs: ${container?.name ?? ''}`"
    :style="{ width: 'min(960px, 96vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <div class="bar">
      <span class="state" :class="state" data-test="logs-state">{{ labels[state] }}</span>
      <span class="spacer" />
      <Button data-test="logs-pause" :label="paused ? 'Weiter' : 'Pause'" :icon="paused ? 'pi pi-play' : 'pi pi-pause'" size="small" text @click="togglePause" />
      <Button data-test="logs-clear" label="Leeren" icon="pi pi-eraser" size="small" text @click="text = ''" />
      <Button data-test="logs-reconnect" label="Neu verbinden" icon="pi pi-refresh" size="small" text @click="open" />
    </div>
    <pre ref="view" class="logs" data-test="logs-text" aria-live="off">{{ text || (state === 'live' ? 'Noch keine Ausgabe.' : '') }}</pre>
  </Dialog>
</template>

<style scoped>
.bar {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  margin-bottom: 0.5rem;
}
.spacer {
  flex: 1;
}
.state {
  font-size: 0.8rem;
  padding: 0.15rem 0.6rem;
  border-radius: 999px;
  background: var(--p-content-hover-background);
}
.state.live {
  color: var(--p-green-500);
}
.state.error {
  color: var(--p-red-500);
}
.logs {
  margin: 0;
  height: 460px;
  overflow: auto;
  padding: 0.75rem;
  border-radius: 8px;
  background: #0b1020;
  color: #d6deeb;
  font: 12.5px/1.45 ui-monospace, SFMono-Regular, Menlo, monospace;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
