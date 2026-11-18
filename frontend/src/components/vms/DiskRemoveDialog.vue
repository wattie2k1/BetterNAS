<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Button from 'primevue/button'
import Checkbox from 'primevue/checkbox'
import Dialog from 'primevue/dialog'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import { vmsApi, type VmDisk } from '@/api/vms'
import SnapshotConfirm from './SnapshotConfirm.vue'
import { useSnapshotGuard } from './useSnapshotGuard'

const props = defineProps<{ vm: string; disk: VmDisk | null }>()
const open = defineModel<boolean>('visible', { required: true })
const emit = defineEmits<{ removed: [] }>()
const toast = useToast()

const deleteFile = ref(false)
const busy = ref(false)
const guard = useSnapshotGuard(() => props.vm, open)
const canSubmit = computed(() => guard.ok.value && !busy.value)

watch(open, (v) => {
  if (v) deleteFile.value = false
})

async function submit() {
  if (!props.disk || !canSubmit.value) return
  busy.value = true
  try {
    await vmsApi.diskRemove(props.vm, { path: props.disk.path, deleteFile: deleteFile.value, deleteSnapshots: guard.deleteSnapshots.value })
    toast.add({ severity: 'success', summary: deleteFile.value ? 'Festplatte entfernt und gelöscht' : 'Festplatte entfernt', detail: props.disk.path, life: 6000 })
    open.value = false
    emit('removed')
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Entfernen fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Dialog v-model:visible="open" modal header="Festplatte entfernen" :style="{ width: '32rem' }" :closable="!busy">
    <div v-if="disk" class="body">
      <p class="path">{{ disk.path }}</p>
      <Message severity="info" :closable="false">Die Festplatte wird von der VM getrennt. Ihre Datei bleibt erhalten, solange du sie nicht ausdrücklich löschen lässt – so lässt sie sich später wieder verwenden.</Message>
      <label class="row">
        <Checkbox v-model="deleteFile" binary input-id="dx-file" data-test="dx-file" />
        <span>Datei auch endgültig löschen ({{ disk.sizeGiB }} GiB, alle Daten darauf gehen verloren)</span>
      </label>
      <Message v-if="deleteFile" severity="error" :closable="false" data-test="dx-warn">Das Löschen lässt sich nicht rückgängig machen.</Message>
      <SnapshotConfirm v-model="guard.confirmDelete.value" :snapshots="guard.snapshots.value" prefix="dx" action="Festplatte entfernen" />
    </div>
    <template #footer>
      <Button label="Abbrechen" text :disabled="busy" @click="open = false" />
      <Button :label="deleteFile ? 'Entfernen und löschen' : 'Entfernen'" icon="pi pi-trash" severity="danger" :loading="busy" :disabled="!canSubmit" data-test="dx-submit" @click="submit" />
    </template>
  </Dialog>
</template>

<style scoped>
.body {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.path {
  margin: 0;
  font-size: 0.8rem;
  color: var(--bn-text-soft);
  word-break: break-all;
}
.row {
  display: flex;
  align-items: center;
  gap: 0.6rem;
}
</style>
