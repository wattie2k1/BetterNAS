<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import Button from 'primevue/button'
import Message from 'primevue/message'
import Select from 'primevue/select'
import { fetchLog, logDownloadUrl, maintenanceApi, SUPPORT_BUNDLE_URL, type AuditEntry, type LogSource } from '@/api/security'
import { formatDateTime } from '@/utils/format'

type Source = LogSource | 'audit'
const sources: { label: string; value: Source }[] = [
  { label: 'Änderungsprotokoll (Audit)', value: 'audit' },
  { label: 'BetterNAS-API', value: 'api' },
  { label: 'BetterNAS-Agent', value: 'agent' },
  { label: 'Samba', value: 'samba' },
  { label: 'Docker', value: 'docker' },
  { label: 'libvirt', value: 'libvirt' },
]
const lineOptions = [100, 200, 500, 1000, 2000]

const source = ref<Source>('audit')
const lines = ref(200)
const text = ref('')
const audit = ref<AuditEntry[]>([])
const error = ref('')
const loading = ref(false)

async function load() {
  loading.value = true
  error.value = ''
  try {
    if (source.value === 'audit') audit.value = await maintenanceApi.audit(lines.value)
    else text.value = await fetchLog(source.value, lines.value)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch([source, lines], load)
</script>

<template>
  <div class="card" data-test="settings-logs">
    <h3>Protokolle</h3>
    <div class="bar">
      <Select v-model="source" :options="sources" option-label="label" option-value="value" data-test="logs-source" />
      <Select v-model="lines" :options="lineOptions" data-test="logs-lines" />
      <Button label="Aktualisieren" icon="pi pi-refresh" severity="secondary" :loading="loading" data-test="logs-reload" @click="load" />
      <a v-if="source !== 'audit'" :href="logDownloadUrl(source, lines)" class="link" data-test="logs-download" download>
        <i class="pi pi-download" /> Als Datei laden
      </a>
    </div>
    <Message v-if="error" severity="error" :closable="false" data-test="logs-error">{{ error }}</Message>
    <table v-if="source === 'audit'" class="table" data-test="audit-table">
      <thead>
        <tr><th>Zeit</th><th>Benutzer</th><th>Aktion</th><th>Ziel</th><th>Ergebnis</th></tr>
      </thead>
      <tbody>
        <tr v-for="e in audit" :key="e.id">
          <td>{{ formatDateTime(e.time) }}</td>
          <td>{{ e.actor }}</td>
          <td>{{ e.action }}</td>
          <td>{{ e.target }}</td>
          <td :class="{ bad: e.result === 'error' }">{{ e.result }}</td>
        </tr>
      </tbody>
    </table>
    <pre v-else class="log" data-test="log-text">{{ text || (loading ? '' : 'Keine Einträge.') }}</pre>

    <h3 class="sub">Support-Paket</h3>
    <p class="soft">
      Sammelt Systeminformationen, die letzten Journale und das Änderungsprotokoll in einer Datei, die du dem Support geben kannst. Passwörter, Zugangsdaten für Mail/ntfy und die Konfigurationsdatenbank sind nicht enthalten.
    </p>
    <a :href="SUPPORT_BUNDLE_URL" class="link btn" data-test="support-bundle" download><i class="pi pi-box" /> Support-Paket erzeugen</a>
  </div>
</template>

<style scoped>
.card {
  padding: 20px 22px;
  max-width: 980px;
}
h3 {
  margin: 0 0 12px;
  font-size: 1rem;
}
.sub {
  margin-top: 26px;
}
.bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
}
.link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--p-primary-color, #6366f1);
  text-decoration: none;
  font-size: 0.9rem;
}
.btn {
  padding: 8px 14px;
  border: 1px solid currentColor;
  border-radius: 10px;
}
.soft {
  margin: 0 0 14px;
  color: var(--bn-text-soft);
  font-size: 0.875rem;
}
.log {
  margin: 0;
  max-height: 460px;
  overflow: auto;
  padding: 12px;
  border-radius: 10px;
  background: rgba(0, 0, 0, 0.35);
  color: #e5e7eb;
  font: 0.78rem/1.45 ui-monospace, SFMono-Regular, Menlo, monospace;
  white-space: pre-wrap;
  word-break: break-all;
}
.table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.82rem;
}
.table th {
  text-align: left;
  color: var(--bn-text-soft);
  padding: 6px 8px;
}
.table td {
  padding: 5px 8px;
  border-top: 1px solid rgba(128, 128, 128, 0.2);
}
.bad {
  color: #ef4444;
}
</style>
