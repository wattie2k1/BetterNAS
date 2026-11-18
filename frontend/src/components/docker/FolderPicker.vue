<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import Message from 'primevue/message'
import { dockerApi, type FolderList } from '@/api/docker'

const props = defineProps<{ visible: boolean; /** Pfad, bei dem die Auswahl beginnt (z. B. der bisherige Wert). */ start?: string }>()
const emit = defineEmits<{ 'update:visible': [boolean]; select: [path: string] }>()

const list = ref<FolderList | null>(null)
const loading = ref(false)
const error = ref('')

const ROOT_LABEL: Record<string, string> = { pools: 'Speicherpools', appdata: 'App-Daten' }

async function load(path: string) {
  loading.value = true
  error.value = ''
  try {
    list.value = await dockerApi.folders(path)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

watch(
  () => props.visible,
  async (v) => {
    if (!v) return
    list.value = null
    const s = (props.start ?? '').replace(/\/+$/, '')
    const inRoots = s === '/srv/pools' || s.startsWith('/srv/pools/') || s === '/srv/appdata' || s.startsWith('/srv/appdata/')
    await load(inRoots ? s : '')
    // Existiert der bisherige Ordner nicht (mehr), mit der Übersicht beginnen.
    if (error.value && inRoots) await load('')
  },
  { immediate: true },
)

const current = computed(() => list.value?.path ?? '')
const label = (name: string, path: string) => (current.value === '' ? `${ROOT_LABEL[name] ?? name} (${path})` : name)
const choose = () => {
  emit('select', current.value)
  emit('update:visible', false)
}
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    header="Ordner wählen"
    :style="{ width: 'min(520px, 96vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <div class="bar">
      <Button
        data-test="folder-up"
        icon="pi pi-arrow-up"
        text
        rounded
        aria-label="Eine Ebene höher"
        title="Eine Ebene höher"
        :disabled="loading || !list || current === ''"
        @click="load(list?.parent ?? '')"
      />
      <code data-test="folder-current">{{ current || 'Bereich wählen' }}</code>
    </div>
    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
    <ul v-else class="dirs" data-test="folder-list">
      <li v-for="d in list?.dirs ?? []" :key="d.path">
        <button type="button" :data-test="`folder-${d.name}`" @click="load(d.path)">
          <i class="pi pi-folder" /> {{ label(d.name, d.path) }}
        </button>
      </li>
      <li v-if="list && !list.dirs.length" class="empty">Keine Unterordner.</li>
    </ul>
    <small class="hint">Es werden vorhandene Ordner der Speicherpools und der App-Daten angezeigt.</small>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button data-test="folder-select" label="Diesen Ordner wählen" icon="pi pi-check" :disabled="loading || current === ''" @click="choose" />
    </template>
  </Dialog>
</template>

<style scoped>
.bar {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
}
.bar code {
  overflow-wrap: anywhere;
}
.dirs {
  list-style: none;
  margin: 0 0 0.5rem;
  padding: 0;
  max-height: 320px;
  overflow: auto;
  border: 1px solid var(--p-content-border-color);
  border-radius: var(--bn-radius-xs);
}
.dirs button {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  width: 100%;
  padding: 0.55rem 0.75rem;
  text-align: left;
  color: inherit;
  font: inherit;
  cursor: pointer;
  background: transparent;
  border: 0;
}
.dirs button:hover,
.dirs button:focus-visible {
  background: var(--p-content-hover-background);
}
.empty {
  padding: 0.75rem;
  color: var(--bn-text-soft);
}
.hint {
  color: var(--bn-text-soft);
}
</style>
