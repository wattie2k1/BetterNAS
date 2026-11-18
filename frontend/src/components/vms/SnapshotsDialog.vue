<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import { SNAPSHOT_NAME_RE, vmsApi, type VmSnapshot } from '@/api/vms'

const props = defineProps<{ visible: boolean; name: string | null }>()
const emit = defineEmits<{ 'update:visible': [boolean] }>()
const toast = useToast()

const list = ref<VmSnapshot[]>([])
const newName = ref('')
const busy = ref(false)
const loading = ref(false)
const error = ref('')
const confirmRevert = ref<string | null>(null)
const confirmDelete = ref<string | null>(null)
const nameError = computed(() => (newName.value && !SNAPSHOT_NAME_RE.test(newName.value) ? 'Buchstaben, Ziffern, . _ - (höchstens 40 Zeichen).' : ''))

async function load() {
  if (!props.name) return
  loading.value = true
  try {
    list.value = await vmsApi.snapshots(props.name)
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

watch(
  () => [props.visible, props.name] as const,
  ([v]) => {
    if (!v) return
    newName.value = ''
    confirmRevert.value = confirmDelete.value = null
    list.value = []
    void load()
  },
  { immediate: true },
)

async function run(fn: () => Promise<unknown>, ok: string) {
  busy.value = true
  error.value = ''
  try {
    await fn()
    toast.add({ severity: 'success', summary: ok, detail: props.name ?? '', life: 4000 })
    confirmRevert.value = confirmDelete.value = null
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

const create = () => run(async () => {
  await vmsApi.snapshotCreate(props.name!, newName.value)
  newName.value = ''
}, 'Snapshot angelegt')
const revert = (s: string) => run(() => vmsApi.snapshotRevert(props.name!, s), 'VM zurückgesetzt')
const remove = (s: string) => run(() => vmsApi.snapshotDelete(props.name!, s), 'Snapshot gelöscht')
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="`Snapshots: ${name ?? ''}`"
    :style="{ width: 'min(620px, 96vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <form class="add" @submit.prevent="create">
      <InputText v-model="newName" placeholder="Name des neuen Snapshots" aria-label="Snapshot-Name" :invalid="!!nameError" autocomplete="off" data-test="snap-name" />
      <Button type="submit" label="Snapshot anlegen" icon="pi pi-camera" :disabled="busy || !SNAPSHOT_NAME_RE.test(newName)" data-test="snap-create" />
    </form>
    <small v-if="nameError" class="err">{{ nameError }}</small>
    <Message v-if="error" severity="error" :closable="false" data-test="snap-error">{{ error }}</Message>
    <p v-if="!loading && !list.length && !error" class="empty" data-test="snap-empty">Noch keine Snapshots.</p>
    <ul v-else class="list">
      <li v-for="s in list" :key="s.name" :data-test="`snap-${s.name}`">
        <div class="meta">
          <strong>{{ s.name }}</strong>
          <small>{{ s.created }} · {{ s.state }}</small>
        </div>
        <template v-if="confirmRevert === s.name">
          <small>VM auf diesen Stand zurücksetzen? Änderungen seitdem gehen verloren.</small>
          <Button label="Zurücksetzen" size="small" severity="danger" :disabled="busy" data-test="snap-revert-ok" @click="revert(s.name)" />
          <Button label="Abbrechen" size="small" text @click="confirmRevert = null" />
        </template>
        <template v-else-if="confirmDelete === s.name">
          <small>Snapshot löschen?</small>
          <Button label="Löschen" size="small" severity="danger" :disabled="busy" data-test="snap-delete-ok" @click="remove(s.name)" />
          <Button label="Abbrechen" size="small" text @click="confirmDelete = null" />
        </template>
        <template v-else>
          <Button icon="pi pi-history" text rounded aria-label="Zurücksetzen" title="Zurücksetzen" :disabled="busy" data-test="snap-revert" @click="confirmRevert = s.name" />
          <Button icon="pi pi-trash" text rounded aria-label="Löschen" title="Löschen" :disabled="busy" data-test="snap-delete" @click="confirmDelete = s.name" />
        </template>
      </li>
    </ul>
  </Dialog>
</template>

<style scoped>
.add {
  display: flex;
  gap: 0.5rem;
  margin-bottom: 0.75rem;
}
.add input {
  flex: 1;
}
.err {
  color: var(--bn-danger);
}
.empty {
  color: var(--bn-text-soft);
}
.list {
  list-style: none;
  margin: 0.5rem 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.list li {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.4rem 0;
}
.meta {
  flex: 1;
  display: flex;
  flex-direction: column;
}
.meta small {
  color: var(--bn-text-soft);
}
</style>
