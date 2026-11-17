<script setup lang="ts">
import { computed, defineAsyncComponent, ref, watch } from 'vue'
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
import { STACK_NAME_RE, STACK_TEMPLATE, type StackCheck, type StackFiles } from '@/api/docker'

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

const name = ref('')
const compose = ref('')
const env = ref('')
const tab = ref('compose')

watch(
  () => [props.visible, props.stack] as const,
  ([v]) => {
    if (!v) return
    const src = props.stack ?? props.draft ?? null
    name.value = src?.name ?? ''
    compose.value = src?.compose ?? STACK_TEMPLATE
    env.value = src?.env ?? ''
    tab.value = 'compose'
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
const files = (): StackFiles => ({ name: name.value, compose: compose.value, env: env.value })
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
        <Tab value="env">.env</Tab>
      </TabList>
      <TabPanels>
        <TabPanel value="compose">
          <YamlEditor v-model="compose" />
        </TabPanel>
        <TabPanel value="env">
          <Textarea id="stack-env" v-model="env" rows="12" class="env" spellcheck="false" placeholder="NAME=wert" />
          <small class="hint">Variablen für die compose.yaml, z. B. Passwörter. Die Datei ist nur für Administratoren lesbar.</small>
        </TabPanel>
      </TabPanels>
    </Tabs>
    <Message v-if="check" :severity="check.ok ? 'success' : 'error'" :closable="false" data-test="stack-check-result">
      <span v-if="check.ok">Die Compose-Datei ist gültig.</span>
      <pre v-else class="out">{{ check.message }}</pre>
    </Message>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button data-test="stack-check" label="Speichern & prüfen" icon="pi pi-check-circle" severity="secondary" :disabled="!valid || busy" @click="emit('submit', files(), 'check')" />
      <Button data-test="stack-save" label="Speichern" severity="secondary" :disabled="!valid || busy" @click="emit('submit', files(), 'save')" />
      <Button data-test="stack-start" label="Speichern & starten" icon="pi pi-play" :disabled="!valid || busy" :loading="busy" @click="emit('submit', files(), 'start')" />
    </template>
  </Dialog>
</template>

<style scoped>
.field {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  margin-bottom: 0.75rem;
}
.err {
  color: var(--p-red-500);
}
.hint {
  color: var(--p-text-muted-color);
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
