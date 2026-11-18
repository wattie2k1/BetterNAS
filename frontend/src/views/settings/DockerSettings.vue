<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import Textarea from 'primevue/textarea'
import ToggleSwitch from 'primevue/toggleswitch'
import { useToast } from 'primevue/usetoast'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { servicesApi, type DockerSettings } from '@/api/system'

const toast = useToast()
const loaded = ref<DockerSettings | null>(null)
const logMaxSize = ref('10m')
const logMaxFile = ref(3)
const mirrors = ref('')
const liveRestore = ref(false)
const error = ref('')
const busy = ref(false)
const needsRestart = ref(false)
const confirmRestart = ref(false)

const lines = (s: string) => s.split(/\s+/).filter(Boolean)
const dirty = computed(
  () =>
    !!loaded.value &&
    (logMaxSize.value !== loaded.value.logMaxSize || logMaxFile.value !== loaded.value.logMaxFile || lines(mirrors.value).join(' ') !== loaded.value.registryMirrors.join(' ') || liveRestore.value !== loaded.value.liveRestore),
)

function fill(s: DockerSettings) {
  loaded.value = s
  logMaxSize.value = s.logMaxSize
  logMaxFile.value = s.logMaxFile
  mirrors.value = s.registryMirrors.join('\n')
  liveRestore.value = s.liveRestore
}

async function load() {
  try {
    fill(await servicesApi.docker())
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}
onMounted(load)

async function save() {
  busy.value = true
  error.value = ''
  try {
    await servicesApi.setDocker({ logMaxSize: logMaxSize.value.trim(), logMaxFile: logMaxFile.value, registryMirrors: lines(mirrors.value), liveRestore: liveRestore.value })
    needsRestart.value = true
    toast.add({ severity: 'success', summary: 'Docker-Einstellungen gespeichert', life: 4000 })
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function restart() {
  busy.value = true
  try {
    await servicesApi.act('docker', 'restart')
    needsRestart.value = false
    toast.add({ severity: 'success', summary: 'Docker neu gestartet', life: 4000 })
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
    confirmRestart.value = false
  }
}
</script>

<template>
  <div class="card" data-test="settings-docker">
    <h2>Docker</h2>
    <Message v-if="error" severity="error" :closable="false" data-test="docker-error">{{ error }}</Message>
    <form v-if="loaded" class="grid" @submit.prevent="save">
      <label>Daten-Verzeichnis</label>
      <code data-test="docker-dataroot">{{ loaded.dataRoot }}</code>
      <label for="d-size">Maximale Größe je Log-Datei</label>
      <InputText id="d-size" v-model="logMaxSize" placeholder="10m" autocomplete="off" />
      <label for="d-file">Anzahl Log-Dateien je Container</label>
      <InputNumber v-model="logMaxFile" input-id="d-file" :min="1" :max="100" show-buttons />
      <label for="d-mirrors">Registry-Mirrors (einer pro Zeile)</label>
      <Textarea id="d-mirrors" v-model="mirrors" rows="3" placeholder="https://mirror.example.org" spellcheck="false" />
      <label for="d-live">Container bei Docker-Neustart weiterlaufen lassen</label>
      <ToggleSwitch v-model="liveRestore" input-id="d-live" data-test="docker-live" />
      <span />
      <Button type="submit" data-test="docker-save" label="Speichern" icon="pi pi-check" :disabled="!dirty" :loading="busy" class="save" />
    </form>
    <Message v-if="needsRestart" severity="warn" :closable="false" data-test="docker-restart-hint">
      Die Änderungen werden erst nach einem Neustart von Docker wirksam.
      <Button label="Docker jetzt neu starten" size="small" text @click="confirmRestart = true" />
    </Message>
    <ConfirmDialog
      :visible="confirmRestart"
      header="Docker neu starten"
      message="Docker wird neu gestartet. Laufende Container werden dabei beendet und, sofern sie einen Neustart-Modus haben, danach wieder gestartet. Mit „Container weiterlaufen lassen“ bleiben sie ab dem nächsten Mal unberührt."
      confirm-label="Neu starten"
      :busy="busy"
      @update:visible="(v) => !v && (confirmRestart = false)"
      @confirm="restart"
    />
  </div>
</template>

<style scoped>
.card {
  max-width: 760px;
}
.grid {
  display: grid;
  grid-template-columns: minmax(200px, 300px) 1fr;
  gap: 12px 14px;
  align-items: center;
  margin-bottom: 14px;
}
.save {
  justify-self: start;
}
@media (max-width: 600px) {
  .grid {
    grid-template-columns: 1fr;
  }
}
</style>
