<script setup lang="ts">
import { ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import Select from 'primevue/select'
import { SCRUB_INTERVALS, type BtrfsScrubSchedule } from '@/api/storage'

const props = defineProps<{ visible: boolean; schedule: BtrfsScrubSchedule; busy?: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; submit: [BtrfsScrubSchedule] }>()
const interval = ref<BtrfsScrubSchedule['interval']>('monthly')
const hour = ref(3)
watch(
  () => [props.visible, props.schedule],
  () => {
    interval.value = props.schedule.interval
    hour.value = props.schedule.hour
  },
  { immediate: true },
)
const HOURS = Array.from({ length: 24 }, (_, h) => ({ v: h, l: `${String(h).padStart(2, '0')}:00 Uhr` }))
</script>

<template>
  <Dialog :visible="visible" modal append-to="self" header="Prüfung der btrfs-Pools" :style="{ width: 'min(480px, 95vw)' }" @update:visible="emit('update:visible', $event)">
    <div class="form" data-test="scrub-schedule">
      <p class="hint">
        Bei einer Prüfung liest btrfs alle Daten und vergleicht die Prüfsummen. Gefundene Fehler repariert es aus der zweiten Kopie (RAID1 und mehr).
        Eine regelmäßige Prüfung entdeckt schleichende Plattenfehler früh. Sie gilt für alle btrfs-Pools.
      </p>
      <label for="sc-int">Wie oft</label>
      <Select v-model="interval" input-id="sc-int" :options="[...SCRUB_INTERVALS]" option-label="label" option-value="value" append-to="self" fluid />
      <template v-if="interval !== 'off'">
        <label for="sc-hour">Uhrzeit</label>
        <Select v-model="hour" input-id="sc-hour" :options="HOURS" option-label="l" option-value="v" append-to="self" fluid />
      </template>
    </div>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button data-test="scrub-schedule-save" label="Speichern" :loading="busy" :disabled="busy" @click="emit('submit', { interval, hour })" />
    </template>
  </Dialog>
</template>

<style scoped>
.form {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.form > label {
  font-weight: 600;
  margin-top: 0.6rem;
}
.hint {
  color: var(--bn-text-soft);
  margin: 0;
}
</style>
