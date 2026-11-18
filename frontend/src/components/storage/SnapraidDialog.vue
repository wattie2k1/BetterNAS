<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Checkbox from 'primevue/checkbox'
import Message from 'primevue/message'
import { SNAPRAID_DEFAULTS, type DiskView, type PoolView, type SnapraidInput } from '@/api/storage'
import { formatBytes } from '@/utils/format'

const props = defineProps<{ visible: boolean; pool: PoolView | null; disks: DiskView[]; busy?: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; submit: [SnapraidInput] }>()

const parity = ref<string[]>([])
const sync = ref(SNAPRAID_DEFAULTS.syncSchedule)
const scrub = ref(SNAPRAID_DEFAULTS.scrubSchedule)
const percent = ref(SNAPRAID_DEFAULTS.scrubPercent)
const days = ref(SNAPRAID_DEFAULTS.scrubOlderDays)
const threshold = ref(SNAPRAID_DEFAULTS.deleteThreshold)

const editing = computed(() => !!props.pool?.snapraid)

function reset() {
  const s = props.pool?.snapraid
  parity.value = [...(s?.parity ?? [])]
  sync.value = s?.syncSchedule ?? SNAPRAID_DEFAULTS.syncSchedule
  scrub.value = s?.scrubSchedule ?? SNAPRAID_DEFAULTS.scrubSchedule
  percent.value = s?.scrubPercent ?? SNAPRAID_DEFAULTS.scrubPercent
  days.value = s?.scrubOlderDays ?? SNAPRAID_DEFAULTS.scrubOlderDays
  threshold.value = s?.deleteThreshold ?? SNAPRAID_DEFAULTS.deleteThreshold
}
watch(() => [props.visible, props.pool], reset, { immediate: true })

/** Wählbar: eingehängte Paritätsplatten ohne anderen Pool. */
const options = computed(() =>
  props.disks.filter((d) => d.role === 'parity' && d.state === 'online' && (!d.pool || d.pool === props.pool?.name)),
)
const largestData = computed(() =>
  Math.max(0, ...props.disks.filter((d) => props.pool?.branches.includes(d.id)).map((d) => d.sizeBytes)),
)
const tooSmall = computed(() => options.value.filter((d) => parity.value.includes(d.id) && d.sizeBytes < largestData.value))
const locked = (id: string) => parity.value.length >= 2 && !parity.value.includes(id)

const inRange = (v: number, lo: number, hi: number) => Number.isInteger(v) && v >= lo && v <= hi
const valid = computed(
  () =>
    parity.value.length >= 1 &&
    parity.value.length <= 2 &&
    !tooSmall.value.length &&
    sync.value.trim() !== '' &&
    scrub.value.trim() !== '' &&
    inRange(percent.value, 1, 100) &&
    inRange(days.value, 0, 3650) &&
    inRange(threshold.value, 0, 1000000),
)

function submit() {
  if (!valid.value) return
  const ordered = options.value.map((d) => d.id).filter((id) => parity.value.includes(id))
  emit('submit', {
    parity: ordered,
    syncSchedule: sync.value.trim(),
    scrubSchedule: scrub.value.trim(),
    scrubPercent: percent.value,
    scrubOlderDays: days.value,
    deleteThreshold: threshold.value,
  })
}
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="editing ? `SnapRAID für „${pool?.name}“ ändern` : `SnapRAID für „${pool?.name}“ einrichten`"
    :style="{ width: 'min(620px, 95vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <div class="form">
      <span class="label">Paritätsplatten (1–2)</span>
      <p v-if="!options.length" class="hint">
        Keine freie Paritätsplatte. Unter „Datenträger“ eine Platte formatieren und als „Paritätsplatte“ verwenden.
      </p>
      <div v-for="d in options" :key="d.id" class="branch" data-test="parity-option" :data-id="d.id">
        <Checkbox v-model="parity" :value="d.id" :input-id="`p-${d.id}`" :disabled="locked(d.id)" />
        <label :for="`p-${d.id}`">{{ d.model || d.id }} · {{ d.device }} · {{ formatBytes(d.sizeBytes) }}</label>
      </div>
      <Message v-if="tooSmall.length" severity="error" :closable="false" data-test="parity-warning">
        Jede Paritätsplatte muss mindestens so groß sein wie die größte Datenplatte des Pools
        ({{ formatBytes(largestData) }}).
      </Message>

      <label for="sr-sync">Synchronisierung (systemd-Zeitplan)</label>
      <InputText id="sr-sync" v-model="sync" fluid />
      <small class="hint">Standard: täglich um 03:00 Uhr. Sichert neue und geänderte Dateien in der Parität.</small>

      <label for="sr-scrub">Prüfung (systemd-Zeitplan)</label>
      <InputText id="sr-scrub" v-model="scrub" fluid />
      <small class="hint">Standard: sonntags um 05:00 Uhr. Liest Daten und Parität und findet stille Fehler.</small>

      <div class="row">
        <div>
          <label for="sr-percent">Prüfumfang (%)</label>
          <InputText id="sr-percent" :model-value="String(percent)" type="number" fluid @update:model-value="(v) => (percent = Number(v))" />
        </div>
        <div>
          <label for="sr-days">Nur Daten älter als (Tage)</label>
          <InputText id="sr-days" :model-value="String(days)" type="number" fluid @update:model-value="(v) => (days = Number(v))" />
        </div>
      </div>

      <label for="sr-threshold">Schutz vor Massenlöschung (Dateien)</label>
      <InputText id="sr-threshold" :model-value="String(threshold)" type="number" fluid @update:model-value="(v) => (threshold = Number(v))" />
      <small class="hint">
        Sind seit der letzten Synchronisierung mehr Dateien gelöscht worden, hält die Synchronisierung an und fragt nach.
      </small>
    </div>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button
        data-test="snapraid-submit"
        :label="editing ? 'Speichern' : 'Einrichten'"
        :disabled="!valid || busy"
        :loading="busy"
        @click="submit"
      />
    </template>
  </Dialog>
</template>

<style scoped>
.form {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.form > label,
.label,
.row label {
  font-weight: 600;
  margin-top: 0.8rem;
  display: block;
}
.row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 1rem;
}
.branch {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
.hint {
  color: var(--bn-text-soft);
}
</style>
