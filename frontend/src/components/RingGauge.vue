<script setup lang="ts">
import { computed, useId } from 'vue'

const props = defineProps<{ value: number; label?: string; size?: number; from?: string; to?: string }>()
const id = useId()
const size = computed(() => props.size ?? 132)
const stroke = 12
const r = computed(() => (size.value - stroke) / 2)
const c = computed(() => 2 * Math.PI * r.value)
const pct = computed(() => Math.max(0, Math.min(100, Math.round(props.value))))
const dash = computed(() => `${(pct.value / 100) * c.value} ${c.value}`)
</script>

<template>
  <div class="ring" :style="{ width: `${size}px`, height: `${size}px` }" role="img" :aria-label="`${label ?? ''} ${pct} Prozent`">
    <svg :width="size" :height="size" :viewBox="`0 0 ${size} ${size}`">
      <defs>
        <linearGradient :id="`g-${id}`" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" :stop-color="from ?? '#6366f1'" />
          <stop offset="100%" :stop-color="to ?? '#22d3ee'" />
        </linearGradient>
      </defs>
      <circle :cx="size / 2" :cy="size / 2" :r="r" fill="none" class="track" :stroke-width="stroke" />
      <circle
        :cx="size / 2"
        :cy="size / 2"
        :r="r"
        fill="none"
        :stroke="`url(#g-${id})`"
        :stroke-width="stroke"
        stroke-linecap="round"
        :stroke-dasharray="dash"
        :transform="`rotate(-90 ${size / 2} ${size / 2})`"
        class="bar"
      />
    </svg>
    <div class="center">
      <strong>{{ pct }}<small>%</small></strong>
      <span v-if="label">{{ label }}</span>
    </div>
  </div>
</template>

<style scoped>
.ring {
  position: relative;
  flex: 0 0 auto;
}
.track {
  stroke: var(--bn-border-soft);
}
.bar {
  transition: stroke-dasharray 0.6s ease;
}
.center {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}
strong {
  font-size: 1.7rem;
  letter-spacing: -0.03em;
}
small {
  font-size: 0.9rem;
  opacity: 0.7;
}
span {
  font-size: 0.72rem;
  color: var(--bn-text-soft);
}
</style>
