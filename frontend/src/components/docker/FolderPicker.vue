<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import { dockerApi, type FolderList } from '@/api/docker'

const props = defineProps<{ visible: boolean; /** Pfad, bei dem die Auswahl beginnt (z. B. der bisherige Wert). */ start?: string }>()
const emit = defineEmits<{ 'update:visible': [boolean]; select: [path: string] }>()

const list = ref<FolderList | null>(null)
const loading = ref(false)
const error = ref('')
const creating = ref(false)
const newName = ref('')
const createError = ref('')
const saving = ref(false)

const ROOT_LABEL: Record<string, string> = { pools: 'Speicherpools', disks: 'Einzelne Platten', appdata: 'App-Daten' }

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
    creating.value = false
    const s = (props.start ?? '').replace(/\/+$/, '')
    const inRoots = s.startsWith('/srv/')
    await load(inRoots ? s : '')
    // Existiert der bisherige Ordner nicht (mehr), mit der Übersicht beginnen.
    if (error.value && inRoots) await load('')
  },
  { immediate: true },
)

const current = computed(() => list.value?.path ?? '')

// --- Neuer Ordner ---
const NAME_RE = /^[A-Za-z0-9][A-Za-z0-9_.+@-]{0,63}$/
/** Direkt in der Pool- oder Platten-Übersicht und in der Bereichs-Übersicht lassen sich keine Ordner anlegen. */
const canCreate = computed(() => current.value !== '' && current.value !== '/srv/pools' && current.value !== '/srv/disks')
const nameProblem = computed(() => {
  const n = newName.value
  if (!n) return ''
  if (n === 'lost+found' || !NAME_RE.test(n)) return 'Erlaubt: Buchstaben ohne Umlaute, Ziffern und _ . + @ - (höchstens 64 Zeichen).'
  if (list.value?.dirs.some((d) => d.name === n)) return 'Diesen Ordner gibt es hier schon.'
  return ''
})

function startCreate() {
  creating.value = true
  newName.value = ''
  createError.value = ''
}

async function create() {
  if (!newName.value || nameProblem.value || saving.value) return
  saving.value = true
  createError.value = ''
  try {
    const f = await dockerApi.createFolder(current.value, newName.value)
    creating.value = false
    await load(f.path) // direkt in den neuen Ordner wechseln
  } catch (e) {
    createError.value = e instanceof Error ? e.message : String(e)
  } finally {
    saving.value = false
  }
}
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
      <Button
        data-test="folder-new"
        icon="pi pi-folder-plus"
        label="Neuer Ordner"
        size="small"
        severity="secondary"
        text
        :disabled="loading || !canCreate"
        :title="canCreate ? 'Legt in diesem Ordner einen neuen Ordner an' : 'Öffne zuerst einen Pool oder eine Platte'"
        @click="startCreate"
      />
    </div>
    <div v-if="creating" class="create" data-test="folder-create">
      <InputText v-model="newName" placeholder="Name des neuen Ordners" autocomplete="off" spellcheck="false" data-test="folder-name" :invalid="!!nameProblem" @keydown.enter.prevent="create" />
      <Button data-test="folder-create-submit" label="Anlegen" size="small" :disabled="!newName || !!nameProblem" :loading="saving" @click="create" />
      <Button label="Abbrechen" size="small" text @click="creating = false" />
      <small v-if="nameProblem" class="err" data-test="folder-name-error">{{ nameProblem }}</small>
      <small v-else-if="createError" class="err" data-test="folder-create-error">{{ createError }}</small>
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
    <small class="hint">Es werden vorhandene Ordner der Speicherpools, der Platten und der Docker-Speicherorte angezeigt.</small>
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
  flex: 1;
  overflow-wrap: anywhere;
}
.create {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
}
.create input {
  flex: 1;
  min-width: 12rem;
}
.err {
  flex-basis: 100%;
  color: var(--bn-danger);
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
