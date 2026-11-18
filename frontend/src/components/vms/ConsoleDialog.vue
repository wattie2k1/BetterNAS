<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import { vmsApi } from '@/api/vms'

const props = defineProps<{ visible: boolean; name: string | null }>()
const emit = defineEmits<{ 'update:visible': [boolean] }>()

type Rfb = InstanceType<(typeof import('@novnc/novnc'))['default']>

const host = ref<HTMLElement>()
const state = ref<'connecting' | 'live' | 'ended'>('connecting')
const reason = ref('')
let rfb: Rfb | undefined

function stop() {
  if (rfb) {
    try {
      rfb.disconnect()
    } catch {
      /* schon getrennt */
    }
    rfb = undefined
  }
}

async function start() {
  stop()
  if (!props.name || !host.value) return
  state.value = 'connecting'
  reason.value = ''
  // noVNC wird erst beim Öffnen der Konsole geladen (eigener Chunk).
  const { default: RFB } = await import('@novnc/novnc')
  if (!host.value || !props.visible) return
  host.value.replaceChildren()
  const r = new RFB(host.value, vmsApi.consoleUrl(props.name), { wsProtocols: ['binary'] })
  r.scaleViewport = true
  r.resizeSession = false
  r.background = '#0b1020'
  r.addEventListener('connect', () => {
    state.value = 'live'
    r.focus()
  })
  r.addEventListener('disconnect', (e) => {
    if (rfb !== r) return
    state.value = 'ended'
    reason.value = (e as CustomEvent<{ clean: boolean }>).detail?.clean ? 'Verbindung beendet.' : 'Verbindung unterbrochen – läuft die VM noch?'
  })
  rfb = r
}

async function whenHost(): Promise<boolean> {
  for (let i = 0; i < 50; i++) {
    await nextTick()
    if (host.value) return true
    await new Promise((r) => setTimeout(r, 10))
  }
  return false
}

watch(
  () => [props.visible, props.name] as const,
  async ([v]) => {
    if (!v) return stop()
    if (await whenHost()) await start()
  },
  { immediate: true },
)
onBeforeUnmount(stop)

function fullscreen() {
  void host.value?.requestFullscreen?.()
}
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="`Konsole: ${name ?? ''}`"
    :style="{ width: 'min(1100px, 96vw)' }"
    @update:visible="emit('update:visible', $event)"
    @hide="stop"
  >
    <div class="tools">
      <Button label="Strg+Alt+Entf" icon="pi pi-keyboard" size="small" text :disabled="state !== 'live'" data-test="console-cad" @click="rfb?.sendCtrlAltDel()" />
      <Button label="Vollbild" icon="pi pi-expand" size="small" text :disabled="state !== 'live'" @click="fullscreen" />
      <Button v-if="state === 'ended'" label="Neu verbinden" icon="pi pi-refresh" size="small" data-test="console-reconnect" @click="start" />
    </div>
    <div ref="host" class="screen" data-test="console-host" />
    <small class="hint" data-test="console-state">
      {{ state === 'live' ? 'Verbunden' : state === 'ended' ? reason : 'Verbinde …' }}
    </small>
  </Dialog>
</template>

<style scoped>
.screen {
  height: min(620px, 70vh);
  border-radius: var(--bn-radius-xs);
  background: var(--bn-terminal-bg);
  overflow: hidden;
}
.tools {
  display: flex;
  gap: 0.25rem;
  margin-bottom: 0.5rem;
}
.hint {
  display: block;
  margin-top: 0.4rem;
  color: var(--bn-text-soft);
}
</style>
