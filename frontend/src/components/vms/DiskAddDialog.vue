<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputNumber from 'primevue/inputnumber'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import { vmsApi } from '@/api/vms'
import SnapshotConfirm from './SnapshotConfirm.vue'
import { useSnapshotGuard } from './useSnapshotGuard'

const props = defineProps<{ vm: string }>()
const open = defineModel<boolean>('visible', { required: true })
const emit = defineEmits<{ added: [] }>()
const toast = useToast()

const sizeGiB = ref<number | null>(20)
const busy = ref(false)
const guard = useSnapshotGuard(() => props.vm, open)
const valid = computed(() => sizeGiB.value !== null && Number.isInteger(sizeGiB.value) && sizeGiB.value >= 1 && sizeGiB.value <= 65536)
const canSubmit = computed(() => valid.value && guard.ok.value && !busy.value)

watch(open, (v) => {
  if (v) sizeGiB.value = 20
})

async function submit() {
  if (!canSubmit.value) return
  busy.value = true
  try {
    const r = await vmsApi.diskAdd(props.vm, { sizeGiB: sizeGiB.value!, deleteSnapshots: guard.deleteSnapshots.value })
    toast.add({ severity: 'success', summary: 'Festplatte hinzugefügt', detail: r.path, life: 6000 })
    open.value = false
    emit('added')
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Hinzufügen fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Dialog v-model:visible="open" modal header="Festplatte hinzufügen" :style="{ width: '32rem' }" :closable="!busy">
    <div class="body">
      <div class="field">
        <label for="da-size">Größe (GiB)</label>
        <InputNumber v-model="sizeGiB" input-id="da-size" :min="1" :max="65536" show-buttons data-test="da-size" />
        <small class="hint">Es entsteht eine leere, platzsparende Festplatte (qcow2, belegt nur den tatsächlich genutzten Platz) unter /srv/vms/disks.</small>
      </div>
      <SnapshotConfirm v-model="guard.confirmDelete.value" :snapshots="guard.snapshots.value" prefix="da" action="Festplatte hinzufügen" />
      <Message severity="info" :closable="false">
        Die neue Festplatte ist leer. Im System der VM muss sie noch partitioniert und formatiert werden: unter Linux z. B. mit <code>fdisk</code> und <code>mkfs.ext4</code>, unter Windows in der Datenträgerverwaltung.
      </Message>
    </div>
    <template #footer>
      <Button label="Abbrechen" text :disabled="busy" @click="open = false" />
      <Button label="Hinzufügen" icon="pi pi-plus" :loading="busy" :disabled="!canSubmit" data-test="da-submit" @click="submit" />
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
.hint {
  color: var(--bn-text-soft);
  font-size: 0.8rem;
}
</style>
