<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import FolderPicker from '@/components/docker/FolderPicker.vue'
import { dockerApi, type HostDevice, type StackFiles } from '@/api/docker'
import { APP_TEMPLATES, defaultValues, containerNameError, fieldError, groupTemplates, randomSecret, renderTemplate, stackNameError, type AppTemplate } from '@/utils/appTemplates'

const props = defineProps<{ visible: boolean; existing: string[]; containers?: string[] }>()
const emit = defineEmits<{ 'update:visible': [boolean]; create: [files: StackFiles] }>()

const picked = ref<AppTemplate | null>(null)
const query = ref('')
const groups = computed(() => groupTemplates(APP_TEMPLATES, query.value))
const name = ref('')
const values = ref<Record<string, string>>({})
const browsing = ref<string | null>(null)
const devices = ref<HostDevice[]>([])
const deviceChoice = ref('')
const deviceOptions = computed(() => devices.value.filter((d) => d.kind === picked.value?.device?.kind))
const deviceNote = computed(() => deviceOptions.value.find((d) => d.path === deviceChoice.value)?.note ?? '')

watch(
  () => props.visible,
  (v) => {
    if (v) {
      picked.value = null
      query.value = ''
    }
  },
)

function pick(t: AppTemplate) {
  picked.value = t
  name.value = t.stack
  let n = 2
  while (props.existing.includes(name.value) || containerNameError(t, name.value, props.containers ?? [])) name.value = `${t.stack}-${n++}`
  values.value = defaultValues(t, name.value)
  deviceChoice.value = ''
  devices.value = []
  if (t.device) dockerApi.devices().then((l) => (devices.value = Array.isArray(l) ? l : []), () => (devices.value = []))
}

// Standardpfade folgen dem Stacknamen, solange sie nicht von Hand geändert wurden.
let lastName = ''
watch(name, (n, old) => {
  const t = picked.value
  if (!t) return
  lastName = old ?? lastName
  for (const f of t.fields) {
    if (f.kind === 'path' && f.def && values.value[f.key] === f.def(lastName)) values.value[f.key] = f.def(n)
  }
})

const nameErr = computed(() =>
  picked.value ? stackNameError(name.value, props.existing) || containerNameError(picked.value, name.value, props.containers ?? []) : '',
)
const errors = computed(() => {
  const t = picked.value
  const e: Record<string, string> = {}
  if (t) for (const f of t.fields) e[f.key] = fieldError(f, values.value[f.key] ?? '')
  return e
})
const valid = computed(() => !!picked.value && !nameErr.value && Object.values(errors.value).every((e) => !e))

