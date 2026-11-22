<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { api } from '@/api/client'
import { formatBytes } from '@/utils/format'
import { formatRate, rateScale, type NetRates } from '@/utils/netRate'

/** Inhalt der Netzwerk-Kachel: aktuelle Rate, Verlauf von Empfang und Senden, Datenmengen je Tag, Monat und Jahr. */
const props = defineProps<{
  current: NetRates | null
  /** Raten der letzten Messungen in Byte/s, älteste zuerst. */
  rx: number[]
  tx: number[]
}>()

const uid = useId()
const W = 300
const H = 80
/** Die Skala wächst mit dem Spitzenwert, mindestens bis 1 MB/s, damit Rauschen nicht wie Last aussieht. */
const peak = computed(() => Math.max(1e6, ...props.rx, ...props.tx))
const path = (vals: number[]) =>
  vals.map((v, i) => `${i ? 'L' : 'M'}${((i / Math.max(1, vals.length - 1)) * W).toFixed(1)} ${(H - (v / peak.value) * H).toFixed(1)}`).join(' ')
const scale = computed(() => rateScale(peak.value))
const rxLine = computed(() => path(props.rx))
const txLine = computed(() => path(props.tx))
const rxArea = computed(() => (rxLine.value ? `${rxLine.value} L${W} ${H} L0 ${H} Z` : ''))

interface TrafficEntry {
  key: string
  rx: number
  tx: number
}
interface Traffic {
  days: TrafficEntry[]
  months: TrafficEntry[]
  years: TrafficEntry[]
}
const PERIODS = [
  { id: 'days', label: 'Tag' },
  { id: 'months', label: 'Monat' },
  { id: 'years', label: 'Jahr' },
] as const
const period = ref<(typeof PERIODS)[number]['id']>('days')
const traffic = ref<Traffic>({ days: [], months: [], years: [] })
/** Neueste zuerst, Balken relativ zum größten Wert des Zeitraums. */
const rows = computed(() => {
  const list = [...(traffic.value[period.value] ?? [])].reverse()
  const max = Math.max(1, ...list.map((e) => e.rx + e.tx))
  return list.map((e) => ({ ...e, label: periodLabel(e.key), rxPct: (e.rx / max) * 100, txPct: (e.tx / max) * 100 }))
})
const total = computed(() => rows.value.reduce((a, r) => ({ rx: a.rx + r.rx, tx: a.tx + r.tx }), { rx: 0, tx: 0 }))
function periodLabel(key: string): string {
  const [y, m, d] = key.split('-')
  if (d) return `${d}.${m}.${y}`
  if (m) return `${m}/${y}`
  return y
}
async function loadTraffic() {
  try {
    const t = await api.get<Partial<Traffic>>('/api/v1/system/traffic')
    traffic.value = { days: t?.days ?? [], months: t?.months ?? [], years: t?.years ?? [] }
  } catch {
    /* Statistik ist optional */
  }
}
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void loadTraffic()
  timer = setInterval(loadTraffic, 60_000)
})
onBeforeUnmount(() => clearInterval(timer))
const peakRx = computed(() => Math.max(0, ...props.rx))
const peakTx = computed(() => Math.max(0, ...props.tx))
</script>

