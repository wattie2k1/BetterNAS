<script setup lang="ts">
import { ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import Message from 'primevue/message'
import ProgressBar from 'primevue/progressbar'
import { useToast } from 'primevue/usetoast'
import { ISO_FILE_RE, MAX_UPLOAD_BYTES, vmsApi } from '@/api/vms'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; changed: [] }>()
const toast = useToast()

const files = ref<string[]>([])
const error = ref('')
const progress = ref<number | null>(null)
const confirmDelete = ref<string | null>(null)
const input = ref<HTMLInputElement>()

async function load() {
  try {
    files.value = await vmsApi.isos()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

watch(
  () => props.visible,
  (v) => {
    if (!v) return
    error.value = ''
    progress.value = null
    confirmDelete.value = null
    void load()
  },
  { immediate: true },
)

async function upload(file: File) {
  error.value = ''
  if (!ISO_FILE_RE.test(file.name)) {
    error.value = 'Ungültiger Dateiname (erlaubt: .iso oder .qcow2, nur Buchstaben, Ziffern, . _ -, ohne Leerzeichen).'
    return
  }
  if (file.size > MAX_UPLOAD_BYTES) {
    error.value = 'Die Datei ist größer als 50 GiB.'
    return
  }
  progress.value = 0
  try {
    await vmsApi.isoUpload(file, (done, total) => (progress.value = Math.round((done / total) * 100)))
    toast.add({ severity: 'success', summary: 'Datei hochgeladen', detail: file.name, life: 4000 })
    emit('changed')
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    progress.value = null
  }
}

function onPick(e: Event) {
  const el = e.target as HTMLInputElement
  const f = el.files?.[0]
  el.value = ''
  if (f) void upload(f)
}

async function remove(file: string) {
  error.value = ''
  try {
    await vmsApi.isoDelete(file)
    toast.add({ severity: 'success', summary: 'Datei gelöscht', detail: file, life: 4000 })
    confirmDelete.value = null
    emit('changed')
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
    confirmDelete.value = null
  }
}
</script>

<template>
  <Dialog :visible="visible" modal append-to="self" header="ISO- und Image-Dateien" :style="{ width: 'min(560px, 96vw)' }" @update:visible="emit('update:visible', $event)">
    <p class="hint">Installations-ISOs (.iso) und fertige Festplatten-Images (.qcow2) liegen unter /srv/vms/iso.</p>
    <input ref="input" type="file" accept=".iso,.qcow2" hidden data-test="iso-file" @change="onPick" />
    <Button label="Datei hochladen" icon="pi pi-upload" :disabled="progress !== null" data-test="iso-upload" @click="input?.click()" />
    <ProgressBar v-if="progress !== null" :value="progress" class="bar" data-test="iso-progress" />
    <Message v-if="error" severity="error" :closable="false" data-test="iso-error">{{ error }}</Message>
    <p v-if="!files.length" class="hint" data-test="iso-empty">Noch keine Dateien.</p>
    <ul v-else class="list">
      <li v-for="f in files" :key="f" :data-test="`iso-${f}`">
        <span class="name">{{ f }}</span>
        <template v-if="confirmDelete === f">
          <Button label="Löschen" size="small" severity="danger" data-test="iso-delete-ok" @click="remove(f)" />
          <Button label="Abbrechen" size="small" text @click="confirmDelete = null" />
        </template>
        <Button v-else icon="pi pi-trash" text rounded :aria-label="`${f} löschen`" title="Löschen" data-test="iso-delete" @click="confirmDelete = f" />
      </li>
    </ul>
  </Dialog>
</template>

<style scoped>
.hint {
  color: var(--bn-text-soft);
  font-size: 0.85rem;
}
.bar {
  margin-top: 0.75rem;
}
.list {
  list-style: none;
  margin: 0.75rem 0 0;
  padding: 0;
}
.list li {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.25rem 0;
}
.name {
  flex: 1;
  word-break: break-all;
}
</style>
