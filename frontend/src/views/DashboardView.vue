<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import ProgressBar from 'primevue/progressbar'
import Message from 'primevue/message'
import Skeleton from 'primevue/skeleton'
import { api } from '@/api/client'
import RingGauge from '@/components/RingGauge.vue'
import DockerTile from '@/components/docker/DockerTile.vue'
import { useAuthStore } from '@/stores/auth'
import { useStorageStore } from '@/stores/storage'
import { storageApi, unprotected, type PoolView, type SnapraidStatus } from '@/api/storage'
import { formatBytes, formatDateTime, formatUptime } from '@/utils/format'
import { moveTile, shiftTile, useTileOrder } from '@/utils/tileOrder'

interface SystemInfo {
  hostname: string
  cpuModel: string
  cpuTotalTicks: number
  cpuIdleTicks: number
  load: [number, number, number]
  kernel: string
  os: string
  uptimeSeconds: number
  cpuCount: number
  memTotalBytes: number
  memAvailableBytes: number
  version: string
}

const auth = useAuthStore()
const store = useStorageStore()
const info = ref<SystemInfo | null>(null)
const snap = ref<{ pool: PoolView; status: SnapraidStatus }[]>([])
const error = ref('')
const cpuPercent = ref<number | null>(null)
let lastTicks: { total: number; idle: number } | null = null
let timer: ReturnType<typeof setInterval> | undefined
let firstCpu: ReturnType<typeof setTimeout> | undefined