function submit() {
  if (!picked.value || !valid.value) return
  emit('create', renderTemplate(picked.value, name.value, values.value, deviceOptions.value.filter((d) => d.path === deviceChoice.value)))
  emit('update:visible', false)
}
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    header="Neuer Stack aus Vorlage"
    :style="{ width: 'min(720px, 96vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <div v-if="!picked" data-test="template-list">
      <InputText v-model="query" class="search" placeholder="Vorlage suchen" aria-label="Vorlage suchen" autocomplete="off" data-test="template-search" />
      <p v-if="!groups.length" class="hint" data-test="template-empty">Keine Vorlage gefunden.</p>
      <section v-for="g in groups" :key="g.category">
        <h3 class="group" :data-test="`template-group-${g.category}`">{{ g.category }}</h3>
        <div class="tpl-grid">
          <button v-for="t in g.items" :key="t.id" type="button" class="tpl" :data-test="`template-${t.id}`" @click="pick(t)">
            <i :class="t.icon" />
            <strong>{{ t.title }}</strong>
            <span>{{ t.description }}</span>
          </button>
        </div>
      </section>
    </div>
    <div v-else class="form" data-test="template-form">
      <div class="field">
        <label for="tpl-name">Stackname</label>
        <InputText id="tpl-name" v-model="name" :invalid="!!nameErr" autocomplete="off" />
        <small v-if="nameErr" class="err">{{ nameErr }}</small>
      </div>
      <div v-for="f in picked.fields" :key="f.key" class="field">
        <label :for="`tpl-${f.key}`">{{ f.label }}</label>
        <div class="row">
          <InputText :id="`tpl-${f.key}`" v-model="values[f.key]" :invalid="!!errors[f.key]" autocomplete="off" spellcheck="false" :data-test="`tpl-field-${f.key}`" />
          <Button
            v-if="f.kind === 'path'"
            icon="pi pi-folder-open"
            text
            severity="secondary"
            aria-label="Ordner wählen"
            title="Ordner wählen"
            :data-test="`tpl-browse-${f.key}`"
            @click="browsing = f.key"
          />
          <Button v-if="f.kind === 'secret'" icon="pi pi-refresh" text severity="secondary" aria-label="Neu erzeugen" title="Neu erzeugen" @click="values[f.key] = randomSecret()" />
        </div>
        <small v-if="errors[f.key]" class="err">{{ errors[f.key] }}</small>
        <small v-else-if="f.hint" class="hint">{{ f.hint }}</small>
      </div>
      <div v-if="picked.device" class="field" data-test="tpl-device">
        <label for="tpl-device-select">{{ picked.device.label }}</label>
        <select id="tpl-device-select" v-model="deviceChoice" data-test="tpl-device-select" :disabled="!deviceOptions.length">
          <option value="">Nicht durchreichen</option>
          <option v-for="d in deviceOptions" :key="d.path" :value="d.path">{{ d.name }} ({{ d.path }})</option>
        </select>
        <small v-if="deviceNote" class="err">{{ deviceNote }}</small>
        <small v-else-if="!deviceOptions.length" class="hint">Kein passendes Gerät gefunden.</small>
        <small v-else-if="picked.device.hint" class="hint">{{ picked.device.hint }}</small>
      </div>
      <Message v-if="picked.notice" severity="warn" :closable="false" data-test="tpl-notice">{{ picked.notice }}</Message>
      <Message severity="info" :closable="false">
        Die Eingaben werden in der .env gespeichert (nur für Administratoren lesbar). Im nächsten Schritt kannst du alles noch ändern, bevor der Stack angelegt wird.
      </Message>
    </div>
    <template #footer>
      <Button v-if="picked" label="Zurück" text @click="picked = null" />
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button v-if="picked" data-test="template-next" label="Weiter" icon="pi pi-arrow-right" icon-pos="right" :disabled="!valid" @click="submit" />
    </template>
  </Dialog>
  <FolderPicker
    :visible="browsing !== null"
    :start="browsing ? values[browsing] : ''"
    @update:visible="(v) => !v && (browsing = null)"
    @select="(p) => browsing && (values[browsing] = p)"
  />
</template>

<style scoped>
.search {
  width: 100%;
  margin-bottom: 0.75rem;
}
.group {
  margin: 1rem 0 0.5rem;
  font-size: 0.95rem;
  color: var(--bn-text-soft);
}
.tpl-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 0.75rem;
}
.tpl {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.35rem;
  padding: 0.9rem;
  text-align: left;
  color: inherit;
  font: inherit;
  cursor: pointer;
  border: 1px solid var(--p-content-border-color);
  border-radius: var(--bn-radius-xs);
  background: transparent;
}
.tpl:hover,
.tpl:focus-visible {
  border-color: var(--p-primary-color);
}
.tpl i {
  font-size: 1.4rem;
  color: var(--p-primary-color);
}
.tpl span {
  color: var(--bn-text-soft);
  font-size: 0.85rem;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  margin-bottom: 0.75rem;
}
.row {
  display: flex;
  gap: 0.25rem;
}
.row :deep(input) {
  flex: 1;
}
.err {
  color: var(--bn-danger);
}
.hint {
  color: var(--bn-text-soft);
}
</style>
