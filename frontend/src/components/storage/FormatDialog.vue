<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import RadioButton from 'primevue/radiobutton'
import Message from 'primevue/message'
import type { FormatPreview, FormatRequest } from '@/api/storage'
import { formatBytes } from '@/utils/format'

const props = defineProps<{ visible: boolean; preview: FormatPreview | null; busy?: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; submit: [FormatRequest] }>()

const fsType = ref<'ext4' | 'xfs'>('ext4')
const role = ref<'data' | 'parity'>('data')
const confirm = ref('')
watch(
  () => props.preview,
  () => {
    confirm.value = ''
    fsType.value = 'ext4'
    role.value = 'data'
  },
)

const expected = computed(() => props.preview?.serial || props.preview?.diskId || '')
const ok = computed(() => !!props.preview && confirm.value === expected.value)

function submit() {
  if (!ok.value || !props.preview) return
  emit('submit', { fsType: fsType.value, role: role.value, confirmSerial: confirm.value, confirmToken: props.preview.confirmToken })
}
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    header="Datenträger formatieren"
    :style="{ width: 'min(560px, 95vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <template v-if="preview">
      <p>
        <strong>{{ preview.model || 'Unbekanntes Modell' }}</strong> · {{ preview.device }} ·
        {{ formatBytes(preview.sizeBytes) }}
      </p>
      <Message v-if="preview.partitions.length" severity="error" :closable="false">
        Diese Daten werden unwiderruflich gelöscht:
        <ul>
          <li v-for="p in preview.partitions" :key="p.device">
            {{ p.device }} – {{ p.fsType || 'unbekannt' }}{{ p.label ? ` „${p.label}“` : '' }} ({{ formatBytes(p.sizeBytes) }})
          </li>
        </ul>
      </Message>
      <Message v-else severity="warn" :closable="false">Alle Daten auf diesem Datenträger werden gelöscht.</Message>

      <Message v-if="preview.transport === 'usb'" severity="warn" :closable="false" data-test="usb-warning">
        Diese Platte hängt per USB. USB-Platten fallen bei Wackelkontakt, Stromsparen oder Abziehen kurz aus und können dabei Daten beschädigen;
        für den Dauerbetrieb, besonders als Paritätsplatte, sind SATA-Anschlüsse besser geeignet.
      </Message>

      <fieldset class="fs">
        <legend>Dateisystem</legend>
        <label><RadioButton v-model="fsType" value="ext4" input-id="fs-ext4" /> ext4 (empfohlen)</label>
        <label><RadioButton v-model="fsType" value="xfs" input-id="fs-xfs" /> XFS (sehr große Dateien)</label>
      </fieldset>

      <fieldset class="fs">
        <legend>Verwendung</legend>
        <label><RadioButton v-model="role" value="data" input-id="role-data" /> Datenplatte</label>
        <label><RadioButton v-model="role" value="parity" input-id="role-parity" /> Paritätsplatte (SnapRAID)</label>
      </fieldset>

      <label for="confirm-serial" class="confirm-label">
        Zur Bestätigung die Seriennummer eingeben: <code>{{ expected }}</code>
      </label>
      <InputText id="confirm-serial" v-model="confirm" autocomplete="off" fluid />
    </template>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button
        data-test="format-submit"
        label="Formatieren"
        severity="danger"
        :disabled="!ok || busy"
        :loading="busy"
        @click="submit"
      />
    </template>
  </Dialog>
</template>

<style scoped>
.fs {
  border: none;
  padding: 0;
  margin: 1rem 0;
  display: flex;
  gap: 1.5rem;
  flex-wrap: wrap;
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
