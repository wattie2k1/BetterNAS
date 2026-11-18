<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Button from 'primevue/button'
import Checkbox from 'primevue/checkbox'
import Dialog from 'primevue/dialog'
import InputNumber from 'primevue/inputnumber'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import { vmsApi, type VmDisk, type VmSnapshot } from '@/api/vms'

const props = defineProps<{ vm: string; disk: VmDisk | null }>()
const open = defineModel<boolean>('visible', { required: true })
const emit = defineEmits<{ resized: [] }>()
const toast = useToast()

const sizeGiB = ref<number | null>(null)
const snapshots = ref<VmSnapshot[]>([])
const confirmDelete = ref(false)
const busy = ref(false)

const minGiB = computed(() => (props.disk?.sizeGiB ?? 0) + 1)
const valid = computed(() => sizeGiB.value !== null && Number.isInteger(sizeGiB.value) && sizeGiB.value >= minGiB.value && sizeGiB.value <= 65536)
const needsConfirm = computed(() => snapshots.value.length > 0)
const canSubmit = computed(() => valid.value && (!needsConfirm.value || confirmDelete.value) && !busy.value)

watch(open, async (v) => {
  if (!v || !props.disk) return
  sizeGiB.value = props.disk.sizeGiB + Math.max(1, Math.round(props.disk.sizeGiB / 4))
  confirmDelete.value = false
  snapshots.value = []
  try {
    snapshots.value = await vmsApi.snapshots(props.vm)
  } catch {
    /* Fehlt die Bestätigung, meldet der Agent das beim Speichern ohnehin als Fehler. */
  }
})

async function submit() {
  if (!props.disk || !canSubmit.value) return
  busy.value = true
  try {
    await vmsApi.diskResize(props.vm, { path: props.disk.path, sizeGiB: sizeGiB.value!, deleteSnapshots: needsConfirm.value && confirmDelete.value })
    toast.add({ severity: 'success', summary: 'Festplatte vergrößert', detail: `${props.vm}: ${sizeGiB.value} GiB`, life: 6000 })
    open.value = false
    emit('resized')
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Vergrößern fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Dialog v-model:visible="open" modal header="Festplatte vergrößern" :style="{ width: '32rem' }" :closable="!busy">
    <div v-if="disk" class="body">
      <p class="path">{{ disk.path }}</p>
      <div class="field">
        <label for="dr-size">Neue Größe (GiB) – aktuell {{ disk.sizeGiB }} GiB</label>
        <InputNumber v-model="sizeGiB" input-id="dr-size" :min="minGiB" :max="65536" show-buttons data-test="dr-size" />
      </div>
      <Message v-if="needsConfirm" severity="warn" :closable="false" data-test="dr-snapshots">
        Diese VM hat {{ snapshots.length }} Snapshot(s): {{ snapshots.map((s) => s.name).join(', ') }}. Mit Snapshots lässt sich die Festplatte nicht vergrößern – sie werden dafür endgültig gelöscht.
        <label class="row">
          <Checkbox v-model="confirmDelete" binary input-id="dr-confirm" data-test="dr-confirm" />
          <span>Snapshots löschen und Festplatte vergrößern</span>
        </label>
      </Message>
      <Message severity="info" :closable="false">
        Verkleinern ist nicht möglich. Danach muss das System in der VM den neuen Platz nutzen: unter Linux z. B. mit <code>growpart</code> und <code>resize2fs</code> (bzw. <code>xfs_growfs</code>), unter Windows in der Datenträgerverwaltung („Volume erweitern“).
      </Message>
    </div>
    <template #footer>
      <Button label="Abbrechen" text :disabled="busy" @click="open = false" />
      <Button label="Vergrößern" icon="pi pi-arrows-v" :loading="busy" :disabled="!canSubmit" data-test="dr-submit" @click="submit" />
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
.field {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}
.row {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  margin-top: 0.6rem;
}
</style>
