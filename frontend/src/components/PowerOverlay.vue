<script setup lang="ts">
import { computed } from 'vue'
import ProgressSpinner from 'primevue/progressspinner'
import { usePowerStore } from '@/stores/power'

const power = usePowerStore()
const title = computed(() => ({ rebooting: 'Das NAS startet neu …', shuttingDown: 'Das NAS fährt herunter …', off: 'Das NAS ist ausgeschaltet', idle: '' })[power.phase])
const text = computed(() => {
  if (power.phase === 'off') return 'Zum Einschalten brauchst du den Netzschalter.'
  if (power.phase === 'shuttingDown') return 'Bitte warten, bis das NAS vollständig ausgeschaltet ist.'
  if (power.slow) return 'Das NAS antwortet noch nicht. Das kann bei einem Neustart nach Updates länger dauern; diese Seite wartet weiter und lädt sich selbst neu.'
  return 'Diese Seite lädt sich automatisch neu, sobald das NAS wieder erreichbar ist.'
})
</script>

<template>
  <div v-if="power.phase !== 'idle'" class="overlay" data-test="power-overlay" role="alertdialog" aria-modal="true" aria-live="polite">
    <div class="box glass">
      <i v-if="power.phase === 'off'" class="pi pi-power-off off" />
      <ProgressSpinner v-else style="width: 48px; height: 48px" stroke-width="4" />
      <h2>{{ title }}</h2>
      <p>{{ text }}</p>
    </div>
  </div>
</template>

<style scoped>
.overlay {
  position: fixed;
  inset: 0;
  z-index: 10000;
  display: grid;
  place-items: center;
  background: rgba(8, 12, 20, 0.72);
  backdrop-filter: blur(10px);
}
.box {
  text-align: center;
  padding: 2.2rem 2.6rem;
  max-width: min(480px, 92vw);
  border-radius: 18px;
}
h2 {
  margin: 1rem 0 0.4rem;
  font-size: 1.25rem;
}
p {
  margin: 0;
  color: var(--bn-text-soft);
  font-size: 0.9rem;
}
.off {
  font-size: 2.6rem;
  color: var(--p-red-400, #f87171);
}
</style>
