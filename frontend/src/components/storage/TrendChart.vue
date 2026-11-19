<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  title: string
  unit?: string
  values: number[]
  /** Zeitstempel (Unix-Sekunden) je Wert, für die Beschriftung der Achse. */
  times: number[]
  /** Ab diesem Wert wird der Verlauf als Warnung eingefärbt. */
  warnAt?: number
}>()

const W = 300
const H = 64
const PAD = 4

const last = computed(() => (props.values.length ? props.values[props.values.length - 1] : undefined))
const min = computed(() => Math.min(...props.values))
const max = computed(() => Math.max(...props.values))
const warn = computed(() => props.warnAt !== undefined && max.value >= props.warnAt)

const path = computed(() => {
  const n = props.values.length
  if (n < 2) return ''
  const lo = min.value
  const span = max.value - lo || 1
  return props.values
    .map((v, i) => {
      const x = PAD + (i / (n - 1)) * (W - 2 * PAD)
      const y = H - PAD - ((v - lo) / span) * (H - 2 * PAD)
      return `${i ? 'L' : 'M'}${x.toFixed(1)} ${y.toFixed(1)}`
    })
    .join(' ')
})
const area = computed(() => (path.value ? `${path.value} L${W - PAD} ${H - PAD} L${PAD} ${H - PAD} Z` : ''))
const date = (ts?: number) => (ts ? new Date(ts * 1000).toLocaleDateString('de-DE', { day: '2-digit', month: '2-digit' }) : '')
const fmt = (v: number | undefined) => (v === undefined ? '–' : `${v.toLocaleString('de-DE')}${props.unit ? ' ' + props.unit : ''}`)
</script>

<template>
  <figure class="chart" :class="{ warn }" data-test="trend-chart">
    <figcaption>
      <span>{{ title }}</span>
      <strong>{{ fmt(last) }}</strong>
    </figcaption>
    <svg v-if="values.length >= 2" :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" role="img" :aria-label="`${title}: von ${fmt(min)} bis ${fmt(max)}`">
      <path :d="area" class="area" />
      <path :d="path" class="line" vector-effect="non-scaling-stroke" />
    </svg>
    <p v-else class="empty">Noch zu wenig Messpunkte – der Verlauf füllt sich stündlich.</p>
    <div v-if="values.length >= 2" class="axis">
      <span>{{ date(times[0]) }} · min {{ fmt(min) }}</span>
      <span>max {{ fmt(max) }} · {{ date(times[times.length - 1]) }}</span>
    </div>
  </figure>
</template>

<style scoped>
.chart {
  margin: 0;
  color: var(--bn-accent-1);
}
.chart.warn {
  color: var(--bn-warn);
}
figcaption {
  display: flex;
  justify-content: space-between;
  font-size: 0.85rem;
  color: var(--bn-text-soft);
}
figcaption strong {
  color: inherit;
}
svg {
  width: 100%;
  height: 4rem;
  display: block;
  margin-top: 0.25rem;
}
.line {
  fill: none;
  stroke: currentColor;
  stroke-width: 2;
  stroke-linejoin: round;
}
.area {
  fill: currentColor;
  opacity: 0.12;
}
.axis {
  display: flex;
  justify-content: space-between;
  font-size: 0.7rem;
  color: var(--bn-text-soft);
}
.empty {
  font-size: 0.8rem;
  color: var(--bn-text-soft);
  margin: 0.5rem 0;
}
</style>
