<script setup lang="ts">
import { onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Card from 'primevue/card'
import ProgressBar from 'primevue/progressbar'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import PoolDialog from '@/components/storage/PoolDialog.vue'
import ConfirmDeleteDialog from '@/components/storage/ConfirmDeleteDialog.vue'
import SnapraidDialog from '@/components/storage/SnapraidDialog.vue'
import SnapraidPanel from '@/components/storage/SnapraidPanel.vue'
import { useStorageStore } from '@/stores/storage'
import { POLICIES, type PoolInput, type PoolView, type SnapraidInput } from '@/api/storage'
import { formatBytes } from '@/utils/format'

const store = useStorageStore()
const toast = useToast()
const showDialog = ref(false)
const editing = ref<PoolView | null>(null)
const deleting = ref<PoolView | null>(null)
const busy = ref(false)
const snapPool = ref<PoolView | null>(null)
const disabling = ref<PoolView | null>(null)
const refreshKey = ref(0)

onMounted(() => store.refresh())

const diskLabel = (id: string) => {
  const d = store.disks.find((x) => x.id === id)
  return d ? `${d.model || id} (${formatBytes(d.sizeBytes)})${d.state === 'missing' ? ' – fehlt' : ''}` : id
}
const policyLabel = (v: string) => POLICIES.find((p) => p.value === v)?.label ?? v
const used = (p: PoolView) => (p.sizeBytes ? Math.round(((p.sizeBytes - p.freeBytes) / p.sizeBytes) * 100) : 0)

function openNew() {
  editing.value = null
  showDialog.value = true
}
function openEdit(p: PoolView) {
  editing.value = p
  showDialog.value = true
}

async function run(fn: () => Promise<void>, done: string) {
  busy.value = true
  try {
    await fn()
    toast.add({ severity: 'success', summary: done, life: 4000 })
    showDialog.value = false
    deleting.value = null
    snapPool.value = null
    disabling.value = null
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    refreshKey.value++
    busy.value = false
  }
}

const actionLabel = { sync: 'Synchronisierung abgeschlossen', scrub: 'Prüfung abgeschlossen', diff: 'Vergleich abgeschlossen' }

function runSnapraid(p: PoolView, action: 'sync' | 'scrub' | 'diff', force: boolean) {
  run(() => store.runSnapraid(p.name, action, force), actionLabel[action])
}

function saveSnapraid(input: SnapraidInput) {
  run(() => store.enableSnapraid(snapPool.value!.name, input), 'SnapRAID gespeichert')
}

function save(input: PoolInput) {
  if (editing.value) run(() => store.updatePool(editing.value!.name, input), 'Pool gespeichert')
  else run(() => store.createPool(input), 'Pool angelegt')
}
</script>

<template>
  <div class="toolbar">
    <Button label="Neuer Pool" icon="pi pi-plus" @click="openNew" />
  </div>
  <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>
  <p v-if="!store.pools.length && !store.loading" class="empty">
    Noch keine Speicherpools. Ein Pool fasst mehrere Datenplatten zu einem gemeinsamen Speicherort zusammen.
  </p>
  <div class="grid">
    <Card v-for="p in store.pools" :key="p.name">
      <template #title>
        <div class="title">
          <span><i class="pi pi-box" /> {{ p.name }}</span>
          <Tag :value="p.mounted ? 'Eingehängt' : 'Nicht eingehängt'" :severity="p.mounted ? 'success' : 'danger'" />
        </div>
      </template>
      <template #content>
        <p class="sub">{{ p.path }}</p>
        <template v-if="p.mounted">
          <ProgressBar :value="used(p)" :show-value="false" />
          <p class="sub">{{ formatBytes(p.sizeBytes - p.freeBytes) }} von {{ formatBytes(p.sizeBytes) }} belegt</p>
        </template>
        <ul>
          <li v-for="b in p.branches" :key="b">{{ diskLabel(b) }}</li>
        </ul>
        <p class="sub">Verteilung: {{ policyLabel(p.createPolicy) }} · mind. {{ p.minFree }} frei</p>
        <SnapraidPanel
          :pool="p"
          :refresh-key="refreshKey"
          @configure="snapPool = p"
          @run="(action, force) => runSnapraid(p, action, force)"
          @disable="disabling = p"
        />
      </template>
      <template #footer>
        <div class="actions">
          <Button label="Bearbeiten" icon="pi pi-pencil" text @click="openEdit(p)" />
          <Button label="Löschen" icon="pi pi-trash" text severity="danger" @click="deleting = p" />
        </div>
      </template>
    </Card>
  </div>
  <PoolDialog v-model:visible="showDialog" :disks="store.disks" :pools="store.pools" :pool="editing" :busy="busy" @submit="save" />
  <SnapraidDialog
    :visible="!!snapPool"
    :pool="snapPool"
    :disks="store.disks"
    :busy="busy"
    @update:visible="(v) => !v && (snapPool = null)"
    @submit="saveSnapraid"
  />
  <ConfirmDeleteDialog
    :visible="!!disabling"
    :name="disabling?.name ?? ''"
    header="SnapRAID abschalten"
    :text="`SnapRAID wird für „${disabling?.name}“ abgeschaltet: Zeitpläne entfallen, die Parität wird nicht mehr aktualisiert. Die Daten bleiben erhalten.`"
    confirm-label="Abschalten"
    :busy="busy"
    @update:visible="(v) => !v && (disabling = null)"
    @confirm="run(() => store.disableSnapraid(disabling!.name), 'SnapRAID abgeschaltet')"
  />
  <ConfirmDeleteDialog
    :visible="!!deleting"
    :name="deleting?.name ?? ''"
    :busy="busy"
    @update:visible="(v) => !v && (deleting = null)"
    @confirm="run(() => store.deletePool(deleting!.name), 'Pool gelöscht')"
  />
</template>

<style scoped>
.toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 1rem;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 1rem;
}
.title {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem;
}
.sub,
.empty {
  color: var(--p-text-muted-color);
  font-size: 0.875rem;
}
ul {
  padding-left: 1.2rem;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.25rem;
}
</style>
