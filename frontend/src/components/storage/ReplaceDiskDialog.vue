<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import Message from 'primevue/message'
import type { DiskView, PoolView } from '@/api/storage'
import { formatBytes } from '@/utils/format'

const props = defineProps<{ visible: boolean; pool: PoolView | null; disks: DiskView[]; busy?: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; submit: [{ oldDisk: string; newDisk: string }] }>()

const step = ref(1)
const oldId = ref('')
const newId = ref('')
const confirmed = ref(false)

const byId = (id: string) => props.disks.find((d) => d.id === id)
const label = (d: DiskView) => `${d.model || d.id} · ${d.serial || d.id} · ${formatBytes(d.sizeBytes)}${d.state === 'missing' ? ' – fehlt' : ''}`

const members = computed(() => (props.pool?.branches ?? []).map((id) => byId(id)).filter((d): d is DiskView => !!d))
const oldDisk = computed(() => byId(oldId.value))
const newDisk = computed(() => byId(newId.value))
/** Ersatz: formatierte Datenplatten ohne Pool, angeschlossen, mindestens so groß wie die alte. */
const candidates = computed(() =>
  props.disks.filter((d) => d.role === 'data' && !d.pool && d.state === 'online' && d.id !== oldId.value && d.sizeBytes >= (oldDisk.value?.sizeBytes ?? 0)),
)

watch(
  () => [props.visible, props.pool?.name],
  () => {
    step.value = 1
    newId.value = ''
    confirmed.value = false
    oldId.value = members.value.find((d) => d.state === 'missing')?.id ?? ''
  },
  { immediate: true },
)

const canNext = computed(() => (step.value === 1 ? !!oldId.value : step.value === 2 ? !!newId.value : false))

function submit() {
  emit('submit', { oldDisk: oldId.value, newDisk: newId.value })
}
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    header="Platte ersetzen"
    :style="{ width: 'min(560px, 95vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <p class="step">Schritt {{ step }} von 3 · Pool {{ pool?.name }}</p>

    <template v-if="step === 1">
      <p>Welche Datenplatte soll ersetzt werden?</p>
      <label v-for="d in members" :key="d.id" class="opt" data-test="old-option" :data-id="d.id">
        <input v-model="oldId" type="radio" name="old-disk" :value="d.id" />
        <span>{{ label(d) }}</span>
      </label>
    </template>

    <template v-else-if="step === 2">
      <p>Welche Platte tritt an ihre Stelle?</p>
      <Message v-if="!candidates.length" severity="warn" :closable="false" data-test="no-candidates">
        Keine passende Platte gefunden. Schließe eine neue Platte an und formatiere sie unter „Datenträger“ als Datenplatte
        (mindestens {{ formatBytes(oldDisk?.sizeBytes ?? 0) }} groß, noch in keinem Pool).
      </Message>
      <label v-for="d in candidates" :key="d.id" class="opt" data-test="new-option" :data-id="d.id">
        <input v-model="newId" type="radio" name="new-disk" :value="d.id" />
        <span>{{ label(d) }}</span>
      </label>
    </template>

    <template v-else>
      <p>
        <strong>{{ oldDisk ? label(oldDisk) : oldId }}</strong> wird durch <strong>{{ newDisk ? label(newDisk) : newId }}</strong> ersetzt.
      </p>
      <Message severity="warn" :closable="false">
        Die Dateien der alten Platte werden aus der Parität wiederhergestellt. Das kann viele Stunden dauern. Synchronisierung und
        Prüfung des Pools sind währenddessen gesperrt. Die neue Platte darf keine wichtigen Daten enthalten – sie wird Teil des Pools.
      </Message>
      <label class="opt confirm">
        <input id="replace-confirm" v-model="confirmed" type="checkbox" />
        <span>Ich habe verstanden und möchte fortfahren.</span>
      </label>
    </template>

    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button v-if="step > 1" label="Zurück" text :disabled="busy" @click="step--" />
      <Button v-if="step < 3" data-test="replace-next" label="Weiter" :disabled="!canNext" @click="step++" />
      <Button v-else data-test="replace-submit" label="Platte ersetzen" severity="danger" :disabled="!confirmed || busy" :loading="busy" @click="submit" />
    </template>
  </Dialog>
</template>

<style scoped>
.step {
  color: var(--bn-text-soft);
  font-size: 0.85rem;
}
.opt {
  display: flex;
  gap: 0.6rem;
  align-items: center;
  padding: 0.45rem 0.2rem;
  cursor: pointer;
}
.confirm {
  margin-top: 0.8rem;
}
</style>
