<script setup lang="ts">
import { computed } from 'vue'
import RingGauge from '@/components/RingGauge.vue'
import UsageHistory from '@/components/UsageHistory.vue'
import { formatBytes } from '@/utils/format'

/** Inhalt der Arbeitsspeicher-Kachel: Belegung, Verlauf der letzten Minuten und Zahlen in Byte. */
const props = defineProps<{
  totalBytes: number
  availableBytes: number
  /** Belegung in Prozent aus den letzten Messungen, älteste zuerst. */
  history: number[]
}>()

const used = computed(() => Math.max(0, props.totalBytes - props.availableBytes))
const percent = computed(() => (props.totalBytes ? (used.value / props.totalBytes) * 100 : 0))
const stats = computed(() => {
  const h = props.history
  if (!h.length) return null
  return { min: Math.min(...h), max: Math.max(...h), avg: h.reduce((a, b) => a + b, 0) / h.length }
})
const pct = (v: number) => `${Math.round(v)} %`
const level = computed(() => (percent.value >= 90 ? 'bad' : percent.value >= 75 ? 'warn' : 'ok'))
</script>

<template>
  <div class="mem-tile" data-test="mem-tile">
    <div class="top">
      <RingGauge :value="percent" label="belegt" :size="116" from="#e879f9" to="#6366f1" />
      <div class="side">
        <p class="big" data-test="mem-used">{{ formatBytes(used) }}</p>
        <p class="soft">von {{ formatBytes(totalBytes) }}</p>
        <ul v-if="stats" class="stats" data-test="mem-stats">
          <li><span>Min.</span><strong>{{ pct(stats.min) }}</strong></li>
          <li><span>Ø</span><strong>{{ pct(stats.avg) }}</strong></li>
          <li><span>Max.</span><strong>{{ pct(stats.max) }}</strong></li>
        </ul>
      </div>
    </div>

    <UsageHistory :history="history" from="#e879f9" to="#6366f1" data-test="mem-history" />

    <ul class="facts" aria-label="Arbeitsspeicher in Zahlen">
      <li :class="level"><span>Belegt</span><strong>{{ formatBytes(used) }}</strong></li>
      <li class="ok"><span>Verfügbar</span><strong data-test="mem-available">{{ formatBytes(availableBytes) }}</strong></li>
      <li class="plain"><span>Gesamt</span><strong>{{ formatBytes(totalBytes) }}</strong></li>
    </ul>
  </div>
</template>

<style scoped>
.mem-tile {
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
.facts {
  list-style: none;
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
  padding: 0;
  margin: 0;
}
.facts li {
  display: flex;
  flex-direction: column;
  padding: 6px 10px;
  border-radius: 10px;
  background: var(--bn-border-soft);
  font-size: 0.7rem;
  color: var(--bn-text-soft);
  border-left: 3px solid var(--bn-ok);
}
.facts li.warn {
  border-left-color: var(--bn-warn);
}
.facts li.bad {
  border-left-color: var(--bn-danger);
}
.facts li.plain {
  border-left-color: var(--bn-accent-1);
}
.facts strong {
  font-size: 0.95rem;
  color: var(--p-text-color, inherit);
}
</style>
