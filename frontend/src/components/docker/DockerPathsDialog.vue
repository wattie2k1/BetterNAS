<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import FolderPicker from '@/components/docker/FolderPicker.vue'
import { useDockerStore } from '@/stores/docker'
import type { DockerPaths } from '@/api/docker'

const props = defineProps<{ visible: boolean; /** Startwerte; sonst die eingerichteten Speicherorte. */ initial?: { appdata: string; dockerdata: string } }>()
const emit = defineEmits<{ 'update:visible': [boolean]; saved: [DockerPaths] }>()

const store = useDockerStore()
const appdata = ref('')
const dockerdata = ref('')
const busy = ref(false)
const error = ref('')
const result = ref<DockerPaths | null>(null)
const browsing = ref<'appdata' | 'dockerdata' | null>(null)

watch(
  () => props.visible,
  (v) => {
    if (!v) return
    const s = props.initial ?? (store.paths?.configured ? store.paths : null)
    appdata.value = s?.appdata ?? ''
    dockerdata.value = s?.dockerdata ?? ''
    error.value = ''
    result.value = null
  },
  { immediate: true },
)

const configured = computed(() => store.configured && !!store.paths?.configured)
const canSave = computed(() => !!appdata.value.trim() && !!dockerdata.value.trim() && !busy.value)
const staleText = computed(() => {
  const n = result.value?.staleStacks ?? 0
  if (!n) return ''
  return `${n} ${n === 1 ? 'Stack nutzt' : 'Stacks nutzen'} noch Pfade aus einer früheren Einstellung. Vorhandene Daten werden nicht verschoben.`
})

async function save() {
  if (!canSave.value) return
  busy.value = true
  error.value = ''
  try {
    result.value = await store.savePaths({ appdata: appdata.value.trim(), dockerdata: dockerdata.value.trim() })
    emit('saved', result.value)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

function pick(p: string) {
  if (browsing.value === 'appdata') appdata.value = p
  else if (browsing.value === 'dockerdata') dockerdata.value = p
}
</script>

<template>
  <Dialog :visible="visible" modal append-to="self" header="Speicherorte für Docker" :style="{ width: 'min(620px, 96vw)' }" @update:visible="emit('update:visible', $event)">
    <template v-if="!result">
      <p class="intro">
        Wähle zwei Ordner auf einer Platte oder einem Pool (btrfs-Pool oder einzelne Platte; mergerfs-Pools sind für Docker ungeeignet). Lege sie bei Bedarf im Ordnerfenster neu an.
      </p>
      <Message v-if="!configured" severity="info" :closable="false">Bis die Speicherorte festgelegt sind, ist Docker gesperrt. Beim ersten Speichern wird Docker eingerichtet und gestartet.</Message>
      <div class="field">
        <label for="p-app">Einstellungen der Apps (appdata)</label>
        <div class="row">
          <InputText id="p-app" v-model="appdata" data-test="paths-appdata" placeholder="/srv/pools/<Pool>/appdata" autocomplete="off" spellcheck="false" />
          <Button label="Auswählen" severity="secondary" icon="pi pi-folder-open" data-test="paths-browse-appdata" @click="browsing = 'appdata'" />
        </div>
        <small>Klein: Einstellungen der Container sowie Stacks, Reverse-Proxy und Sicherungen von BetterNAS.</small>
      </div>
      <div class="field">
        <label for="p-data">Daten der Apps (dockerdata)</label>
        <div class="row">
          <InputText id="p-data" v-model="dockerdata" data-test="paths-dockerdata" placeholder="/srv/pools/<Pool>/dockerdata" autocomplete="off" spellcheck="false" />
          <Button label="Auswählen" severity="secondary" icon="pi pi-folder-open" data-test="paths-browse-dockerdata" @click="browsing = 'dockerdata'" />
        </div>
        <small>Groß: Daten, Datenbanken, Caches sowie Docker-Images und Container.</small>
      </div>
      <Message v-if="configured" severity="warn" :closable="false">Eine Änderung gilt nur für neue Stacks. Vorhandene Stacks und Daten werden nicht verschoben, und die Images bleiben, wo sie sind.</Message>
      <Message v-if="error" severity="error" :closable="false" data-test="paths-error">{{ error }}</Message>
    </template>
    <template v-else>
      <Message severity="success" :closable="false" data-test="paths-done">Die Speicherorte sind gespeichert.</Message>
      <Message v-for="w in result.warnings" :key="w" severity="warn" :closable="false" data-test="paths-warning">{{ w }}</Message>
      <Message v-if="staleText" severity="info" :closable="false" data-test="paths-stale">{{ staleText }}</Message>
    </template>
    <template #footer>
      <template v-if="!result">
        <Button label="Abbrechen" text @click="emit('update:visible', false)" />
        <Button data-test="paths-save" label="Speichern" icon="pi pi-check" :disabled="!canSave" :loading="busy" @click="save" />
      </template>
      <Button v-else data-test="paths-close" label="Schließen" @click="emit('update:visible', false)" />
    </template>
    <FolderPicker :visible="browsing !== null" :start="browsing === 'appdata' ? appdata : dockerdata" @update:visible="(v) => !v && (browsing = null)" @select="pick" />
  </Dialog>
</template>

<style scoped>
.intro {
  margin-top: 0;
}
.field {
  display: grid;
  gap: 6px;
  margin-bottom: 14px;
}
.row {
  display: flex;
  gap: 8px;
}
.row :deep(input) {
  flex: 1;
}
small {
  color: var(--p-text-muted-color);
}
</style>
