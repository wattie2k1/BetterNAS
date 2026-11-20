<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import ProgressBar from 'primevue/progressbar'
import Message from 'primevue/message'
import Skeleton from 'primevue/skeleton'
import { api } from '@/api/client'
import RingGauge from '@/components/RingGauge.vue'
import CpuTile from '@/components/CpuTile.vue'
import MemoryTile from '@/components/MemoryTile.vue'
import NetTile from '@/components/NetTile.vue'
import { netRates, type NetIface, type NetRates } from '@/utils/netRate'
import QuickTiles from '@/components/QuickTiles.vue'
import DockerTile from '@/components/docker/DockerTile.vue'
import UsageBar from '@/components/storage/UsageBar.vue'
import { useAuthStore } from '@/stores/auth'
import { useStorageStore } from '@/stores/storage'
import { useNotificationsStore } from '@/stores/notifications'
import { storageApi, unprotected, type PoolView, type SnapraidStatus } from '@/api/storage'
import { formatBytes, formatDateTime, formatUptime } from '@/utils/format'
import { pollWhileVisible } from '@/utils/poll'
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
  net?: NetIface[]
  version: string
}

const auth = useAuthStore()
const store = useStorageStore()
const notifications = useNotificationsStore()
const info = ref<SystemInfo | null>(null)
const snap = ref<{ pool: PoolView; status: SnapraidStatus }[]>([])
const error = ref('')
const cpuPercent = ref<number | null>(null)
/** Letzte Messungen für den Verlauf in der CPU-Kachel (bei 5 s Takt rund 5 Minuten). */
const cpuHistory = ref<number[]>([])
const memHistory = ref<number[]>([])
const netNow = ref<NetRates | null>(null)
const netRx = ref<number[]>([])
const netTx = ref<number[]>([])
let lastNet: { at: number; net: NetIface[] } | null = null
const HISTORY_MAX = 60
let lastTicks: { total: number; idle: number } | null = null
let stopTimer: (() => void) | undefined
let stopSnapTimer: (() => void) | undefined
let loading = false
let loadingSnap = false
let firstCpu: ReturnType<typeof setTimeout> | undefined

async function load() {
  if (loading) return // keine überlappenden Abfragen, wenn eine länger als der Takt dauert
  loading = true
  try {
    const next = await api.get<SystemInfo>('/api/v1/system/info')
    info.value = next
    updateCpu(next)
    updateMem(next)
    updateNet(next)
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading = false
  }
}

/** Auslastung seit der letzten Abfrage aus den kumulierten Zählern des Kernels. */
function updateCpu(i: Pick<SystemInfo, 'cpuTotalTicks' | 'cpuIdleTicks'>) {
  const total = i.cpuTotalTicks
  const idle = i.cpuIdleTicks
  if (lastTicks && total > lastTicks.total) {
    const busy = 1 - (idle - lastTicks.idle) / (total - lastTicks.total)
    cpuPercent.value = Math.max(0, Math.min(100, busy * 100))
    cpuHistory.value = [...cpuHistory.value, cpuPercent.value].slice(-HISTORY_MAX)
  }
  lastTicks = { total, idle }
}

/** Belegung in Prozent für den Verlauf der Speicher-Kachel. */
function updateMem(i: Pick<SystemInfo, 'memTotalBytes' | 'memAvailableBytes'>) {
  if (!i.memTotalBytes) return
  const used = ((i.memTotalBytes - i.memAvailableBytes) / i.memTotalBytes) * 100
  memHistory.value = [...memHistory.value, Math.max(0, Math.min(100, used))].slice(-HISTORY_MAX)
}

/** Datenrate aus den Byte-Zählern der Netzwerkkarten seit der letzten Abfrage. */
function updateNet(i: Pick<SystemInfo, 'net'>, at = Date.now()) {
  if (!i.net?.length) return
  const cur = { at, net: i.net }
  const r = netRates(lastNet, cur)
  lastNet = cur
  if (!r) return
  netNow.value = r
  netRx.value = [...netRx.value, r.rx].slice(-HISTORY_MAX)
  netTx.value = [...netTx.value, r.tx].slice(-HISTORY_MAX)
}

/** Verlauf der letzten Minuten vom Server holen, damit die Graphen schon beim Öffnen gefüllt sind. */
async function loadSamples() {
  try {
    const samples = await api.get<(Pick<SystemInfo, 'cpuTotalTicks' | 'cpuIdleTicks' | 'memTotalBytes' | 'memAvailableBytes' | 'net'> & { at: number })[]>('/api/v1/system/samples')
    for (const x of samples) {
      updateCpu(x)
      updateMem(x)
      updateNet(x, x.at)
    }
  } catch {
    // Verlauf ist optional; die Graphen füllen sich dann wie bisher durch eigene Messungen
  }
}

