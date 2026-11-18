<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Message from 'primevue/message'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { servicesApi, type ServiceAction, type ServiceState } from '@/api/system'

const toast = useToast()
const list = ref<ServiceState[]>([])
const error = ref('')
const busy = ref<string | null>(null)
const pending = ref<{ s: ServiceState; action: ServiceAction } | null>(null)

/** Bei diesen Diensten reißt Stoppen/Neustart laufende Arbeit mit. */
const careful = new Set(['docker', 'libvirtd', 'ssh', 'smbd', 'nfs-server'])
const verbs: Record<ServiceAction, string> = { start: 'gestartet', stop: 'gestoppt', restart: 'neu gestartet' }
const message = computed(() => {
  const p = pending.value
  if (!p) return ''
  const what: Record<string, string> = {
    docker: 'Alle laufenden Container werden dabei beendet und – sofern sie einen Neustart-Modus haben – wieder gestartet (außer „Live-Restore“ ist aktiv).',
    libvirtd: 'Laufende virtuelle Maschinen laufen weiter, lassen sich aber kurz nicht verwalten.',
    ssh: 'Bestehende SSH-Sitzungen bleiben offen; neue Anmeldungen sind danach ' + (p.action === 'stop' ? 'nicht mehr ' : '') + 'möglich.',
    smbd: 'Verbundene Windows-/Mac-Clients verlieren kurz die Verbindung, laufende Übertragungen brechen ab.',
    'nfs-server': 'Verbundene NFS-Clients verlieren kurz die Verbindung.',
  }
  return `„${p.s.label}“ wird ${verbs[p.action]}. ${what[p.s.unit] ?? ''}`
})

async function load() {
  try {
    list.value = await servicesApi.list()
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}
onMounted(load)

function ask(s: ServiceState, action: ServiceAction) {
  if (action !== 'start' && careful.has(s.unit)) pending.value = { s, action }
  else void run(s, action)
}

async function run(s: ServiceState, action: ServiceAction) {
  busy.value = s.unit
  try {
    await servicesApi.act(s.unit, action)
    toast.add({ severity: 'success', summary: `${s.label} ${verbs[action]}`, life: 4000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = null
    pending.value = null
    await load()
  }
}
</script>

<template>
  <div class="card" data-test="settings-services">
    <h2>Dienste</h2>
    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
    <ul class="list">
      <li v-for="s in list" :key="s.unit" :data-test="`svc-${s.unit}`">
        <span class="txt">
          <strong>{{ s.label }}</strong>
          <small class="soft">{{ s.unit }}</small>
        </span>
        <Tag v-if="!s.installed" value="Nicht installiert" severity="secondary" />
        <Tag v-else :value="s.active ? 'Läuft' : 'Gestoppt'" :severity="s.active ? 'success' : 'secondary'" />
        <span class="btns">
          <Button v-if="!s.active" icon="pi pi-play" text rounded :aria-label="`${s.label} starten`" title="Starten" :disabled="!s.installed || busy === s.unit" @click="ask(s, 'start')" />
          <Button v-else icon="pi pi-stop" text rounded :aria-label="`${s.label} stoppen`" title="Stoppen" :disabled="busy === s.unit" @click="ask(s, 'stop')" />
          <Button icon="pi pi-refresh" text rounded :aria-label="`${s.label} neu starten`" title="Neu starten" :disabled="!s.installed || busy === s.unit" @click="ask(s, 'restart')" />
        </span>
      </li>
    </ul>
    <ConfirmDialog
      :visible="!!pending"
      :header="pending?.action === 'stop' ? 'Dienst stoppen' : 'Dienst neu starten'"
      :message="message"
      :confirm-label="pending?.action === 'stop' ? 'Stoppen' : 'Neu starten'"
      :busy="busy === pending?.s.unit"
      @update:visible="(v) => !v && (pending = null)"
      @confirm="pending && run(pending.s, pending.action)"
    />
  </div>
</template>

<style scoped>
.card {
  max-width: 760px;
}
.list {
  list-style: none;
  margin: 0;
  padding: 0;
}
li {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 0;
  border-bottom: 1px solid var(--bn-border-soft);
}
li:last-child {
  border-bottom: 0;
}
.txt {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.soft {
  color: var(--bn-text-soft);
}
.btns {
  display: flex;
}
</style>
