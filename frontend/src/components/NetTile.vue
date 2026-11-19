<script setup lang="ts">
import { computed, useId } from 'vue'
import { formatRate, ifaceLoad, type NetRates } from '@/utils/netRate'

/** Inhalt der Netzwerk-Kachel: aktuelle Rate, Verlauf von Empfang und Senden, Auslastung je Schnittstelle. */
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
const rxLine = computed(() => path(props.rx))
const txLine = computed(() => path(props.tx))
const rxArea = computed(() => (rxLine.value ? `${rxLine.value} L${W} ${H} L0 ${H} Z` : ''))
const ifaces = computed(() => (props.current?.ifaces ?? []).filter((i) => i.up || i.rx > 0 || i.tx > 0))
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
      <svg v-if="rx.length >= 2" :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" role="img" aria-label="Verlauf der Datenrate: Empfangen und Senden">
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
      <p v-else class="empty">Der Verlauf füllt sich mit den nächsten Messungen.</p>
      <figcaption v-if="rx.length >= 2"><span>früher</span><span>Skala bis {{ formatRate(peak) }}</span><span>jetzt</span></figcaption>
    </figure>

    <ul v-if="ifaces.length" class="ifaces" data-test="net-ifaces">
      <li v-for="i in ifaces" :key="i.name">
        <div class="row">
          <strong>{{ i.name }}</strong>
          <span class="soft">
            <template v-if="!i.up">getrennt</template>
            <template v-else-if="i.speedMbps">{{ i.speedMbps >= 1000 ? `${i.speedMbps / 1000} GBit/s` : `${i.speedMbps} MBit/s` }}</template>
            <template v-else>verbunden</template>
          </span>
        </div>
        <div v-if="ifaceLoad(i) !== null" class="bar" :title="`${Math.round(ifaceLoad(i)!)} % der Leitungsgeschwindigkeit`">
          <span :style="{ width: `${Math.max(1, ifaceLoad(i)!)}%` }" />
        </div>
      </li>
    </ul>
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
.ifaces {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.row {
  display: flex;
  justify-content: space-between;
  font-size: 0.9rem;
}
.soft {
  color: var(--bn-text-soft);
  font-size: 0.85rem;
}
.bar {
  height: 6px;
  border-radius: 99px;
  background: var(--bn-border-soft);
  margin-top: 4px;
  overflow: hidden;
}
.bar span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, #22d3ee, #6366f1);
  transition: width 0.6s ease;
}
</style>
