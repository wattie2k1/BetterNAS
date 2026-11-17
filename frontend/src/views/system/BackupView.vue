<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { backupApi, type RestorePreview } from '@/api/backup'
import { useBackupStore } from '@/stores/backup'
import { formatBytes, formatDateTime } from '@/utils/format'

const CONFIRM_WORD = 'WIEDERHERSTELLEN'

const store = useBackupStore()
const toast = useToast()
const fileInput = ref<HTMLInputElement | null>(null)
const creating = ref(false)
const error = ref('')
const deleting = ref<string | null>(null)
const preview = ref<RestorePreview | null>(null)
const source = ref('')
const typed = ref('')
const busy = ref(false)

onMounted(() => store.load())

const msg = (e: unknown) => (e instanceof Error ? e.message : String(e))
const dialogOpen = computed({ get: () => preview.value !== null, set: (v: boolean) => !v && (preview.value = null) })
const canRestore = computed(() => typed.value === CONFIRM_WORD && !busy.value)

async function create() {
  creating.value = true
  error.value = ''
  try {
    await store.create()
    toast.add({ severity: 'success', summary: 'Sicherung angelegt', life: 3000 })
  } catch (e) {
    error.value = msg(e)
  } finally {
    creating.value = false
  }
}

async function remove() {
  const name = deleting.value
  deleting.value = null
  if (!name) return
  try {
    await store.remove(name)
  } catch (e) {
    error.value = msg(e)
  }
}

async function openPreview(load: () => Promise<RestorePreview>, label: string) {
  error.value = ''
  try {
    preview.value = await load()
    source.value = label
    typed.value = ''
  } catch (e) {
    error.value = msg(e)
  }
}

async function onFile(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  await openPreview(() => store.previewUpload(file), file.name)
  input.value = ''
}

async function doRestore() {
  if (!preview.value) return
  busy.value = true
  try {
    const token = preview.value.token
    preview.value = null
    void store.restore(token).catch((e) => {
      store.restarting = false
      error.value = msg(e)
    })
  } finally {
    busy.value = false
  }
}

const countLabel = (c: Record<string, number>, key: string, one: string, many: string) => `${c[key] ?? 0} ${(c[key] ?? 0) === 1 ? one : many}`
</script>