async function loadSnapraid() {
  if (loadingSnap) return
  loadingSnap = true
  try {
    await store.refresh()
    const pools = store.pools.filter((p) => p.snapraid)
    snap.value = await Promise.all(pools.map(async (pool) => ({ pool, status: await storageApi.snapraidStatus(pool.name) })))
  } catch {
    snap.value = [] // Speicherstatus ist optional; Fehler zeigt die Speicherseite
  } finally {
    loadingSnap = false
  }
}

onMounted(async () => {
  void notifications.loadAlerts()
  loadSnapraid()
  await loadSamples() // vor der ersten Live-Messung, damit die Reihenfolge stimmt
  load()
  if (!cpuHistory.value.length) firstCpu = setTimeout(load, 1500) // ohne Verlauf: zweite Messung, damit die CPU-Kachel schnell einen Wert zeigt
  stopTimer = pollWhileVisible(load, 5000)
  stopSnapTimer = pollWhileVisible(loadSnapraid, 60000) // Sync-/Scrub-Stand ändert sich im Hintergrund
})
onUnmounted(() => {
  stopTimer?.()
  stopSnapTimer?.()
  clearTimeout(firstCpu)
})



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
const TILES = ['alerts', 'storage', 'cpu', 'memory', 'network', 'disks', 'snapraid', 'docker', 'system'] as const
const tileMeta: Record<string, { icon: string; title: string }> = {
  alerts: { icon: 'pi pi-bell', title: 'Meldungen' },
  storage: { icon: 'pi pi-database', title: 'Speicher' },
  cpu: { icon: 'pi pi-bolt', title: 'Prozessor' },
  memory: { icon: 'pi pi-microchip', title: 'Arbeitsspeicher' },
  network: { icon: 'pi pi-arrow-right-arrow-left', title: 'Netzwerk' },
  disks: { icon: 'pi pi-server', title: 'Datenträger' },
  snapraid: { icon: 'pi pi-shield', title: 'SnapRAID' },
  docker: { icon: 'pi pi-box', title: 'Docker-Container' },
  system: { icon: 'pi pi-info-circle', title: 'System' },
}
const tiles = useTileOrder(() => `bn-dashboard-tiles:${auth.user ?? ''}`, TILES)
const visibleTiles = computed(() =>
  tiles.order.value.filter((id) => (id !== 'snapraid' || snap.value.length > 0) && (id !== 'network' || !!info.value?.net?.length) && (id !== 'alerts' || notifications.alerts.length > 0)),
)
const alertsCrit = computed(() => notifications.alerts.some((a) => a.level === 'crit'))
const customOrder = computed(() => tiles.order.value.join() !== TILES.join())
const dragging = ref<string | null>(null)
/** Verschieben ist nur im Bearbeiten-Modus möglich. */
const editing = ref(false)
const quick = ref<{ reset: () => void; isCustom: boolean } | null>(null)
const anyCustom = computed(() => customOrder.value || !!quick.value?.isCustom)
function resetAll() {
  tiles.reset()
  quick.value?.reset()
}

function onDragStart(e: DragEvent, id: string) {
  if (!editing.value) return
  dragging.value = id
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', id)
  }
}
function onDrop(target: string) {
  if (editing.value && dragging.value) tiles.set(moveTile(tiles.order.value, dragging.value, target))
  dragging.value = null
}
function onHandleKey(e: KeyboardEvent, id: string) {
  const delta = e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : 0
  if (!editing.value || !delta) return
  e.preventDefault()
  tiles.set(shiftTile(tiles.order.value, visibleTiles.value, id, delta))
  // Fokus bleibt am verschobenen Griff, auch wenn Vue das Element neu einsetzt.
  void nextTick(() => (document.querySelector(`[data-test="tile-${id}"] .handle`) as HTMLElement | null)?.focus())
}

const dataDisks = computed(() => store.disks.filter((d) => d.role === 'data' || d.role === 'parity'))
const usedDisks = computed(() => dataDisks.value.filter((d) => d.usage && d.state === 'online'))
const smartWarnings = computed(() => store.disks.filter((d) => d.smart && !d.smart.passed).length)
const missingDisks = computed(() => store.disks.filter((d) => d.state === 'missing').length)

</script>

