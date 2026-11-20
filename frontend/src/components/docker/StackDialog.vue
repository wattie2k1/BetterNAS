<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import Tabs from 'primevue/tabs'
import TabList from 'primevue/tablist'
import Tab from 'primevue/tab'
import TabPanels from 'primevue/tabpanels'
import TabPanel from 'primevue/tabpanel'
import Textarea from 'primevue/textarea'
import { useDockerStore } from '@/stores/docker'
import FolderPicker from '@/components/docker/FolderPicker.vue'
import DevicePicker from '@/components/docker/DevicePicker.vue'
import StackLimits from '@/components/docker/StackLimits.vue'
import { deviceSnippet } from '@/utils/deviceSnippet'
import { MAX_ICON_BYTES, STACK_NAME_RE, STACK_TEMPLATE, type HostDevice, type IconChange, type StackCheck, type StackFiles } from '@/api/docker'

export type SubmitMode = 'save' | 'start' | 'check'

const YamlEditor = defineAsyncComponent(() => import('./YamlEditor.vue'))

const props = defineProps<{
  visible: boolean
  /** null = neuer Stack */
  stack: StackFiles | null
  /** Vorbelegung eines neuen Stacks (z. B. aus einer Vorlage) */
  draft?: StackFiles | null
  existing: string[]
  busy?: boolean
  check?: StackCheck | null
}>()
const emit = defineEmits<{
  'update:visible': [boolean]
  submit: [files: StackFiles, mode: SubmitMode]
}>()

const store = useDockerStore()
const name = ref('')
const compose = ref('')
const env = ref('')
const tab = ref('compose')
const editor = ref<{ insert?: (text: string) => boolean } | null>(null)
const browsing = ref(false)
const inserted = ref(false)
const pickingDevice = ref(false)
const insertedDevice = ref(false)

/** Fügt einen gewählten Ordner als Bind-Mount ein; den Zielpfad im Container muss man noch anpassen. */
function insertFolder(path: string) {
  const text = `${path}:/pfad-im-container`
  if (!editor.value?.insert?.(text)) compose.value = `${compose.value.replace(/\n*$/, '\n')}${text}\n`
  inserted.value = true
}

/** Fügt devices/group_add ein; der Aufrufer muss die Zeilen unter den passenden Dienst setzen. */
function insertDevices(devs: HostDevice[]) {
  const text = deviceSnippet(devs)
  if (!editor.value?.insert?.(text)) compose.value = `${compose.value.replace(/\n*$/, '\n')}${text}`
  insertedDevice.value = true
}

// --- Icon: gewählte Änderung wird erst mit „Speichern“ angewendet ---
const iconChange = ref<IconChange | null>(null)
const iconUrlInput = ref('')
const iconError = ref('')
const iconBroken = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
let blobUrl = ''

function revoke() {
  if (blobUrl) URL.revokeObjectURL(blobUrl)
  blobUrl = ''
}
onBeforeUnmount(revoke)

/** Aktuelle Vorschau: gewählte Änderung, sonst das gespeicherte Icon des Stacks. */
const preview = computed(() => {
  const ch = iconChange.value
  if (ch?.kind === 'remove') return null
  if (ch?.kind === 'file') return blobUrl
  if (ch?.kind === 'url') return ch.url
  return props.stack ? store.iconUrlOf(props.stack.name) : null
})

function pickFile(ev: Event) {
  const input = ev.target as HTMLInputElement
  const f = input.files?.[0]
  input.value = ''
  iconError.value = ''
  if (!f) return
  if (!f.type.startsWith('image/')) {
    iconError.value = 'Bitte eine Bilddatei wählen (PNG, JPEG, GIF, WebP oder SVG).'
    return
  }
  if (f.size > MAX_ICON_BYTES) {
    iconError.value = 'Das Bild ist zu groß (höchstens 512 KiB).'
    return
  }
  revoke()
  blobUrl = URL.createObjectURL(f)
  iconBroken.value = false
  iconChange.value = { kind: 'file', file: f }
}

