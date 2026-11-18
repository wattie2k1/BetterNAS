<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Button from 'primevue/button'
import Checkbox from 'primevue/checkbox'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import { VM_NAME_RE, vmsApi } from '@/api/vms'
import SnapshotConfirm from './SnapshotConfirm.vue'
import { useSnapshotGuard } from './useSnapshotGuard'

const props = defineProps<{ vm: string }>()
const open = defineModel<boolean>('visible', { required: true })
const emit = defineEmits<{ renamed: [newName: string] }>()
const toast = useToast()

const newName = ref('')
const renameFiles = ref(true)
const busy = ref(false)
const guard = useSnapshotGuard(() => props.vm, open)

const nameError = computed(() => {
  if (!newName.value) return ''
  if (!VM_NAME_RE.test(newName.value)) return 'Nur Kleinbuchstaben, Ziffern, - und _ (höchstens 40 Zeichen).'
  if (newName.value === props.vm) return 'Der neue Name ist gleich dem alten.'
  return ''
})
const valid = computed(() => VM_NAME_RE.test(newName.value) && newName.value !== props.vm)
// Snapshots stören nur, wenn die Dateien mit umbenannt werden.
const snapshotsOk = computed(() => !renameFiles.value || guard.ok.value)
const canSubmit = computed(() => valid.value && snapshotsOk.value && !busy.value)

watch(open, (v) => {
  if (!v) return
  newName.value = props.vm
  renameFiles.value = true
})

async function submit() {
  if (!canSubmit.value) return
  busy.value = true
  try {
    await vmsApi.rename(props.vm, { newName: newName.value, renameFiles: renameFiles.value, deleteSnapshots: renameFiles.value && guard.deleteSnapshots.value })
    toast.add({ severity: 'success', summary: 'VM umbenannt', detail: `${props.vm} → ${newName.value}`, life: 6000 })
    open.value = false
    emit('renamed', newName.value)
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Umbenennen fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Dialog v-model:visible="open" modal header="VM umbenennen" :style="{ width: '32rem' }" :closable="!busy">
    <div class="body">
      <div class="field">
        <label for="rn-name">Neuer Name</label>
        <InputText v-model="newName" input-id="rn-name" :invalid="!!nameError" autocomplete="off" data-test="rn-name" @keyup.enter="submit" />
        <small class="hint" :class="{ bad: nameError }" data-test="rn-hint">{{ nameError || 'Kleinbuchstaben, Ziffern, - und _ (höchstens 40 Zeichen).' }}</small>
      </div>
      <label class="row">
        <Checkbox v-model="renameFiles" binary input-id="rn-files" data-test="rn-files" />
        <span>Festplatten-Dateien und UEFI-Variablen mit umbenennen (empfohlen)</span>
      </label>
      <small class="hint">Sonst behalten die Dateien den alten Namen. Das funktioniert, kann aber später mit einer neuen VM gleichen Namens kollidieren. Nur von BetterNAS angelegte Dateien werden umbenannt, importierte Images bleiben unverändert.</small>
      <SnapshotConfirm v-if="renameFiles" v-model="guard.confirmDelete.value" :snapshots="guard.snapshots.value" prefix="rn" action="umbenennen" />
      <Message severity="info" :closable="false">Die VM muss dafür ausgeschaltet sein. Autostart und Einstellungen bleiben erhalten.</Message>
    </div>
    <template #footer>
      <Button label="Abbrechen" text :disabled="busy" @click="open = false" />
      <Button label="Umbenennen" icon="pi pi-pencil" :loading="busy" :disabled="!canSubmit" data-test="rn-submit" @click="submit" />
    </template>
  </Dialog>
</template>

<style scoped>
.body {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}
.row {
  display: flex;
  align-items: center;
  gap: 0.6rem;
}
.hint {
  color: var(--bn-text-soft);
  font-size: 0.8rem;
}
.hint.bad {
  color: var(--bn-danger);
}
</style>
