<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import Button from 'primevue/button'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { useUpdatesStore } from '@/stores/updates'

const store = useUpdatesStore()
const toast = useToast()
const confirm = ref(false)

onMounted(() => store.load())

// Nach einem Update von BetterNAS selbst läuft die Oberfläche in der alten Version: neu laden.
watch(
  () => store.reloadNeeded,
  (v) => v && setTimeout(() => window.location.reload(), 1500),
)

async function check() {
  try {
    await store.check()
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Suche fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  }
}

async function install() {
  confirm.value = false
  try {
    await store.install()
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Installation nicht gestartet', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  }
}

const when = (iso: string) => (iso ? new Date(iso).toLocaleString('de-DE') : '')
</script>

<template>
  <div class="toolbar">
    <Button
      label="Nach Updates suchen"
      icon="pi pi-refresh"
      outlined
      data-test="updates-check"
      :loading="store.checking"
      :disabled="store.installing"
      @click="check"
    />
    <Button
      label="Updates installieren"
      icon="pi pi-download"
      data-test="updates-install"
      :disabled="!store.overview.packages.length || store.installing"
      @click="confirm = true"
    />
  </div>
  <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>

  <Message v-if="store.restarting" severity="info" :closable="false" data-test="updates-restarting">
    BetterNAS startet neu … Die Seite verbindet sich automatisch wieder.
  </Message>
  <Message v-else-if="store.overview.run.state === 'running'" severity="info" :closable="false" data-test="updates-running">
    Updates werden installiert. Bitte das NAS nicht ausschalten.
  </Message>
  <Message v-else-if="store.overview.run.state === 'succeeded'" severity="success" :closable="false" data-test="updates-done">
    Die Updates wurden erfolgreich installiert.
  </Message>
  <Message v-else-if="store.overview.run.state === 'failed'" severity="error" :closable="false" data-test="updates-failed">
    Die Installation ist fehlgeschlagen. Details stehen im Protokoll.
  </Message>

  <p v-if="store.overview.checkedAt" class="sub">Zuletzt geprüft: {{ when(store.overview.checkedAt) }}</p>
  <p v-else-if="!store.loading" class="sub">Noch nicht nach Updates gesucht.</p>

  <p v-if="store.overview.checkedAt && !store.overview.packages.length && store.overview.run.state === 'none'" class="ok">
    <i class="pi pi-check-circle" /> Alles auf dem neuesten Stand.
  </p>
  <div v-if="store.overview.packages.length" class="table-wrap">
    <table class="pkgs">
      <thead>
        <tr><th>Paket</th><th>Installiert</th><th>Neu</th></tr>
      </thead>
      <tbody>
        <tr v-for="p in store.overview.packages" :key="p.name">
          <td>{{ p.name }}</td>
          <td>{{ p.current || '—' }}</td>
          <td>{{ p.candidate }}</td>
        </tr>
      </tbody>
    </table>
  </div>

  <template v-if="store.overview.run.log">
    <h3>Protokoll</h3>
    <pre class="log">{{ store.overview.run.log }}</pre>
  </template>

  <ConfirmDialog
    v-model:visible="confirm"
    header="Updates installieren"
    message="Die Updates werden jetzt installiert. Dabei kann BetterNAS kurz nicht erreichbar sein. Laufende Übertragungen können abbrechen."
    confirm-label="Installieren"
    @confirm="install"
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
}
.ok {
  display: flex;
  align-items: center;
  gap: 0.7rem;
  padding: 1.4rem 1.6rem;
  border-radius: var(--bn-radius);
  background: linear-gradient(135deg, rgb(16 185 129 / 18%), rgb(34 211 238 / 12%));
  border: 1px solid rgb(16 185 129 / 35%);
  color: var(--p-green-500);
  font-weight: 650;
}
.ok i {
  font-size: 1.5rem;
}
.table-wrap {
  overflow-x: auto;
}
.pkgs {
  width: 100%;
  border-collapse: collapse;
  background: var(--bn-glass);
  border: 1px solid var(--bn-border);
  border-radius: var(--bn-radius);
  box-shadow: var(--bn-shadow);
  backdrop-filter: var(--bn-blur);
}
th,
td {
  text-align: left;
  padding: 0.5rem 0.9rem;
  border-bottom: 1px solid var(--bn-border-soft);
}
th {
  font-size: 0.8rem;
  color: var(--p-text-muted-color);
}
.log {
  background: var(--bn-glass);
  border: 1px solid var(--bn-border);
  border-radius: var(--bn-radius);
  box-shadow: var(--bn-shadow);
  backdrop-filter: var(--bn-blur);
  padding: 0.8rem;
  max-height: 24rem;
  overflow: auto;
  font-size: 0.8rem;
  font-family: ui-monospace, 'SF Mono', Menlo, monospace;
  white-space: pre-wrap;
}
</style>