function applyUrl() {
  const u = iconUrlInput.value.trim()
  iconError.value = ''
  if (!/^https?:\/\/\S+$/i.test(u)) {
    iconError.value = 'Bitte eine Adresse mit http:// oder https:// angeben.'
    return
  }
  revoke()
  iconBroken.value = false
  iconChange.value = { kind: 'url', url: u }
  iconUrlInput.value = ''
}

function removeIcon() {
  revoke()
  iconError.value = ''
  iconChange.value = { kind: 'remove' }
}

function resetIcon() {
  revoke()
  iconChange.value = null
  iconUrlInput.value = ''
  iconError.value = ''
  iconBroken.value = false
}

watch(
  () => [props.visible, props.stack] as const,
  ([v]) => {
    if (!v) return
    const src = props.stack ?? props.draft ?? null
    name.value = src?.name ?? ''
    compose.value = src?.compose ?? STACK_TEMPLATE
    env.value = src?.env ?? ''
    tab.value = 'compose'
    inserted.value = false
    insertedDevice.value = false
    resetIcon()
  },
  { immediate: true },
)

const isNew = computed(() => !props.stack)
const nameError = computed(() => {
  if (!isNew.value || !name.value) return ''
  if (!STACK_NAME_RE.test(name.value)) return 'Nur Kleinbuchstaben, Ziffern, - und _ (höchstens 40 Zeichen).'
  if (props.existing.includes(name.value)) return 'Ein Stack mit diesem Namen existiert bereits.'
  return ''
})
const valid = computed(() => !!name.value && !nameError.value && compose.value.trim() !== '')
const files = (): StackFiles => ({
  name: name.value,
  compose: compose.value,
  env: env.value,
  ...(iconChange.value ? { icon: iconChange.value } : {}),
})
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="isNew ? 'Neuer Stack' : `Stack „${stack?.name}“`"
    :style="{ width: 'min(860px, 96vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <div class="field">
      <label for="stack-name">Name</label>
      <InputText id="stack-name" v-model="name" :disabled="!isNew" :invalid="!!nameError" autocomplete="off" />
      <small v-if="nameError" class="err">{{ nameError }}</small>
    </div>
    <Tabs v-model:value="tab">
      <TabList>
        <Tab value="compose">compose.yaml</Tab>
        <Tab value="limits" data-test="stack-limits-tab">Ressourcen</Tab>
        <Tab value="env">.env</Tab>
        <Tab value="icon" data-test="stack-icon-tab">Icon</Tab>
      </TabList>
      <TabPanels>
        <TabPanel value="compose">
          <div class="folder-bar">
            <Button data-test="stack-insert-folder" label="Ordner einfügen" icon="pi pi-folder-open" size="small" severity="secondary" text @click="browsing = true" />
            <Button data-test="stack-insert-device" label="Gerät einfügen" icon="pi pi-microchip" size="small" severity="secondary" text @click="pickingDevice = true" />
            <small v-if="insertedDevice" class="hint" data-test="stack-device-hint">Eingefügt. Die Zeilen <code>devices:</code> und <code>group_add:</code> müssen direkt unter dem Dienst stehen, der das Gerät braucht (gleiche Einrückung wie <code>image:</code>).</small>
            <small v-else-if="inserted" class="hint" data-test="stack-insert-hint">Eingefügt. Ersetze „/pfad-im-container“ durch den Ordner, unter dem der Container die Daten erwartet.</small>
          </div>
          <YamlEditor ref="editor" v-model="compose" />
        </TabPanel>
        <TabPanel value="limits">
          <StackLimits v-model="compose" />
        </TabPanel>
        <TabPanel value="env">
          <Textarea id="stack-env" v-model="env" rows="12" class="env" spellcheck="false" placeholder="NAME=wert" />
          <small class="hint">Variablen für die compose.yaml, z. B. Passwörter. Die Datei ist nur für Administratoren lesbar.</small>
        </TabPanel>
        <TabPanel value="icon">
          <div class="icon-row" data-test="stack-icon">
            <span class="icon-preview" :class="{ empty: !preview || iconBroken }">
              <img v-if="preview && !iconBroken" :src="preview" alt="Vorschau des Icons" data-test="stack-icon-preview" @error="iconBroken = true" />
              <i v-else class="pi pi-image" />
            </span>
            <div class="icon-actions">
              <input ref="fileInput" type="file" accept="image/png,image/jpeg,image/gif,image/webp,image/svg+xml" hidden data-test="stack-icon-file" @change="pickFile" />
              <Button label="Bild hochladen …" icon="pi pi-upload" size="small" severity="secondary" @click="fileInput?.click()" />
              <Button v-if="preview" label="Entfernen" icon="pi pi-trash" size="small" text severity="danger" data-test="stack-icon-remove" @click="removeIcon" />
            </div>
          </div>
          <div class="field">
            <label for="stack-icon-url">oder Adresse eines Bildes</label>
            <div class="url-row">
              <InputText id="stack-icon-url" v-model="iconUrlInput" placeholder="https://…/icon.png" autocomplete="off" fluid data-test="stack-icon-url" @keydown.enter.prevent="applyUrl" />
              <Button label="Übernehmen" size="small" severity="secondary" :disabled="!iconUrlInput.trim()" data-test="stack-icon-url-apply" @click="applyUrl" />
            </div>
          </div>
          <small v-if="iconError" class="err" data-test="stack-icon-error">{{ iconError }}</small>
          <small v-else-if="iconChange?.kind === 'url'" class="hint">Das NAS lädt das Bild beim Speichern von der Adresse und legt es selbst ab.</small>
          <small v-else-if="iconChange?.kind === 'remove'" class="hint">Das Icon wird beim Speichern entfernt.</small>
          <small v-else class="hint">Das Icon erscheint auf den Kacheln aller Container dieses Stacks. PNG, JPEG, GIF, WebP oder SVG, höchstens 512 KiB.</small>
        </TabPanel>
      </TabPanels>
    </Tabs>
    <Message v-if="check" :severity="check.ok ? 'success' : 'error'" :closable="false" data-test="stack-check-result">
      <span v-if="check.ok">Die Compose-Datei ist gültig.</span>
      <pre v-else class="out">{{ check.message }}</pre>
    </Message>
    <DevicePicker :visible="pickingDevice" @update:visible="pickingDevice = $event" @select="insertDevices" />
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button data-test="stack-check" label="Speichern & prüfen" icon="pi pi-check-circle" severity="secondary" :disabled="!valid || busy" @click="emit('submit', files(), 'check')" />
      <Button data-test="stack-save" label="Speichern" severity="secondary" :disabled="!valid || busy" @click="emit('submit', files(), 'save')" />
      <Button data-test="stack-start" label="Speichern & starten" icon="pi pi-play" :disabled="!valid || busy" :loading="busy" @click="emit('submit', files(), 'start')" />
    </template>
  </Dialog>
  <FolderPicker :visible="browsing" @update:visible="browsing = $event" @select="insertFolder" />
</template>

<style scoped>
.folder-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem 0.75rem;
  margin-bottom: 0.4rem;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  margin-bottom: 0.75rem;
}
.err {
  color: var(--bn-danger);
}
.hint {
  color: var(--bn-text-soft);
}
.icon-row {
  display: flex;
  align-items: center;
  gap: 1rem;
  margin-bottom: 1rem;
}
.icon-preview {
  width: 72px;
  height: 72px;
  flex: none;
  display: grid;
  place-items: center;
  border-radius: var(--bn-radius-sm);
  background: var(--bn-glass-strong);
  border: 1px solid var(--bn-border);
  overflow: hidden;
}
.icon-preview img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  padding: 8px;
}
.icon-preview.empty {
  color: var(--bn-text-soft);
  font-size: 1.6rem;
}
.icon-actions {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
}
.url-row {
  display: flex;
  gap: 0.5rem;
}
.env {
  width: 100%;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.out {
  margin: 0;
  white-space: pre-wrap;
  font-size: 0.85rem;
}
</style>
