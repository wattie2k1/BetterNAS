<script setup lang="ts">
import { computed } from 'vue'
import type { PowerDay } from '@/api/storage'

const props = defineProps<{ days: PowerDay[]; range: number }>()

const MAX_BARS = 30
const key = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

interface Bar {
  label: string
  standby: number // Anteil 0–1
  known: boolean
  spinups: number
}

/** Ein Balken je Tag, bei langen Zeiträumen je Abschnitt mehrerer Tage (höchstens 30 Balken). */
const bars = computed<Bar[]>(() => {
  const byDay = new Map(props.days.map((d) => [d.day, d]))
  const step = Math.ceil(props.range / MAX_BARS)
  const today = new Date()
  const start = new Date(today.getFullYear(), today.getMonth(), today.getDate() - (props.range - 1))
  const out: Bar[] = []
  for (let i = 0; i < props.range; i += step) {
    let active = 0
    let standby = 0
    let spin = 0
    let first = ''
    for (let j = i; j < Math.min(i + step, props.range); j++) {
      const k = key(new Date(start.getFullYear(), start.getMonth(), start.getDate() + j))
      first = first || k
      const d = byDay.get(k)
      if (d) {
        active += d.activeMin
        standby += d.standbyMin
        spin += d.spinups
      }
    }
    const total = active + standby
    const [, m, dd] = first.split('-')
    out.push({ label: `${dd}.${m}.${step > 1 ? ' ff.' : ''}`, standby: total ? standby / total : 0, known: total > 0, spinups: spin })
  }
  return out
})

const totals = computed(() => {
  let active = 0
  let standby = 0
  let spin = 0
  for (const d of props.days) {
    active += d.activeMin
    standby += d.standbyMin
    spin += d.spinups
  }
  const total = active + standby
  return { share: total ? Math.round((standby / total) * 100) : 0, known: total > 0, spinups: spin, hours: Math.round(total / 60) }
})
</script>

<template>
  <figure class="standby" data-test="standby-chart">
    <figcaption>
      <span>Standby-Anteil</span>
      <strong v-if="totals.known" data-test="standby-share">{{ totals.share }} %</strong>
    </figcaption>
    <div v-if="totals.known" class="bars" role="img" :aria-label="`${totals.share} % der Zeit im Standby, ${totals.spinups} Anläufe`">
      <div v-for="(b, i) in bars" :key="i" class="col" :title="b.known ? `${b.label}: ${Math.round(b.standby * 100)} % Standby, ${b.spinups} Anläufe` : `${b.label}: keine Messung`">
        <div class="fill" :class="{ unknown: !b.known }" :style="{ height: (b.known ? Math.max(b.standby * 100, 3) : 100) + '%' }" />
      </div>
    </div>
    <p v-if="totals.known" class="sum" data-test="standby-sum">{{ totals.spinups }} Anläufe in {{ totals.hours }} gemessenen Stunden – jeder Anlauf belastet die Mechanik.</p>
    <p v-else class="empty" data-test="standby-empty">Noch keine Messwerte – der Energiezustand wird jede Minute erfasst, ohne die Platte zu wecken.</p>
  </figure>
</template>

<style scoped>
.standby {
  margin: 0;
}
figcaption {
  display: flex;
  justify-content: space-between;
  font-size: 0.85rem;
  color: var(--bn-text-soft);
}
figcaption strong {
  color: var(--bn-accent-1);
}
.bars {
  display: flex;
  gap: 2px;
  height: 4rem;
  margin-top: 0.3rem;
}
.col {
  flex: 1;
  display: flex;
  align-items: flex-end;
  background: var(--bn-border-soft);
  border-radius: 3px;
  overflow: hidden;
}
.fill {
  width: 100%;
  background: linear-gradient(0deg, var(--bn-accent-1), var(--bn-accent-2));
}
.fill.unknown {
  background: transparent;
}
.sum,
.empty {
  font-size: 0.8rem;
  color: var(--bn-text-soft);
  margin: 0.4rem 0 0;
}
</style>
