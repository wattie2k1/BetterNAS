<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import RadioButton from 'primevue/radiobutton'
import Message from 'primevue/message'
import type { EraseMethod, EraseRequest, FormatPreview } from '@/api/storage'
import { formatBytes } from '@/utils/format'

const props = defineProps<{ visible: boolean; preview: FormatPreview | null; ssd?: boolean; busy?: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; submit: [EraseRequest] }>()

const method = ref<EraseMethod>('zero')
const confirm = ref('')
watch(
  () => props.preview,
  () => {
    confirm.value = ''
    method.value = 'zero'
  },
)
const expected = computed(() => props.preview?.serial || props.preview?.diskId || '')
const ok = computed(() => !!props.preview && confirm.value === expected.value)

function submit() {
  if (!ok.value || !props.preview) return
  emit('submit', { method: method.value, confirmSerial: confirm.value, confirmToken: props.preview.confirmToken })
}
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    header="Datenträger sicher löschen"
    :style="{ width: 'min(560px, 95vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <template v-if="preview">
      <p>
        <strong>{{ preview.model || 'Unbekanntes Modell' }}</strong> · {{ preview.device }} · {{ formatBytes(preview.sizeBytes) }}
      </p>
      <Message severity="error" :closable="false" data-test="erase-warning">
        Alle Daten auf diesem Datenträger werden unwiderruflich vernichtet und lassen sich nicht wiederherstellen.
        <ul v-if="preview.partitions.length">
          <li v-for="p in preview.partitions" :key="p.device">
            {{ p.device }} – {{ p.fsType || 'unbekannt' }}{{ p.label ? ` „${p.label}“` : '' }} ({{ formatBytes(p.sizeBytes) }})
          </li>
        </ul>
      </Message>

      <fieldset class="fs">
        <legend>Methode</legend>
        <label><RadioButton v-model="method" value="zero" input-id="erase-zero" /> Mit Nullen überschreiben (jede Platte; bei großen Platten viele Stunden)</label>
        <label v-if="ssd"><RadioButton v-model="method" value="discard" input-id="erase-discard" /> Alle Blöcke verwerfen / TRIM (SSD; dauert Sekunden bis Minuten)</label>
      </fieldset>

      <label for="erase-serial" class="confirm-label">
        Zur Bestätigung die Seriennummer eingeben: <code>{{ expected }}</code>
      </label>
      <InputText id="erase-serial" v-model="confirm" autocomplete="off" fluid data-test="erase-serial" />
    </template>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button data-test="erase-submit" label="Endgültig löschen" severity="danger" :disabled="!ok || busy" :loading="busy" @click="submit" />
    </template>
  </Dialog>
</template>

<style scoped>
.fs {
  border: none;
  padding: 0;
  margin: 1rem 0;
  display: grid;
  gap: 0.5rem;
}
.fs legend {
  font-weight: 600;
  margin-bottom: 0.5rem;
}
.fs label {
  display: flex;
  gap: 0.4rem;
  align-items: center;
}
.confirm-label {
  display: block;
  margin-bottom: 0.4rem;
}
ul {
  margin: 0.4rem 0 0;
  padding-left: 1.2rem;
}
</style>
