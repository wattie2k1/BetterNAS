<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import FolderPicker from '@/components/docker/FolderPicker.vue'
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

const form = ref({ enabled: true, time: '02:30', keep: 7, dir: '', appdata: false })
const browsing = ref(false)
const creatingAppdata = ref(false)
/** appdata lässt sich nur sichern, wenn Docker eingerichtet ist (dann kennt der Agent den Ordner). */
const appdataFolder = computed(() => store.status?.appdata ?? '')
const savingCfg = ref(false)
const cfgDirty = computed(() => JSON.stringify(form.value) !== JSON.stringify(store.config))

onMounted(async () => {
  void store.load()
  void store.loadStatus()
  await store.loadConfig()
  form.value = { ...store.config }
})

async function saveConfig() {
  savingCfg.value = true
  error.value = ''
  try {
    await store.saveConfig({ enabled: form.value.enabled, time: form.value.time, keep: Number(form.value.keep), dir: form.value.dir, appdata: form.value.appdata })
    form.value = { ...store.config }
    toast.add({ severity: 'success', summary: 'Einstellungen gespeichert', life: 4000 })
  } catch (e) {
    error.value = msg(e)
  } finally {
    savingCfg.value = false
  }
}

function pickDir(path: string) {
  form.value.dir = path
}

async function createAppdata() {
  creatingAppdata.value = true
  error.value = ''
  try {
    await store.createAppdata()
    toast.add({ severity: 'success', summary: 'appdata gesichert', life: 4000 })
  } catch (e) {
    error.value = msg(e)
  } finally {
    creatingAppdata.value = false
  }
}

const hasAppdataBackup = computed(() => store.backups.some((b) => b.kind === 'appdata'))

const msg = (e: unknown) => (e instanceof Error ? e.message : String(e))
const dialogOpen = computed({ get: () => preview.value !== null, set: (v: boolean) => !v && (preview.value = null) })
const canRestore = computed(() => typed.value === CONFIRM_WORD && !busy.value)