async function load() {
  try {
    const next = await api.get<SystemInfo>('/api/v1/system/info')
    info.value = next
    updateCpu(next)
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

/** Auslastung seit der letzten Abfrage aus den kumulierten Zählern des Kernels. */
function updateCpu(i: SystemInfo) {
  const total = i.cpuTotalTicks
  const idle = i.cpuIdleTicks
  if (lastTicks && total > lastTicks.total) {
    const busy = 1 - (idle - lastTicks.idle) / (total - lastTicks.total)
    cpuPercent.value = Math.max(0, Math.min(100, busy * 100))
  }
  lastTicks = { total, idle }
}

async function loadSnapraid() {
  try {
    await store.refresh()
    const pools = store.pools.filter((p) => p.snapraid)
    snap.value = await Promise.all(pools.map(async (pool) => ({ pool, status: await storageApi.snapraidStatus(pool.name) })))
  } catch {
    snap.value = [] // Speicherstatus ist optional; Fehler zeigt die Speicherseite
  }
}

onMounted(() => {
  load()
  loadSnapraid()
  firstCpu = setTimeout(load, 1500) // zweite Messung, damit die CPU-Kachel schnell einen Wert zeigt
  timer = setInterval(load, 5000)
})
onUnmounted(() => {
  clearInterval(timer)
  clearTimeout(firstCpu)
})

const memUsed = computed(() => (info.value ? info.value.memTotalBytes - info.value.memAvailableBytes : 0))
const memPercent = computed(() => (info.value ? Math.round((memUsed.value / info.value.memTotalBytes) * 100) : 0))


const greeting = computed(() => {
  const h = new Date().getHours()
  const g = h < 5 ? 'Gute Nacht' : h < 11 ? 'Guten Morgen' : h < 17 ? 'Guten Tag' : 'Guten Abend'
  return `${g}, ${auth.user ?? ''}`.trim().replace(/,$/, '')
})

const mountedPools = computed(() => store.pools.filter((p) => p.mounted && p.sizeBytes > 0))
const storageTotal = computed(() => mountedPools.value.reduce((a, p) => a + p.sizeBytes, 0))
const storageUsed = computed(() => mountedPools.value.reduce((a, p) => a + (p.sizeBytes - p.freeBytes), 0))
const storagePercent = computed(() => (storageTotal.value ? (storageUsed.value / storageTotal.value) * 100 : 0))
const poolPercent = (p: PoolView) => (p.sizeBytes ? Math.round(((p.sizeBytes - p.freeBytes) / p.sizeBytes) * 100) : 0)

// --- Kacheln: vom Nutzer sortierbar, Reihenfolge wird im Browser gemerkt ---
const TILES = ['storage', 'cpu', 'memory', 'disks', 'snapraid', 'docker', 'system'] as const
const tileMeta: Record<string, { icon: string; title: string }> = {
  storage: { icon: 'pi pi-database', title: 'Speicher' },
  cpu: { icon: 'pi pi-bolt', title: 'Prozessor' },
  memory: { icon: 'pi pi-microchip', title: 'Arbeitsspeicher' },
  disks: { icon: 'pi pi-hard-drive', title: 'Datenträger' },
  snapraid: { icon: 'pi pi-shield', title: 'SnapRAID' },
  docker: { icon: 'pi pi-box', title: 'Docker-Container' },
  system: { icon: 'pi pi-info-circle', title: 'System' },
}
const tiles = useTileOrder(() => `bn-dashboard-tiles:${auth.user ?? ''}`, TILES)
const visibleTiles = computed(() => tiles.order.value.filter((id) => id !== 'snapraid' || snap.value.length > 0))
const customOrder = computed(() => tiles.order.value.join() !== TILES.join())
const dragging = ref<string | null>(null)

function onDragStart(e: DragEvent, id: string) {
  dragging.value = id
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', id)
  }
}
function onDrop(target: string) {
  if (dragging.value) tiles.set(moveTile(tiles.order.value, dragging.value, target))
  dragging.value = null
}
function onHandleKey(e: KeyboardEvent, id: string) {
  const delta = e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : 0
  if (!delta) return
  e.preventDefault()
  tiles.set(shiftTile(tiles.order.value, visibleTiles.value, id, delta))
  // Fokus bleibt am verschobenen Griff, auch wenn Vue das Element neu einsetzt.
  void nextTick(() => (document.querySelector(`[data-test="tile-${id}"] .handle`) as HTMLElement | null)?.focus())
}

const dataDisks = computed(() => store.disks.filter((d) => d.role === 'data' || d.role === 'parity'))
const smartWarnings = computed(() => store.disks.filter((d) => d.smart && !d.smart.passed).length)
const missingDisks = computed(() => store.disks.filter((d) => d.state === 'missing').length)

</script>

<template>
  <section class="hero">
    <h2>{{ greeting }}</h2>
    <p class="page-sub">
      <template v-if="info">{{ info.hostname }} läuft seit {{ formatUptime(info.uptimeSeconds) }} · BetterNAS {{ info.version }}</template>
      <template v-else>Verbinde …</template>
    </p>
  </section>
  <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

  <div v-if="customOrder" class="reset">
    <button type="button" data-test="tiles-reset" @click="tiles.reset()"><i class="pi pi-undo" /> Anordnung zurücksetzen</button>
  </div>
  <div class="widgets">
    <article
      v-for="id in visibleTiles"
      :key="id"
      class="widget glass"
      :class="{ storage: id === 'storage', dragging: dragging === id }"
      :data-test="`tile-${id}`"
      draggable="true"
      @dragstart="onDragStart($event, id)"
      @dragend="dragging = null"
      @dragover.prevent
      @drop.prevent="onDrop(id)"
    >
      <header>
        <i :class="tileMeta[id].icon" />
        <span>{{ tileMeta[id].title }}</span>
        <button
          type="button"
          class="handle"
          :aria-label="`Kachel ${tileMeta[id].title} verschieben (Pfeiltasten)`"
          title="Ziehen oder mit den Pfeiltasten verschieben"
          @keydown="onHandleKey($event, id)"
        >
          <i class="pi pi-arrows-alt" />
        </button>
      </header>

      <div v-if="id === 'storage'" class="storage-body">
        <RingGauge :value="storagePercent" label="belegt" />
        <div class="storage-side">
          <template v-if="mountedPools.length">
            <p class="big">{{ formatBytes(storageUsed) }}<small> von {{ formatBytes(storageTotal) }}</small></p>
            <ul class="pools">
              <li v-for="p in mountedPools.slice(0, 4)" :key="p.name">
                <div class="row"><span>{{ p.name }}</span><span class="soft">{{ poolPercent(p) }} %</span></div>
                <ProgressBar :value="poolPercent(p)" :show-value="false" />
              </li>
            </ul>
          </template>
          <p v-else class="soft">Noch kein Speicherpool. Lege unter „Speicherpools“ einen an.</p>
        </div>
      </div>

      <template v-else-if="id === 'cpu'">
        <div v-if="info" class="mem" data-test="cpu-tile">
          <RingGauge :value="cpuPercent ?? 0" :label="cpuPercent === null ? 'misst …' : 'ausgelastet'" :size="116" from="#f59e0b" to="#ef4444" />
          <div>
            <p class="big" data-test="cpu-percent">{{ cpuPercent === null ? '–' : `${Math.round(cpuPercent)} %` }}</p>
            <p class="soft">{{ info.cpuCount }} Kerne</p>
            <p class="soft" data-test="cpu-load">Last {{ (info.load ?? []).map((l) => l.toFixed(2)).join(' · ') }}</p>
          </div>
        </div>
        <Skeleton v-else height="7rem" />
        <p v-if="info?.cpuModel" class="soft model">{{ info.cpuModel }}</p>
      </template>

      <template v-else-if="id === 'memory'">
        <div v-if="info" class="mem">
          <RingGauge :value="memPercent" label="belegt" :size="116" from="#e879f9" to="#6366f1" />
          <div>
            <p class="big">{{ formatBytes(memUsed) }}</p>
            <p class="soft">von {{ formatBytes(info.memTotalBytes) }}</p>
          </div>
        </div>
        <Skeleton v-else height="7rem" />
      </template>

      <div v-else-if="id === 'disks'" class="facts">
        <div class="fact"><strong>{{ dataDisks.length }}</strong><span>in Verwendung</span></div>
        <div class="fact" :class="{ bad: smartWarnings }"><strong>{{ smartWarnings }}</strong><span>SMART-Warnungen</span></div>
        <div class="fact" :class="{ bad: missingDisks }"><strong>{{ missingDisks }}</strong><span>fehlen</span></div>
      </div>

      <ul v-else-if="id === 'snapraid'" class="snap" data-test="snapraid-summary">
        <li v-for="s in snap" :key="s.pool.name">
          <div class="row">
            <strong>{{ s.pool.name }}</strong>
            <span class="soft">
              <template v-if="s.status.running">läuft gerade</template>
              <template v-else-if="s.status.lastSync">Sync {{ formatDateTime(s.status.lastSync.at) }}<template v-if="s.status.lastSync.result !== 'ok'"> ({{ s.status.lastSync.result === 'blocked' ? 'gestoppt' : 'Fehler' }})</template></template>
              <template v-else>noch nicht synchronisiert</template>
            </span>
          </div>
          <div v-if="s.status.diff" class="soft">{{ unprotected(s.status.diff) }} ungeschützte Änderungen</div>
        </li>
      </ul>

      <DockerTile v-else-if="id === 'docker'" />

      <template v-else-if="id === 'system'">
        <dl v-if="info">
          <dt>Gerätename</dt>
          <dd>{{ info.hostname }}</dd>
          <dt>Betriebssystem</dt>
          <dd>{{ info.os }}</dd>
          <dt>Kernel</dt>
          <dd>{{ info.kernel }}</dd>
          <dt>Laufzeit</dt>
          <dd>{{ formatUptime(info.uptimeSeconds) }}</dd>
        </dl>
        <Skeleton v-else height="7rem" />
      </template>
    </article>
  </div>

</template>

<style scoped>
.hero h2 {
  margin: 0;
  font-size: 1.5rem;
  letter-spacing: -0.02em;
}
.hero p {
  margin: 0.25rem 0 1.5rem;
}
.widgets {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 18px;
}
.widget {
  padding: 20px 22px;
  cursor: grab;
}
.widget.dragging {
  opacity: 0.45;
}
.widgets {
  grid-auto-flow: dense;
}
.handle {
  all: unset;
  margin-left: auto;
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  cursor: grab;
  opacity: 0;
  transition: opacity 0.15s ease;
}
.widget:hover .handle,
.handle:focus-visible {
  opacity: 1;
}
.handle:focus-visible {
  outline: 2px solid var(--bn-accent-1);
}
.reset {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 10px;
}
.reset button {
  all: unset;
  cursor: pointer;
  font-size: 0.8rem;
  color: var(--bn-text-soft);
}
.reset button:hover {
  color: var(--p-text-color);
}
.model {
  margin-top: 12px;
  overflow-wrap: anywhere;
}
.widget.storage {
  grid-column: span 2;
}
.widget header {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-weight: 650;
  margin-bottom: 14px;
  color: var(--bn-text-soft);
  font-size: 0.9rem;
}
.storage-body,
.mem {
  display: flex;
  align-items: center;
  gap: 22px;
}
.storage-side {
  flex: 1;
  min-width: 0;
}
.big {
  margin: 0;
  font-size: 1.5rem;
  font-weight: 700;
  letter-spacing: -0.02em;
}
.big small {
  font-size: 0.85rem;
  font-weight: 500;
  color: var(--bn-text-soft);
}
.soft {
  color: var(--bn-text-soft);
  margin: 0.2rem 0 0;
  font-size: 0.85rem;
}
.pools,
.snap {
  list-style: none;
  padding: 0;
  margin: 12px 0 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.row {
  display: flex;
  justify-content: space-between;
  gap: 0.5rem;
  margin-bottom: 4px;
}
.facts {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
}
.fact {
  border-radius: var(--bn-radius-sm);
  padding: 14px 8px;
  text-align: center;
  background: var(--bn-border-soft);
}
.fact strong {
  display: block;
  font-size: 1.8rem;
  letter-spacing: -0.03em;
}
.fact span {
  font-size: 0.75rem;
  color: var(--bn-text-soft);
}
.fact.bad strong {
  color: #f43f5e;
}
dl {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 0.5rem 1rem;
  margin: 0;
}
dt {
  color: var(--bn-text-soft);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
@media (max-width: 800px) {
  .widget.storage {
    grid-column: auto;
  }
  .storage-body {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
