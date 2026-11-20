<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import Menu from 'primevue/menu'
import { useToast } from 'primevue/usetoast'
import ConsoleDialog from '@/components/vms/ConsoleDialog.vue'
import LogsDialog from '@/components/docker/LogsDialog.vue'
import StackDialog, { type SubmitMode } from '@/components/docker/StackDialog.vue'
import { useAuthStore } from '@/stores/auth'
import { useDockerStore } from '@/stores/docker'
import { useVmsStore } from '@/stores/vms'
import type { ContainerAction, ContainerInfo, StackCheck, StackFiles } from '@/api/docker'
import type { VmAction, VmInfo } from '@/api/vms'
import { moveTile, shiftTile, useTileOrder } from '@/utils/tileOrder'

/** Im Bearbeiten-Modus lassen sich die Kacheln verschieben; sonst sind sie nur Verknüpfungen. */
const props = withDefaults(defineProps<{ editing?: boolean }>(), { editing: false })

const auth = useAuthStore()
const docker = useDockerStore()
const vms = useVmsStore()
const toast = useToast()
const menu = ref<InstanceType<typeof Menu>>()
const items = ref<Record<string, unknown>[]>([])
const busy = ref(false)
const logsFor = ref<ContainerInfo | null>(null)
const consoleFor = ref<string | null>(null)
const showYaml = ref(false)
const yaml = ref<StackFiles | null>(null)
const check = ref<StackCheck | null>(null)

const containers = computed(() => [...docker.containers].sort((a, b) => Number(b.state === 'running') - Number(a.state === 'running') || a.name.localeCompare(b.name)))
const machines = computed(() => [...vms.vms].sort((a, b) => Number(b.state === 'running') - Number(a.state === 'running') || a.name.localeCompare(b.name)))
// --- Reihenfolge: Standard = laufende zuerst, dann nach Name; eigene Anordnung wird im Browser gemerkt ---
type Entry = { id: string; kind: 'c'; item: ContainerInfo } | { id: string; kind: 'v'; item: VmInfo }
const defaultIds = computed(() => [...containers.value.map((c) => `c:${c.name}`), ...machines.value.map((v) => `v:${v.name}`)])
const tileOrder = useTileOrder(() => `bn-quicktiles:${auth.user ?? ''}`, () => defaultIds.value)
const entries = computed<Entry[]>(() => {
  const byId = new Map<string, Entry>()
  for (const c of containers.value) byId.set(`c:${c.name}`, { id: `c:${c.name}`, kind: 'c', item: c })
  for (const v of machines.value) byId.set(`v:${v.name}`, { id: `v:${v.name}`, kind: 'v', item: v })
  return tileOrder.order.value.flatMap((id) => byId.get(id) ?? [])
})
const isCustom = computed(() => tileOrder.order.value.join() !== defaultIds.value.join())
const dragging = ref<string | null>(null)
const slug = (id: string) => (id.startsWith('c:') ? `container-${id.slice(2)}` : `vm-${id.slice(2)}`)

function onDragStart(e: DragEvent, id: string) {
  if (!props.editing) return
  dragging.value = id
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', id)
  }
}
function onDrop(target: string) {
  if (props.editing && dragging.value) tileOrder.set(moveTile(tileOrder.order.value, dragging.value, target))
  dragging.value = null
}
function onHandleKey(e: KeyboardEvent, id: string) {
  const delta = e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : 0
  if (!props.editing || !delta) return
  e.preventDefault()
  tileOrder.set(shiftTile(tileOrder.order.value, entries.value.map((x) => x.id), id, delta))
  void nextTick(() => (document.querySelector(`[data-test="quick-handle-${slug(id)}"]`) as HTMLElement | null)?.focus())
}
defineExpose({ reset: () => tileOrder.reset(), isCustom })

const stackNames = computed(() => docker.stacks.map((s) => s.name))

onMounted(() => {
  void docker.refresh()
  void vms.refresh()
})

const fail = (e: unknown) =>
  toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })

async function run(fn: () => Promise<unknown>, ok: string, detail: string) {
  busy.value = true
  try {
    await fn()
    toast.add({ severity: 'success', summary: ok, detail, life: 4000 })
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

const containerDone: Record<string, string> = { start: 'Container gestartet', stop: 'Container gestoppt', restart: 'Container neu gestartet' }
const containerAct = (c: ContainerInfo, a: ContainerAction) => run(() => docker.containerAction(c.id, a), containerDone[a], c.name)
const vmDone: Record<string, string> = { start: 'VM gestartet', shutdown: 'Herunterfahren angefordert', reboot: 'Neustart angefordert', destroy: 'VM hart ausgeschaltet' }
const vmAct = (v: VmInfo, a: VmAction) => run(() => vms.act(v.name, a), vmDone[a], v.name)

async function editYaml(c: ContainerInfo) {
  try {
    yaml.value = await docker.load(c.stack)
    check.value = null
    showYaml.value = true
  } catch (e) {
    fail(e)
  }
}

async function submitYaml(files: StackFiles, mode: SubmitMode) {
  busy.value = true
  try {
    await docker.update(files)
    yaml.value = files
    if (mode === 'check') {
      check.value = await docker.validate(files.name)
      return
    }
    showYaml.value = false
    toast.add({ severity: 'success', summary: 'Stack gespeichert', detail: files.name, life: 4000 })
    if (mode === 'start') await docker.run(files.name, 'up')
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

// Erreichbare URL aus den veröffentlichten Ports ("0.0.0.0:8080->80/tcp"), nur für laufende Container.
function containerUrl(c: ContainerInfo): string | null {
  if (c.state !== 'running') return null
  for (const p of c.ports) {
    const m = /:(\d+)->(\d+)\/tcp$/.exec(p)
    if (!m) continue
    const https = m[2] === '443' || m[2] === '8443' || m[2] === '9443'
    return `${https ? 'https' : 'http'}://${window.location.hostname}:${m[1]}/`
  }
  return null
}

function openContainer(e: Event, c: ContainerInfo) {
  const running = c.state === 'running'
  items.value = [
    { label: c.name, disabled: true },
    { label: 'YAML bearbeiten', icon: 'pi pi-file-edit', disabled: !c.stack, command: () => editYaml(c) },
    { label: 'Logs', icon: 'pi pi-align-left', command: () => (logsFor.value = c) },
    { separator: true },
    { label: 'Starten', icon: 'pi pi-play', disabled: running, command: () => containerAct(c, 'start') },
    { label: 'Stoppen', icon: 'pi pi-stop', disabled: !running, command: () => containerAct(c, 'stop') },
    { label: 'Neu starten', icon: 'pi pi-refresh', command: () => containerAct(c, 'restart') },
  ]
  menu.value?.toggle(e)
}

const canConsole = (v: VmInfo) => v.state === 'running' || v.state === 'paused'

function openVm(e: Event, v: VmInfo) {
  const running = v.state === 'running'
  items.value = [
    { label: v.name, disabled: true },
    { label: 'Starten', icon: 'pi pi-play', disabled: running, command: () => vmAct(v, 'start') },
    { label: 'Herunterfahren', icon: 'pi pi-power-off', disabled: !running, command: () => vmAct(v, 'shutdown') },
    { label: 'Neu starten', icon: 'pi pi-refresh', disabled: !running, command: () => vmAct(v, 'reboot') },
    { label: 'Hart ausschalten', icon: 'pi pi-stop', disabled: !running, command: () => vmAct(v, 'destroy') },
    { separator: true },
    { label: 'Alle VMs', icon: 'pi pi-desktop', url: '/vms' },
  ]
  menu.value?.toggle(e)
}

const vmLabel: Record<string, string> = { running: 'läuft', shutoff: 'aus', paused: 'pausiert', shuttingdown: 'fährt herunter', crashed: 'abgestürzt', other: 'unbekannt' }
</script>

<template>
  <template v-if="containers.length || machines.length">
    <h3 class="section">Container &amp; virtuelle Maschinen</h3>
    <div class="apps" data-test="quick-tiles">
      <template v-for="e in entries" :key="e.id">
        <div
          v-if="e.kind === 'c'"
          class="app glass has-menu"
          :class="{ editing, dragging: dragging === e.id }"
          :data-test="`quick-container-${e.item.name}`"
          :draggable="editing"
          @dragstart="onDragStart($event, e.id)"
          @dragend="dragging = null"
          @dragover.prevent
          @drop.prevent="onDrop(e.id)"
        >
          <component
            :is="containerUrl(e.item) ? 'a' : 'div'"
            class="app-main"
            :class="{ link: containerUrl(e.item) }"
            :href="containerUrl(e.item) ?? undefined"
            :target="containerUrl(e.item) ? '_blank' : undefined"
            :rel="containerUrl(e.item) ? 'noopener noreferrer' : undefined"
            :data-test="`quick-container-open-${e.item.name}`"
          >
            <span class="app-icon" :class="{ off: e.item.state !== 'running', img: !!docker.iconUrlFor(e.item) }" :style="e.item.state === 'running' && !docker.iconUrlFor(e.item) ? { background: 'linear-gradient(135deg, #2563eb, #60a5fa)' } : {}" :data-test="`quick-container-icon-${e.item.name}`">
              <img v-if="docker.iconUrlFor(e.item)" :src="docker.iconUrlFor(e.item)!" alt="" loading="lazy" @error="($event.target as HTMLImageElement).style.display = 'none'" />
              <i v-else class="pi pi-box" />
            </span>
            <span class="app-text">
              <strong>{{ e.item.name }}</strong>
              <small v-if="e.item.status">{{ e.item.status }}</small>
            </span>
          </component>
          <span class="status" :class="e.item.state === 'running' ? 'ok' : 'bad'" role="img" :aria-label="e.item.state === 'running' ? 'läuft' : 'gestoppt'" :title="e.item.state === 'running' ? 'Läuft' : 'Gestoppt'" :data-test="`quick-container-status-${e.item.name}`" />
          <button v-if="!editing" type="button" class="dots" :data-test="`quick-container-menu-${e.item.name}`" :aria-label="`Container ${e.item.name}: Aktionen`" @click="openContainer($event, e.item)">
            <i class="pi pi-ellipsis-h" />
          </button>
          <button v-else type="button" class="handle" :data-test="`quick-handle-${slug(e.id)}`" :aria-label="`Kachel ${e.item.name} verschieben (Pfeiltasten)`" title="Ziehen oder mit den Pfeiltasten verschieben" @keydown="onHandleKey($event, e.id)">
            <i class="pi pi-arrows-alt" />
          </button>
        </div>
        <div
          v-else
          class="app glass has-menu"
          :class="{ editing, dragging: dragging === e.id }"
          :data-test="`quick-vm-${e.item.name}`"
          :draggable="editing"
          @dragstart="onDragStart($event, e.id)"
          @dragend="dragging = null"
          @dragover.prevent
          @drop.prevent="onDrop(e.id)"
        >
          <component
            :is="canConsole(e.item) ? 'button' : 'div'"
            class="app-main"
            :class="{ link: canConsole(e.item) }"
            :type="canConsole(e.item) ? 'button' : undefined"
            :title="canConsole(e.item) ? 'Konsole öffnen' : undefined"
            :data-test="`quick-vm-open-${e.item.name}`"
            @click="canConsole(e.item) && (consoleFor = e.item.name)"
          >
            <span class="app-icon" :class="{ off: e.item.state === 'shutoff', img: !!vms.iconUrlOf(e.item.name) }" :style="e.item.state === 'running' && !vms.iconUrlOf(e.item.name) ? { background: 'linear-gradient(135deg, #f97316, #fb923c)' } : {}" :data-test="`quick-vm-icon-${e.item.name}`">
              <img v-if="vms.iconUrlOf(e.item.name)" :src="vms.iconUrlOf(e.item.name)!" alt="" loading="lazy" @error="($event.target as HTMLImageElement).style.display = 'none'" />
              <i v-else class="pi pi-desktop" />
            </span>
            <span class="app-text">
              <strong>{{ e.item.name }}</strong>
              <small>VM<template v-if="e.item.state !== 'running' && e.item.state !== 'shutoff'"> · {{ vmLabel[e.item.state] ?? e.item.state }}</template></small>
            </span>
          </component>
          <span class="status" :class="e.item.state === 'running' ? 'ok' : 'bad'" role="img" :aria-label="vmLabel[e.item.state] ?? e.item.state" :title="vmLabel[e.item.state] ?? e.item.state" :data-test="`quick-vm-status-${e.item.name}`" />
          <button v-if="!editing" type="button" class="dots" :data-test="`quick-vm-menu-${e.item.name}`" :aria-label="`VM ${e.item.name}: Aktionen`" @click="openVm($event, e.item)">
            <i class="pi pi-ellipsis-h" />
          </button>
          <button v-else type="button" class="handle" :data-test="`quick-handle-${slug(e.id)}`" :aria-label="`Kachel ${e.item.name} verschieben (Pfeiltasten)`" title="Ziehen oder mit den Pfeiltasten verschieben" @keydown="onHandleKey($event, e.id)">
            <i class="pi pi-arrows-alt" />
          </button>
        </div>
      </template>
    </div>
  </template>
  <Menu ref="menu" :model="items" popup append-to="body" />
  <ConsoleDialog :visible="!!consoleFor" :name="consoleFor" @update:visible="(v) => !v && (consoleFor = null)" />
  <LogsDialog :visible="!!logsFor" :container="logsFor" @update:visible="(v) => !v && (logsFor = null)" />
  <StackDialog v-model:visible="showYaml" :stack="yaml" :existing="stackNames" :busy="busy" :check="check" @submit="submitYaml" />
</template>

<style scoped>
.section {
  margin: 30px 0 12px;
  font-size: 1rem;
  color: var(--bn-text-soft);
  font-weight: 650;
}
.apps {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
  gap: 14px;
}
.app {
  box-sizing: border-box;
  width: 100%;
  font: inherit;
  text-align: left;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 18px;
  color: var(--p-text-color);
  transition: transform 0.18s ease, background 0.18s ease;
}
.app.has-menu {
  position: relative;
  padding: 0;
}
.app-main {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 18px;
  color: inherit;
  text-decoration: none;
  border-radius: inherit;
  font: inherit;
  text-align: left;
  background: transparent;
  border: 0;
}
.app-main.link {
  cursor: pointer;
}
.app-main:not(.link) {
  cursor: default;
}
.dots {
  position: absolute;
  right: 8px;
  bottom: 8px;
  width: 26px;
  height: 26px;
  display: grid;
  place-items: center;
  border: 0;
  border-radius: 50%;
  background: transparent;
  color: var(--bn-text-soft);
  cursor: pointer;
  font-size: 0.85rem;
}
.dots:hover,
.dots:focus-visible {
  background: var(--bn-tile-hover);
  color: var(--p-text-color);
  outline: none;
}
.app.editing {
  cursor: grab;
}
.app.editing .app-main {
  pointer-events: none; /* im Bearbeiten-Modus keine Links/Konsole, damit sich die Kachel greifen lässt */
}
.app.dragging {
  opacity: 0.45;
}
.status {
  position: absolute;
  top: 10px;
  right: 10px;
  width: 11px;
  height: 11px;
  border-radius: 50%;
  pointer-events: none;
}
.status.ok {
  background: #22c55e;
  box-shadow: 0 0 0 3px rgba(34, 197, 94, 0.25);
}
.status.bad {
  background: #ef4444;
  box-shadow: 0 0 0 3px rgba(239, 68, 68, 0.25);
}
.app.editing .status {
  right: 44px;
  top: 17px;
}
.handle {
  position: absolute;
  right: 8px;
  top: 8px;
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border: 0;
  border-radius: 50%;
  background: var(--bn-tile-hover);
  color: var(--p-text-color);
  cursor: grab;
}
.handle:focus-visible {
  outline: 2px solid var(--bn-accent-1);
}
.app:hover {
  transform: translateY(-3px);
  background: var(--bn-tile-hover);
}
.app:focus-visible {
  outline: 2px solid var(--bn-accent-1);
}
.app-icon {
  width: 50px;
  height: 50px;
  flex: none;
  border-radius: 16px;
  display: grid;
  place-items: center;
  color: #fff;
  font-size: 1.3rem;
  background: #64748b;
  box-shadow: 0 8px 20px -8px rgb(0 0 0 / 50%);
}
.app-icon.img {
  background: var(--bn-glass-strong);
  border: 1px solid var(--bn-border-soft);
  overflow: hidden;
}
.app-icon img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  padding: 9px;
}
.app-icon.img.off img {
  filter: grayscale(1);
  opacity: 0.6;
}
.app-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.app-text strong,
.app-text small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.app-text small {
  color: var(--bn-text-soft);
}
</style>
