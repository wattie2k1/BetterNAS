<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Card from 'primevue/card'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import TemplateDialog from '@/components/docker/TemplateDialog.vue'
import StackDialog, { type SubmitMode } from '@/components/docker/StackDialog.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { useDockerStore } from '@/stores/docker'
import type { StackAction, StackCheck, StackFiles, StackInfo } from '@/api/docker'

const store = useDockerStore()
const toast = useToast()
const showDialog = ref(false)
const editing = ref<StackFiles | null>(null)
const draft = ref<StackFiles | null>(null)
const showTemplates = ref(false)
const check = ref<StackCheck | null>(null)
const deleting = ref<StackInfo | null>(null)
const busy = ref(false)
const names = computed(() => store.stacks.map((s) => s.name))
const dockerDown = computed(() => store.status && (!store.status.installed || !store.status.active))

const checking = ref(false)
onMounted(() => Promise.all([store.refresh(), store.loadUpdates()]))

const outdated = (name: string) => store.updates.stacks[name]?.outdated ?? []
const checkedAt = computed(() => (store.updates.checkedAt ? new Date(store.updates.checkedAt).toLocaleString('de-DE') : ''))

async function checkUpdates() {
  checking.value = true
  try {
    await store.checkUpdates()
    const n = store.stacks.filter((s) => outdated(s.name).length).length
    toast.add({ severity: n ? 'info' : 'success', summary: n ? `${n} Stack(s) mit Updates` : 'Alles aktuell', life: 5000 })
  } catch (e) {
    fail(e)
  } finally {
    checking.value = false
  }
}

const fail = (e: unknown) =>
  toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })

const labels: Record<StackAction, string> = {
  up: 'Stack gestartet',
  down: 'Stack heruntergefahren',
  stop: 'Stack gestoppt',
  start: 'Stack gestartet',
  restart: 'Stack neu gestartet',
  pull: 'Images geladen',
  update: 'Stack aktualisiert',
}

async function run(name: string, action: StackAction) {
  busy.value = true
  try {
    await store.run(name, action)
    toast.add({ severity: 'success', summary: labels[action], detail: name, life: 4000 })
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

function openNew() {
  editing.value = null
  draft.value = null
  check.value = null
  showDialog.value = true
}

function fromTemplate(files: StackFiles) {
  editing.value = null
  draft.value = files
  check.value = null
  showDialog.value = true
}

async function openEdit(s: StackInfo) {
  try {
    editing.value = await store.load(s.name)
    check.value = null
    showDialog.value = true
  } catch (e) {
    fail(e)
  }
}

async function submit(files: StackFiles, mode: SubmitMode) {
  busy.value = true
  try {
    if (names.value.includes(files.name)) await store.update(files)
    else await store.create(files)
    editing.value = files // ab jetzt existiert der Stack
    if (mode === 'check') {
      check.value = await store.validate(files.name)
      return
    }
    showDialog.value = false
    toast.add({ severity: 'success', summary: 'Stack gespeichert', detail: files.name, life: 4000 })
    busy.value = false
    if (mode === 'start') await run(files.name, 'up')
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

async function remove() {
  const s = deleting.value
  if (!s) return
  busy.value = true
  try {
    await store.remove(s.name)
    toast.add({ severity: 'success', summary: 'Stack gelöscht', detail: s.name, life: 4000 })
    deleting.value = null
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="toolbar">
    <span v-if="checkedAt" class="sub" data-test="checked-at">Zuletzt auf Updates geprüft: {{ checkedAt }}</span>
    <Button data-test="check-updates" label="Auf Updates prüfen" icon="pi pi-sync" severity="secondary" :loading="checking" :disabled="!!dockerDown || !store.stacks.length" @click="checkUpdates" />
    <Button data-test="from-template" label="Aus Vorlage" icon="pi pi-th-large" severity="secondary" :disabled="!!dockerDown" @click="showTemplates = true" />
    <Button label="Neuer Stack" icon="pi pi-plus" :disabled="!!dockerDown" @click="openNew" />
  </div>
  <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>
  <Message v-if="dockerDown" severity="warn" :closable="false" data-test="docker-down">
    {{ store.status?.installed ? 'Der Docker-Dienst läuft nicht. Gespeicherte Stacks bleiben erhalten; Starten ist erst möglich, wenn Docker läuft.' : 'Docker ist nicht installiert.' }}
  </Message>
  <p v-if="!store.stacks.length && !store.loading" class="empty">
    Noch keine Stacks. Ein Stack ist eine Gruppe zusammengehöriger Container, beschrieben in einer compose.yaml (z. B. Jellyfin, Nextcloud, Home Assistant).
  </p>
  <div class="grid">
    <Card v-for="s in store.stacks" :key="s.name" :data-test="`stack-${s.name}`">
      <template #title>
        <div class="title">
          <span><i class="pi pi-clone" /> {{ s.name }}</span>
          <Tag v-if="outdated(s.name).length" data-test="update-tag" value="Update verfügbar" severity="info" :title="`Neue Version für: ${outdated(s.name).join(', ')}`" />
          <Tag
            :value="s.total ? `${s.running} von ${s.total} laufen` : 'Nicht gestartet'"
            :severity="s.total && s.running === s.total ? 'success' : s.running ? 'warn' : 'secondary'"
          />
        </div>
      </template>
      <template #footer>
        <div class="actions">
          <Button icon="pi pi-play" text rounded aria-label="Starten" title="Starten (up)" :disabled="busy || !!dockerDown" @click="run(s.name, 'up')" />
          <Button icon="pi pi-stop" text rounded aria-label="Stoppen" title="Stoppen" :disabled="busy || !!dockerDown" @click="run(s.name, 'stop')" />
          <Button icon="pi pi-refresh" text rounded aria-label="Neu starten" title="Neu starten" :disabled="busy || !!dockerDown" @click="run(s.name, 'restart')" />
          <Button icon="pi pi-download" text rounded aria-label="Aktualisieren" title="Aktualisieren (neue Images laden und neu starten)" :disabled="busy || !!dockerDown" @click="run(s.name, 'update')" />
          <Button icon="pi pi-pencil" text rounded aria-label="Bearbeiten" title="Bearbeiten" @click="openEdit(s)" />
          <Button icon="pi pi-trash" text rounded severity="danger" aria-label="Löschen" title="Löschen" @click="deleting = s" />
        </div>
      </template>
    </Card>
  </div>
  <TemplateDialog v-model:visible="showTemplates" :existing="names" @create="fromTemplate" />
  <StackDialog v-model:visible="showDialog" :stack="editing" :draft="draft" :existing="names" :busy="busy" :check="check" @submit="submit" />
  <ConfirmDialog
    :visible="!!deleting"
    header="Stack löschen"
    :message="`Der Stack „${deleting?.name}“ wird heruntergefahren und seine Dateien (compose.yaml, .env) werden gelöscht. Volumes und Daten in Ordnern bleiben erhalten.`"
    :busy="busy"
    @update:visible="(v) => !v && (deleting = null)"
    @confirm="remove"
  />
</template>

<style scoped>
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
  color: var(--bn-text-soft);
  font-size: 0.875rem;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.1rem;
}
</style>
