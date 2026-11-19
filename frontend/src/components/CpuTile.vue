<script setup lang="ts">
import { computed } from 'vue'
import RingGauge from '@/components/RingGauge.vue'
import UsageHistory from '@/components/UsageHistory.vue'

/**
 * Inhalt der Prozessor-Kachel: Anzeige der aktuellen Auslastung, Verlauf der letzten Minuten
 * und Systemlast (1/5/15 Minuten) im Verhältnis zur Kernzahl.
 */
const props = defineProps<{
  /** Aktuelle Auslastung in Prozent, null solange noch keine zweite Messung vorliegt. */
  percent: number | null
  /** Auslastungen der letzten Messungen (0–100), älteste zuerst. */
  history: number[]
  load: number[]
  cores: number
  model?: string
}>()

const stats = computed(() => {
  const h = props.history
  if (!h.length) return null
  return { min: Math.min(...h), max: Math.max(...h), avg: h.reduce((a, b) => a + b, 0) / h.length }
})
const pct = (v: number) => `${Math.round(v)} %`

/** Last je Kern: ab 1,0 ist der Prozessor voll ausgelastet, davor ist noch Luft. */
const loadLevel = (l: number) => {
  const perCore = props.cores > 0 ? l / props.cores : l
  return perCore >= 1 ? 'bad' : perCore >= 0.7 ? 'warn' : 'ok'
}
const loads = computed(() =>
  (props.load ?? []).slice(0, 3).map((l, i) => ({ label: ['1 Min.', '5 Min.', '15 Min.'][i], value: l, level: loadLevel(l) })),
)
</script>

<template>
  <div class="cpu" data-test="cpu-tile">
    <div class="top">
      <RingGauge :value="percent ?? 0" :label="percent === null ? 'misst …' : 'ausgelastet'" :size="116" from="#f59e0b" to="#ef4444" />
      <div class="side">
        <p class="big" data-test="cpu-percent">{{ percent === null ? '–' : pct(percent) }}</p>
        <p class="soft">{{ cores }} {{ cores === 1 ? 'Kern' : 'Kerne' }}</p>
        <ul v-if="stats" class="stats" data-test="cpu-stats">
          <li><span>Min.</span><strong>{{ pct(stats.min) }}</strong></li>
          <li><span>Ø</span><strong>{{ pct(stats.avg) }}</strong></li>
          <li><span>Max.</span><strong>{{ pct(stats.max) }}</strong></li>
        </ul>
      </div>
    </div>

    <UsageHistory :history="history" :from="'#f59e0b'" :to="'#ef4444'" data-test="cpu-history" />

    <ul v-if="loads.length" class="loads" data-test="cpu-load" aria-label="Systemlast">
      <li v-for="l in loads" :key="l.label" :class="l.level">
        <span>{{ l.label }}</span>
        <strong>{{ l.value.toFixed(2) }}</strong>
      </li>
    </ul>
    <p v-if="model" class="soft model">{{ model }}</p>
  </div>
</template>

<style scoped>
.cpu {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.top {
  display: flex;
  align-items: center;
  gap: 22px;
}
.side {
  min-width: 0;
  flex: 1;
}
.big {
  margin: 0;
  font-size: 1.5rem;
  font-weight: 700;
  letter-spacing: -0.02em;
}
.soft {
  color: var(--bn-text-soft);
  margin: 0.2rem 0 0;
  font-size: 0.85rem;
}
.stats {
  list-style: none;
  display: flex;
  gap: 14px;
  padding: 0;
  margin: 10px 0 0;
}
.stats li {
  display: flex;
  flex-direction: column;
  font-size: 0.7rem;
  color: var(--bn-text-soft);
}
.stats strong {
  font-size: 0.9rem;
  color: var(--p-text-color, inherit);
}
.loads {
  list-style: none;
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
  padding: 0;
  margin: 0;
}
.loads li {
  display: flex;
  flex-direction: column;
  padding: 6px 10px;
  border-radius: 10px;
  background: var(--bn-border-soft);
  font-size: 0.7rem;
  color: var(--bn-text-soft);
  border-left: 3px solid var(--bn-ok, #22c55e);
}
.loads li.warn {
  border-left-color: var(--bn-warn, #f59e0b);
}
.loads li.bad {
  border-left-color: var(--bn-danger, #ef4444);
}
.loads strong {
  font-size: 0.95rem;
  color: var(--p-text-color, inherit);
}
.model {
  margin: 0;
  font-size: 0.75rem;
}
</style>