<template>
  <Message v-if="store.restarting" severity="info" :closable="false" data-test="restarting">
    Die Wiederherstellung läuft. BetterNAS startet neu … Die Seite verbindet sich automatisch wieder; danach ist eine neue Anmeldung nötig.
  </Message>
  <Message v-if="error || store.error" severity="error" :closable="false" data-test="backup-error">{{ error || store.error }}</Message>

  <div class="toolbar">
    <input ref="fileInput" type="file" accept=".gz,.tgz,application/gzip" hidden data-test="backup-file" @change="onFile" />
    <Button label="Backup einspielen …" icon="pi pi-upload" outlined :disabled="store.restarting" @click="fileInput?.click()" />
    <Button label="Jetzt sichern" icon="pi pi-save" data-test="backup-create" :loading="creating" :disabled="store.restarting" @click="create" />
  </div>

  <p class="sub">
    BetterNAS sichert seine Konfiguration jede Nacht um 02:30 (die letzten 7 bleiben). Die Sicherungen liegen auf der Systemplatte –
    lade regelmäßig eine herunter und bewahre sie außerhalb des NAS auf. Sie enthalten auch Zugangsdaten (z. B. für Mail und ntfy) –
    behandle sie vertraulich.
  </p>

  <p v-if="!store.backups.length && !store.loading" class="sub">Noch keine Sicherung vorhanden.</p>
  <div v-else class="table-wrap">
    <table class="backups">
      <thead>
        <tr><th>Erstellt</th><th>Größe</th><th>Version</th><th>Inhalt</th><th /></tr>
      </thead>
      <tbody>
        <tr v-for="b in store.backups" :key="b.name" data-test="backup-row">
          <td>{{ b.manifest.createdAt ? formatDateTime(b.manifest.createdAt) : b.name }}<div class="sub">{{ b.name }}</div></td>
          <td>{{ formatBytes(b.size) }}</td>
          <td>{{ b.manifest.version || '—' }}</td>
          <td>
            <template v-if="b.manifest.counts">
              {{ countLabel(b.manifest.counts, 'pools', 'Pool', 'Pools') }} · {{ countLabel(b.manifest.counts, 'shares', 'Freigabe', 'Freigaben') }}
            </template>
          </td>
          <td class="actions">
            <a :href="backupApi.downloadUrl(b.name)" download class="dl" title="Herunterladen"><i class="pi pi-download" /> Herunterladen</a>
            <Button label="Wiederherstellen …" size="small" outlined data-test="backup-restore" :disabled="store.restarting" @click="openPreview(() => store.previewStored(b.name), b.name)" />
            <Button icon="pi pi-trash" size="small" text severity="danger" aria-label="Löschen" data-test="backup-delete" @click="deleting = b.name" />
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <Dialog v-model:visible="dialogOpen" modal append-to="self" header="Konfiguration wiederherstellen" :style="{ width: 'min(560px, 95vw)' }">
    <template v-if="preview">
      <p class="sub">{{ source }}</p>
      <ul class="facts">
        <li>Erstellt: {{ formatDateTime(preview.manifest.createdAt) }} (BetterNAS {{ preview.manifest.version }}, {{ preview.manifest.host }})</li>
        <li>
          {{ countLabel(preview.manifest.counts, 'pools', 'Pool', 'Pools') }},
          {{ countLabel(preview.manifest.counts, 'shares', 'Freigabe', 'Freigaben') }},
          {{ countLabel(preview.manifest.counts, 'stacks', 'Docker-Stack', 'Docker-Stacks') }},
          {{ countLabel(preview.manifest.counts, 'disks', 'Datenträger', 'Datenträger') }}
        </li>
      </ul>
      <Message v-for="w in preview.warnings" :key="w" severity="warn" :closable="false">{{ w }}</Message>
      <Message severity="warn" :closable="false">
        Die aktuelle Konfiguration wird ersetzt (die alte Datenbank bleibt als Kopie liegen). Alle werden abgemeldet und BetterNAS startet neu.
        Nicht enthalten sind Benutzerkonten und Samba-Passwörter, Docker-Daten und der TLS-Schlüssel.
      </Message>
      <label for="restore-confirm" class="confirm-label">Zur Bestätigung <strong>{{ CONFIRM_WORD }}</strong> eintippen:</label>
      <input id="restore-confirm" v-model="typed" type="text" autocomplete="off" class="confirm" />
    </template>
    <template #footer>
      <Button label="Abbrechen" text @click="dialogOpen = false" />
      <Button label="Wiederherstellen" severity="danger" data-test="restore-submit" :disabled="!canRestore" @click="doRestore" />
    </template>
  </Dialog>

  <ConfirmDialog
    :visible="deleting !== null"
    header="Sicherung löschen"
    :message="`Die Sicherung ${deleting ?? ''} wird unwiderruflich gelöscht.`"
    @update:visible="(v: boolean) => !v && (deleting = null)"
    @confirm="remove"
  />
</template>

<style scoped>
.toolbar {
  display: flex;
  justify-content: flex-end;
  gap: 0.5rem;
  margin-bottom: 1rem;
  flex-wrap: wrap;
}
.sub {
  color: var(--p-text-muted-color);
  font-size: 0.85rem;
}
.table-wrap {
  overflow-x: auto;
}
.backups {
  width: 100%;
  border-collapse: collapse;
}
th,
td {
  text-align: left;
  padding: 0.6rem 0.9rem;
  border-bottom: 1px solid var(--bn-border-soft);
  vertical-align: top;
}
th {
  font-size: 0.8rem;
  color: var(--p-text-muted-color);
}
.actions {
  display: flex;
  gap: 0.5rem;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
}
.dl {
  color: var(--p-primary-color);
  text-decoration: none;
  font-size: 0.85rem;
}
.facts {
  padding-left: 1.2rem;
}
.confirm-label {
  display: block;
  margin: 0.8rem 0 0.3rem;
}
.confirm {
  width: 100%;
  padding: 0.5rem 0.6rem;
  border-radius: 8px;
  border: 1px solid var(--bn-border);
  background: var(--bn-glass);
  color: inherit;
  font: inherit;
}
</style>
