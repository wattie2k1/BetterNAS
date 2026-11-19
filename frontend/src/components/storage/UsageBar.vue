<script setup lang="ts">
import { computed } from 'vue'
import type { DiskUsage } from '@/api/storage'
import { formatBytes } from '@/utils/format'

const props = defineProps<{ usage?: DiskUsage; compact?: boolean }>()

const percent = computed(() => (props.usage && props.usage.sizeBytes > 0 ? Math.min(100, Math.round((props.usage.usedBytes / props.usage.sizeBytes) * 100)) : 0))
/** Ab 85 % warnen, ab 95 % Alarm – eine volle Platte macht Probleme beim Schreiben. */
const level = computed(() => (percent.value >= 95 ? 'danger' : percent.value >= 85 ? 'warn' : 'ok'))
</script>

<template>
  <div v-if="usage" class="usage" :class="[level, { compact }]" data-test="usage-bar">
    <div class="track" role="progressbar" :aria-valuenow="percent" aria-valuemin="0" aria-valuemax="100" :aria-label="`${percent} % belegt`">
      <div class="fill" :style="{ width: percent + '%' }" />
    </div>
    <div class="text">
      <template v-if="compact">{{ percent }} % belegt</template>
      <template v-else>{{ formatBytes(usage.usedBytes) }} von {{ formatBytes(usage.sizeBytes) }} belegt · {{ formatBytes(usage.freeBytes) }} frei</template>
    </div>
  </div>
</template>

<style scoped>
.usage {
  min-width: 9rem;
}
.track {
  height: 8px;
  border-radius: 99px;
  background: var(--bn-border-soft);
  overflow: hidden;
}
.compact .track {
  height: 6px;
}
.fill {
  height: 100%;
  border-radius: 99px;
  background: linear-gradient(90deg, var(--bn-accent-1), var(--bn-accent-2));
  transition: width 0.3s;
}
.warn .fill {
  background: var(--bn-warn);
}
.danger .fill {
  background: var(--bn-danger);
}
.text {
  margin-top: 0.25rem;
  font-size: 0.75rem;
  color: var(--bn-text-soft);
}
.warn .text {
  color: var(--bn-warn);
}
.danger .text {
  color: var(--bn-danger);
}
</style>