<template>
  <section class="hero">
    <div>
      <h2>{{ greeting }}</h2>
      <p class="page-sub">
        <template v-if="info">{{ info.hostname }} läuft seit {{ formatUptime(info.uptimeSeconds) }} · BetterNAS {{ info.version }}</template>
        <template v-else>Verbinde …</template>
      </p>
    </div>
    <div class="hero-actions">
      <button v-if="editing && anyCustom" type="button" class="edit-btn" data-test="tiles-reset" @click="resetAll"><i class="pi pi-undo" /> Anordnung zurücksetzen</button>
      <button type="button" class="edit-btn" :class="{ on: editing }" data-test="edit-toggle" :aria-pressed="editing" @click="editing = !editing">
        <i :class="editing ? 'pi pi-check' : 'pi pi-pencil'" /> {{ editing ? 'Fertig' : 'Anordnen' }}
      </button>
    </div>
  </section>
  <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

  <div class="widgets">
    <article
      v-for="id in visibleTiles"
      :key="id"
      class="widget glass"
      :class="{ storage: id === 'storage', dragging: dragging === id, editing, crit: id === 'alerts' && alertsCrit }"
      :data-test="`tile-${id}`"
      :draggable="editing"
      @dragstart="onDragStart($event, id)"
      @dragend="dragging = null"
      @dragover.prevent
      @drop.prevent="onDrop(id)"
    >
      <header>
        <i :class="tileMeta[id].icon" />
        <span>{{ tileMeta[id].title }}</span>
        <button
          v-if="editing"
          type="button"
          class="handle"
          :aria-label="`Kachel ${tileMeta[id].title} verschieben (Pfeiltasten)`"
          title="Ziehen oder mit den Pfeiltasten verschieben"
          @keydown="onHandleKey($event, id)"
        >
          <i class="pi pi-arrows-alt" />
        </button>
      </header>

      <div v-if="id === 'alerts'" data-test="alerts-summary">
        <p class="big">{{ notifications.alerts.length }} {{ notifications.alerts.length === 1 ? 'Meldung' : 'Meldungen' }}</p>
        <ul class="alert-list">
          <li v-for="a in notifications.alerts.slice(0, 4)" :key="a.key" :class="a.level">{{ a.title }}</li>
        </ul>
        <RouterLink to="/einstellungen/benachrichtigungen" class="soft">Details ansehen</RouterLink>
      </div>

      <div v-else-if="id === 'storage'" class="storage-body">
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
        <CpuTile v-if="info" :percent="cpuPercent" :history="cpuHistory" :load="info.load ?? []" :cores="info.cpuCount" :model="info.cpuModel" />
        <Skeleton v-else height="7rem" />
      </template>

      <template v-else-if="id === 'memory'">
        <MemoryTile v-if="info" :total-bytes="info.memTotalBytes" :available-bytes="info.memAvailableBytes" :history="memHistory" />
        <Skeleton v-else height="7rem" />
      </template>

      <NetTile v-else-if="id === 'network'" :current="netNow" :rx="netRx" :tx="netTx" />

      <div v-else-if="id === 'disks'" class="facts">
        <div class="fact"><strong>{{ dataDisks.length }}</strong><span>in Verwendung</span></div>
        <div class="fact" :class="{ bad: smartWarnings }"><strong>{{ smartWarnings }}</strong><span>SMART-Warnungen</span></div>
        <div class="fact" :class="{ bad: missingDisks }"><strong>{{ missingDisks }}</strong><span>fehlen</span></div>
        <ul v-if="usedDisks.length" class="disk-usage" data-test="disk-usage">
          <li v-for="d in usedDisks" :key="d.id">
            <div class="row">
              <strong>{{ d.model || d.id }}</strong>
              <span class="soft">{{ d.role === 'parity' ? 'Parität' : d.pool || 'Daten' }}</span>
            </div>
            <UsageBar :usage="d.usage" />
          </li>
        </ul>
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

  <QuickTiles ref="quick" :editing="editing" />
</template>

<style scoped>
.hero {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  flex-wrap: wrap;
}
.hero-actions {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
.edit-btn {
  all: unset;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  font-size: 0.8rem;
  padding: 6px 12px;
  border-radius: 999px;
  color: var(--bn-text-soft);
  background: var(--bn-border-soft);
}
.edit-btn:hover,
.edit-btn.on {
  color: var(--p-text-color);
  background: var(--bn-tile-hover);
}
.edit-btn:focus-visible {
  outline: 2px solid var(--bn-accent-1);
}
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
}
.widget.editing {
  cursor: grab;
  outline: 1px dashed var(--bn-border-soft);
}
.widget.crit {
  border-color: color-mix(in srgb, var(--bn-danger) 55%, transparent);
}
.alert-list {
  list-style: none;
  margin: 0.4rem 0;
  padding: 0;
  font-size: 0.85rem;
}
.alert-list li::before {
  content: '●';
  margin-right: 0.4rem;
  color: var(--bn-warn);
}
.alert-list li.crit::before {
  color: var(--bn-danger);
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
}
.handle:focus-visible {
  outline: 2px solid var(--bn-accent-1);
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
.storage-body {
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
.disk-usage {
  grid-column: 1 / -1;
  list-style: none;
  margin: 4px 0 0;
  padding: 0;
  display: grid;
  gap: 10px;
}
.disk-usage .row {
  display: flex;
  justify-content: space-between;
  font-size: 0.85rem;
}
.fact.bad strong {
  color: var(--bn-danger);
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