<template>
  <div class="net" data-test="net-tile">
    <div class="rates">
      <div class="rate down">
        <span><i class="pi pi-arrow-down" /> Empfangen</span>
        <strong data-test="net-rx">{{ current ? formatRate(current.rx) : '–' }}</strong>
        <small v-if="rx.length">Spitze {{ formatRate(peakRx) }}</small>
      </div>
      <div class="rate up">
        <span><i class="pi pi-arrow-up" /> Senden</span>
        <strong data-test="net-tx">{{ current ? formatRate(current.tx) : '–' }}</strong>
        <small v-if="tx.length">Spitze {{ formatRate(peakTx) }}</small>
      </div>
    </div>

    <figure class="history" data-test="net-history">
      <div v-if="rx.length >= 2" class="plot">
        <div class="axis" aria-hidden="true" data-test="net-axis">
          <span>{{ scale.format(peak) }}</span><span>{{ scale.format(peak / 2) }}</span><span>0</span>
        </div>
        <svg :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" role="img" aria-label="Verlauf der Datenrate: Empfangen und Senden">
        <defs>
          <linearGradient :id="`fill-${uid}`" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="#22d3ee" stop-opacity="0.4" />
            <stop offset="100%" stop-color="#22d3ee" stop-opacity="0.02" />
          </linearGradient>
        </defs>
        <line v-for="g in [0, 50, 100]" :key="g" x1="0" x2="300" :y1="H - (g / 100) * H" :y2="H - (g / 100) * H" class="grid" vector-effect="non-scaling-stroke" />
        <path :d="rxArea" :fill="`url(#fill-${uid})`" />
        <path :d="rxLine" class="line rx" vector-effect="non-scaling-stroke" />
        <path :d="txLine" class="line tx" vector-effect="non-scaling-stroke" />
        </svg>
      </div>
      <p v-else class="empty">Der Verlauf füllt sich mit den nächsten Messungen.</p>
      <figcaption v-if="rx.length >= 2"><span>früher</span><span data-test="net-unit">{{ scale.unit }}</span><span>jetzt</span></figcaption>
    </figure>

    <section class="traffic" data-test="net-traffic">
      <header>
        <strong>Datenmenge</strong>
        <div class="tabs" role="tablist">
          <button v-for="p in PERIODS" :key="p.id" type="button" role="tab" :aria-selected="period === p.id" :class="{ on: period === p.id }" :data-test="`net-period-${p.id}`" @click="period = p.id">{{ p.label }}</button>
        </div>
      </header>
      <p v-if="!rows.length" class="empty" data-test="net-traffic-empty">Die Statistik füllt sich ab jetzt; Messungen vor dem Update fehlen.</p>
      <template v-else>
        <p class="sum soft" data-test="net-traffic-total">
          Summe: <i class="pi pi-arrow-down" /> {{ formatBytes(total.rx) }} · <i class="pi pi-arrow-up" /> {{ formatBytes(total.tx) }}
        </p>
        <ul class="vol" data-test="net-traffic-rows">
          <li v-for="r in rows" :key="r.key">
            <span class="lbl">{{ r.label }}</span>
            <span class="stack" :title="`Empfangen ${formatBytes(r.rx)}, Gesendet ${formatBytes(r.tx)}`">
              <span class="b rx" :style="{ width: `${r.rxPct}%` }" />
              <span class="b tx" :style="{ width: `${r.txPct}%` }" />
            </span>
            <span class="val"><i class="pi pi-arrow-down" /> {{ formatBytes(r.rx) }}</span>
            <span class="val"><i class="pi pi-arrow-up" /> {{ formatBytes(r.tx) }}</span>
          </li>
        </ul>
      </template>
    </section>
  </div>
</template>

<style scoped>
.net {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.rates {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}
.rate {
  display: flex;
  flex-direction: column;
  padding: 8px 12px;
  border-radius: 12px;
  background: var(--bn-border-soft);
  border-left: 3px solid var(--bn-accent-2);
}
.rate.up {
  border-left-color: var(--bn-accent-3);
}
.rate span,
.rate small {
  font-size: 0.72rem;
  color: var(--bn-text-soft);
}
.rate strong {
  font-size: 1.3rem;
  letter-spacing: -0.02em;
  color: var(--p-text-color, inherit);
}
.history {
  margin: 0;
}
.plot {
  display: flex;
  gap: 6px;
  align-items: stretch;
}
.axis {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  height: 5rem;
  flex: none;
  text-align: right;
  font-size: 0.62rem;
  line-height: 1;
  color: var(--bn-text-soft);
  font-variant-numeric: tabular-nums;
}
.plot svg {
  flex: 1;
  min-width: 0;
}
svg {
  width: 100%;
  height: 5rem;
  display: block;
  border-radius: 8px;
  background: color-mix(in srgb, var(--bn-border-soft) 35%, transparent);
}
.grid {
  stroke: var(--bn-border-soft);
  stroke-width: 1;
  stroke-dasharray: 3 4;
}
.line {
  fill: none;
  stroke-width: 2;
  stroke-linejoin: round;
}
.line.rx {
  stroke: #22d3ee;
}
.line.tx {
  stroke: #e879f9;
}
figcaption {
  display: flex;
  justify-content: space-between;
  font-size: 0.68rem;
  color: var(--bn-text-soft);
  margin-top: 2px;
}
.empty {
  margin: 0;
  font-size: 0.85rem;
  color: var(--bn-text-soft);
}
.traffic header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
}
.tabs {
  display: flex;
  gap: 2px;
  padding: 2px;
  border-radius: 99px;
  background: var(--bn-border-soft);
}
.tabs button {
  border: 0;
  background: transparent;
  color: var(--bn-text-soft);
  font: inherit;
  font-size: 0.78rem;
  padding: 2px 10px;
  border-radius: 99px;
  cursor: pointer;
}
.tabs button.on {
  background: var(--bn-accent-2);
  color: #fff;
}
.sum {
  margin: 0 0 6px;
}
.soft {
  color: var(--bn-text-soft);
  font-size: 0.8rem;
}
.vol {
  list-style: none;
  padding: 0;
  margin: 0;
  max-height: 12rem;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.vol li {
  display: grid;
  grid-template-columns: 5.2rem 1fr 5.6rem 5.6rem;
  gap: 8px;
  align-items: center;
  font-size: 0.78rem;
}
.lbl {
  color: var(--bn-text-soft);
}
.stack {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.b {
  display: block;
  height: 4px;
  border-radius: 99px;
  min-width: 1px;
}
.b.rx {
  background: #22d3ee;
}
.b.tx {
  background: #e879f9;
}
.val {
  text-align: right;
  white-space: nowrap;
}
.val i {
  font-size: 0.65rem;
  color: var(--bn-text-soft);
}
</style>
