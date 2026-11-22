<script setup lang="ts">
import { computed, useId } from 'vue'

/** Flächendiagramm der letzten Messungen (0–100 %), älteste zuerst. */
const props = defineProps<{ history: number[]; from: string; to: string }>()

/** Beschriftung der Skala links, oben nach unten. */
const ticks = ['100', '50', '0']

const uid = useId()
const W = 300
const H = 80
const points = computed(() => {
  const n = props.history.length
  return props.history.map((v, i) => [n === 1 ? W : (i / (n - 1)) * W, H - (Math.max(0, Math.min(100, v)) / 100) * H] as const)
})
const line = computed(() => points.value.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)} ${y.toFixed(1)}`).join(' '))
const area = computed(() => (line.value ? `${line.value} L${W} ${H} L${points.value[0][0].toFixed(1)} ${H} Z` : ''))
const label = computed(() => `Verlauf der letzten Minuten: von ${Math.round(Math.min(...props.history))} bis ${Math.round(Math.max(...props.history))} Prozent`)
</script>

<template>
  <figure class="history">
    <div v-if="history.length >= 2" class="plot">
      <div class="axis" aria-hidden="true" data-test="history-axis"><span v-for="t in ticks" :key="t">{{ t }}</span></div>
      <svg :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" role="img" :aria-label="label">
      <defs>
        <linearGradient :id="`fill-${uid}`" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" :stop-color="from" stop-opacity="0.45" />
          <stop offset="100%" :stop-color="to" stop-opacity="0.02" />
        </linearGradient>
        <linearGradient :id="`stroke-${uid}`" x1="0" y1="1" x2="0" y2="0">
          <stop offset="0%" :stop-color="from" />
          <stop offset="100%" :stop-color="to" />
        </linearGradient>
      </defs>
      <line v-for="g in [0, 50, 100]" :key="g" x1="0" x2="300" :y1="H - (g / 100) * H" :y2="H - (g / 100) * H" class="grid" vector-effect="non-scaling-stroke" />
      <path :d="area" :fill="`url(#fill-${uid})`" />
      <path :d="line" fill="none" :stroke="`url(#stroke-${uid})`" stroke-width="2" stroke-linejoin="round" vector-effect="non-scaling-stroke" />
      </svg>
    </div>
    <p v-else class="empty">Der Verlauf füllt sich mit den nächsten Messungen.</p>
    <figcaption v-if="history.length >= 2"><span>früher</span><span data-test="history-unit">%</span><span>jetzt</span></figcaption>
  </figure>
</template>

<style scoped>
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
</style>
