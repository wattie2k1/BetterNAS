<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import ProgressBar from 'primevue/progressbar'
import Message from 'primevue/message'
import Skeleton from 'primevue/skeleton'
import { api } from '@/api/client'
import RingGauge from '@/components/RingGauge.vue'
import { useAuthStore } from '@/stores/auth'
import { useStorageStore } from '@/stores/storage'
import { storageApi, unprotected, type PoolView, type SnapraidStatus } from '@/api/storage'
import { formatBytes, formatDateTime, formatUptime } from '@/utils/format'

interface SystemInfo {
  hostname: string
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
let timer: ReturnType<typeof setInterval> | undefined

async function load() {
  try {
    info.value = await api.get<SystemInfo>('/api/v1/system/info')
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
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
  timer = setInterval(load, 10000)
})
onUnmounted(() => clearInterval(timer))

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

const dataDisks = computed(() => store.disks.filter((d) => d.role === 'data' || d.role === 'parity'))
const smartWarnings = computed(() => store.disks.filter((d) => d.smart && !d.smart.passed).length)
const missingDisks = computed(() => store.disks.filter((d) => d.state === 'missing').length)

const apps = [
  { label: 'Datenträger', hint: 'Platten verwalten', icon: 'pi pi-database', to: '/speicher/datentraeger', color: ['#0ea5e9', '#22d3ee'] },
  { label: 'Speicherpools', hint: 'Pools & SnapRAID', icon: 'pi pi-box', to: '/speicher/pools', color: ['#10b981', '#34d399'] },
  { label: 'Freigaben', hint: 'Ordner im Netzwerk', icon: 'pi pi-folder-open', to: '/freigaben', color: ['#f59e0b', '#fbbf24'] },
  { label: 'Benutzer', hint: 'Konten & Rechte', icon: 'pi pi-users', to: '/benutzer', color: ['#ec4899', '#f472b6'] },
  { label: 'Gruppen', hint: 'Zugriff bündeln', icon: 'pi pi-sitemap', to: '/benutzer/gruppen', color: ['#a855f7', '#c084fc'] },
  { label: 'Updates', hint: 'System aktuell halten', icon: 'pi pi-download', to: '/system/updates', color: ['#ef4444', '#fb7185'] },
]
const gradient = (c: string[]) => `linear-gradient(135deg, ${c[0]}, ${c[1]})`
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

  <div class="widgets">
    <article class="widget glass storage">
      <header><i class="pi pi-database" /> Speicher</header>
      <div class="storage-body">
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
    </article>

    <article class="widget glass">
      <header><i class="pi pi-microchip" /> Arbeitsspeicher</header>
      <div v-if="info" class="mem">
        <RingGauge :value="memPercent" label="belegt" :size="116" from="#e879f9" to="#6366f1" />
        <div>
          <p class="big">{{ formatBytes(memUsed) }}</p>
          <p class="soft">von {{ formatBytes(info.memTotalBytes) }}</p>
          <p class="soft">{{ info.cpuCount }} CPU-Kerne</p>
        </div>
      </div>
      <Skeleton v-else height="7rem" />
    </article>

    <article class="widget glass">
      <header><i class="pi pi-hard-drive" /> Datenträger</header>
      <div class="facts">
        <div class="fact"><strong>{{ dataDisks.length }}</strong><span>in Verwendung</span></div>
        <div class="fact" :class="{ bad: smartWarnings }"><strong>{{ smartWarnings }}</strong><span>SMART-Warnungen</span></div>
        <div class="fact" :class="{ bad: missingDisks }"><strong>{{ missingDisks }}</strong><span>fehlen</span></div>
      </div>
    </article>

    <article v-if="snap.length" class="widget glass" data-test="snapraid-summary">
      <header><i class="pi pi-shield" /> SnapRAID</header>
      <ul class="snap">
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
    </article>

    <article class="widget glass">
      <header><i class="pi pi-info-circle" /> System</header>
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
    </article>
  </div>

  <h3 class="section">Schnellzugriff</h3>
  <div class="apps">
    <RouterLink v-for="a in apps" :key="a.to" :to="a.to" class="app glass">
      <span class="app-icon" :style="{ background: gradient(a.color) }"><i :class="a.icon" /></span>
      <span class="app-text"><strong>{{ a.label }}</strong><small>{{ a.hint }}</small></span>
    </RouterLink>
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
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 18px;
  text-decoration: none;
  color: var(--p-text-color);
  transition: transform 0.18s ease, background 0.18s ease;
}
.app:hover {
  transform: translateY(-3px);
  background: var(--bn-tile-hover);
}
.app-icon {
  width: 50px;
  height: 50px;
  border-radius: 16px;
  display: grid;
  place-items: center;
  color: #fff;
  font-size: 1.3rem;
  box-shadow: 0 8px 20px -8px rgb(0 0 0 / 50%);
}
.app-text {
  display: flex;
  flex-direction: column;
}
.app-text small {
  color: var(--bn-text-soft);
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