async function create() {
  creating.value = true
  error.value = ''
  try {
    await store.create()
    toast.add({ severity: 'success', summary: 'Sicherung angelegt', life: 4000 })
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
    <Button v-if="store.config.appdata" label="appdata jetzt sichern" icon="pi pi-folder" outlined data-test="appdata-create" :loading="creatingAppdata" :disabled="store.restarting" @click="createAppdata" />
    <Button label="Jetzt sichern" icon="pi pi-save" data-test="backup-create" :loading="creating" :disabled="store.restarting" @click="create" />
  </div>

  <p class="sub">
    <template v-if="store.status && !store.status.default">Die Sicherungen liegen in <code>{{ store.status.dir }}</code>.</template>
    <template v-else>Die Sicherungen liegen auf der Systemplatte – wähle unten einen anderen Ordner, am besten auf einer anderen Platte.</template>
    Bewahre regelmäßig eine Kopie außerhalb des NAS auf. Die Konfigurations-Sicherung enthält auch Zugangsdaten (z. B. für Mail und ntfy) – behandle sie vertraulich.
  </p>

  <section class="card cfg" data-test="backup-config">
    <h2>Automatische Sicherung</h2>
    <label class="check"><input v-model="form.enabled" type="checkbox" data-test="cfg-enabled" /> Täglich automatisch sichern</label>
    <div class="cfg-fields">
      <div class="field">
        <label for="cfg-time">Uhrzeit</label>
        <input id="cfg-time" v-model="form.time" type="time" required :disabled="!form.enabled" data-test="cfg-time" />
      </div>
      <div class="field">
        <label for="cfg-keep">Anzahl behaltener Sicherungen</label>
        <input id="cfg-keep" v-model.number="form.keep" type="number" min="1" max="90" data-test="cfg-keep" />
      </div>
    </div>
    <p class="sub">Ältere Sicherungen werden nach jeder neuen Sicherung automatisch gelöscht – auch manuell angelegte zählen mit (Konfiguration und appdata getrennt).</p>

    <h3>Speicherort</h3>
    <div class="loc">
      <strong data-test="loc-dir">{{ form.dir || 'Systemplatte (Standard)' }}</strong>
      <Button label="Ordner wählen …" icon="pi pi-folder-open" size="small" outlined data-test="loc-pick" @click="browsing = true" />
      <Button v-if="form.dir" label="Systemplatte verwenden" size="small" text data-test="loc-reset" @click="form.dir = ''" />
    </div>
    <p v-if="store.status && !store.status.problem && store.status.free" class="sub" data-test="loc-free">Frei am aktuellen Ort: {{ formatBytes(store.status.free) }}</p>
    <Message v-if="store.status?.problem" severity="warn" :closable="false" data-test="loc-problem">{{ store.status.problem }}</Message>
    <p class="sub">Der Ordner wird beim Speichern angelegt und geprüft. Bereits vorhandene Sicherungen bleiben im bisherigen Ordner liegen und erscheinen hier nicht mehr.</p>

    <h3>appdata</h3>
    <label class="check">
      <input v-model="form.appdata" type="checkbox" :disabled="!appdataFolder && !form.appdata" data-test="cfg-appdata" />
      appdata mitsichern (Einstellungen der Docker-Apps)
    </label>
    <p v-if="appdataFolder" class="sub">Gesichert wird <code>{{ appdataFolder }}</code> als eigenes Archiv, nächtlich zusammen mit der Konfiguration. Pools und die Daten der Apps (dockerdata) sind nicht enthalten.</p>
    <p v-else class="sub" data-test="appdata-hint">Verfügbar, sobald Docker eingerichtet ist (Einstellungen → Docker).</p>
    <p v-if="form.appdata" class="sub">Laufende Apps schreiben währenddessen weiter; Datenbanken im Archiv können daher in einem Zwischenzustand stehen. Für beste Ergebnisse die Uhrzeit auf eine ruhige Zeit legen.</p>
    <Button label="Speichern" icon="pi pi-check" size="small" data-test="cfg-save" :loading="savingCfg" :disabled="!cfgDirty" @click="saveConfig" />
  </section>

  <p v-if="!store.backups.length && !store.loading" class="empty">Noch keine Sicherung vorhanden.</p>
  <div v-else class="table-wrap">
    <table class="backups">
      <thead>
        <tr><th>Erstellt</th><th>Art</th><th>Größe</th><th>Version</th><th>Inhalt</th><th /></tr>
      </thead>
      <tbody>
        <tr v-for="b in store.backups" :key="b.name" data-test="backup-row">
          <td>{{ b.manifest.createdAt ? formatDateTime(b.manifest.createdAt) : b.name }}<div class="sub">{{ b.name }}</div></td>
          <td data-test="backup-kind">{{ b.kind === 'appdata' ? 'appdata' : 'Konfiguration' }}</td>
          <td>{{ formatBytes(b.size) }}</td>
          <td>{{ b.manifest.version || '—' }}</td>
          <td>
            <template v-if="b.kind === 'appdata'">Einstellungen der Docker-Apps</template>
            <template v-else-if="b.manifest.counts">
              {{ countLabel(b.manifest.counts, 'pools', 'Pool', 'Pools') }} · {{ countLabel(b.manifest.counts, 'shares', 'Freigabe', 'Freigaben') }}
            </template>
          </td>
          <td class="actions">
            <a :href="backupApi.downloadUrl(b.name)" download class="dl" title="Herunterladen"><i class="pi pi-download" /> Herunterladen</a>
            <Button v-if="b.kind !== 'appdata'" label="Wiederherstellen …" size="small" outlined data-test="backup-restore" :disabled="store.restarting" @click="openPreview(() => store.previewStored(b.name), b.name)" />
            <Button icon="pi pi-trash" size="small" text severity="danger" aria-label="Löschen" data-test="backup-delete" @click="deleting = b.name" />
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <p v-if="hasAppdataBackup" class="sub" data-test="appdata-restore-hint">
    appdata zurückspielen: Container der betroffenen Stacks stoppen und das Archiv als root in den Ordner <code>{{ appdataFolder || 'appdata' }}</code> entpacken
    (<code>tar -xzpf &lt;Datei&gt; -C {{ appdataFolder || '&lt;appdata&gt;' }}</code>).
  </p>

  <FolderPicker :visible="browsing" :start="form.dir" @update:visible="(v: boolean) => (browsing = v)" @select="pickDir" />

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
.sub {
  color: var(--bn-text-soft);
  font-size: 0.85rem;
}
.cfg {
  display: flex;
  flex-direction: column;
  gap: 0.7rem;
  align-items: flex-start;
}
.cfg-fields {
  display: flex;
  gap: 1rem;
  flex-wrap: wrap;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}
.field label {
  font-size: 0.8rem;
  color: var(--bn-text-soft);
}
.check {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.backups {
  width: 100%;
  border-collapse: collapse;
}
.actions {
  display: flex;
  gap: 0.5rem;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
}
.loc {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  flex-wrap: wrap;
}
.cfg h3 {
  margin: 0.4rem 0 0;
  font-size: 0.95rem;
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
}
</style>
