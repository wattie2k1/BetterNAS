<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import LogsDialog from '@/components/docker/LogsDialog.vue'
import ShellDialog from '@/components/docker/ShellDialog.vue'
import { formatBytes } from '@/utils/format'
import { useDockerStore } from '@/stores/docker'
import type { ContainerAction, ContainerInfo } from '@/api/docker'

const store = useDockerStore()
const toast = useToast()
const busy = ref<string | null>(null)
const removing = ref<ContainerInfo | null>(null)
const dockerDown = computed(() => store.status && (!store.status.installed || !store.status.active))

const logsFor = ref<ContainerInfo | null>(null)
const shellFor = ref<ContainerInfo | null>(null)

onMounted(async () => {
  await store.refresh()
  store.startStats()
})
onUnmounted(() => store.stopStats())

const cpu = (id: string) => store.stats[id]?.cpu
const mem = (id: string) => {
  const s = store.stats[id]
  return s ? `${formatBytes(s.memUsage)} / ${formatBytes(s.memLimit)}` : ''
}

const severity = (state: string) => (state === 'running' ? 'success' : state === 'exited' || state === 'created' ? 'secondary' : 'warn')
const healthLabel: Record<string, string> = { healthy: 'Gesund', unhealthy: 'Fehlerhaft', starting: 'Prüfung läuft' }

const stateLabel: Record<string, string> = {
  running: 'Läuft',
  exited: 'Gestoppt',
  created: 'Erstellt',
  paused: 'Pausiert',
  restarting: 'Startet neu',
  dead: 'Defekt',
}

const done: Record<ContainerAction, string> = {
  start: 'Container gestartet',
  stop: 'Container gestoppt',
  restart: 'Container neu gestartet',
  remove: 'Container entfernt',
}

async function act(c: ContainerInfo, action: ContainerAction) {
  busy.value = c.id
  try {
    await store.containerAction(c.id, action)
    toast.add({ severity: 'success', summary: done[action], detail: c.name, life: 4000 })
    removing.value = null
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = null
  }
}
</script>

<template>
  <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>
  <Message v-if="dockerDown" severity="warn" :closable="false" data-test="docker-down">
    {{ store.status?.installed ? 'Der Docker-Dienst läuft nicht.' : 'Docker ist nicht installiert.' }}
  </Message>
  <p v-else-if="!store.containers.length && !store.loading" class="empty">Keine Container vorhanden.</p>
  <div v-if="store.containers.length" class="table-wrap">
    <table class="containers">
      <thead>
        <tr>
          <th>Name</th>
          <th>Image</th>
          <th>Zustand</th>
          <th>Auslastung</th>
          <th>Ports</th>
          <th />
        </tr>
      </thead>
      <tbody>
        <tr v-for="c in store.containers" :key="c.id" :data-test="`container-${c.name}`">
          <td>
            <div class="who">
              <span class="avatar-sm ico" :class="{ img: !!store.iconUrlFor(c), on: c.state === 'running' }" :data-test="`container-icon-${c.name}`" aria-hidden="true">
                <img v-if="store.iconUrlFor(c)" :src="store.iconUrlFor(c)!" alt="" loading="lazy" @error="($event.target as HTMLImageElement).style.display = 'none'" />
                <template v-else>{{ c.name.slice(0, 1).toUpperCase() }}</template>
              </span>
              <div>
                <strong>{{ c.name }}</strong>
                <div v-if="c.stack" class="sub">Stack {{ c.stack }}</div>
              </div>
            </div>
          </td>
          <td>{{ c.image }}</td>
          <td>
            <Tag :value="stateLabel[c.state] ?? c.state" :severity="severity(c.state)" />
            <Tag
              v-if="c.health"
              :data-test="`health-${c.health}`"
              :value="healthLabel[c.health]"
              :severity="c.health === 'healthy' ? 'success' : c.health === 'unhealthy' ? 'danger' : 'secondary'"
              class="health"
            />
            <div class="sub">{{ c.status }}</div>
          </td>
          <td data-test="usage">
            <template v-if="c.state === 'running' && store.stats[c.id]">
              <div>CPU {{ cpu(c.id)?.toFixed(1) }} %</div>
              <div class="sub">RAM {{ mem(c.id) }}</div>
            </template>
            <span v-else class="sub">—</span>
          </td>
          <td>
            <div v-for="p in c.ports" :key="p" class="sub">{{ p }}</div>
            <span v-if="!c.ports.length" class="sub">—</span>
          </td>
          <td class="actions">
            <Button v-if="c.state === 'running'" icon="pi pi-align-left" text rounded aria-label="Logs" title="Logs" @click="logsFor = c" />
            <Button v-if="c.state === 'running'" icon="pi pi-code" text rounded aria-label="Shell" title="Shell öffnen" @click="shellFor = c" />
            <Button v-if="c.state !== 'running'" icon="pi pi-play" text rounded aria-label="Starten" title="Starten" :disabled="busy === c.id" @click="act(c, 'start')" />
            <Button v-else icon="pi pi-stop" text rounded aria-label="Stoppen" title="Stoppen" :disabled="busy === c.id" @click="act(c, 'stop')" />
            <Button icon="pi pi-refresh" text rounded aria-label="Neu starten" title="Neu starten" :disabled="busy === c.id" @click="act(c, 'restart')" />
            <Button icon="pi pi-trash" text rounded severity="danger" aria-label="Entfernen" title="Entfernen" :disabled="busy === c.id" @click="removing = c" />
          </td>
        </tr>
      </tbody>
    </table>
  </div>
  <LogsDialog :visible="!!logsFor" :container="logsFor" @update:visible="(v) => !v && (logsFor = null)" />
  <ShellDialog :visible="!!shellFor" :container="shellFor" @update:visible="(v) => !v && (shellFor = null)" />
  <ConfirmDialog
    :visible="!!removing"
    header="Container entfernen"
    :message="`Der Container „${removing?.name}“ wird gestoppt und entfernt. Daten in Volumes und Ordnern bleiben erhalten. Gehört er zu einem Stack, wird er beim nächsten Start des Stacks neu erstellt.`"
    confirm-label="Entfernen"
    :busy="busy === removing?.id"
    @update:visible="(v) => !v && (removing = null)"
    @confirm="act(removing!, 'remove')"
  />
</template>

<style scoped>
.health {
  margin-left: 0.4rem;
}
.who {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.containers {
  width: 100%;
  border-collapse: collapse;
}
.sub {
  font-size: 0.8rem;
  color: var(--bn-text-soft);
  margin-top: 0.15rem;
}
.empty {
  color: var(--bn-text-soft);
  font-size: 0.875rem;
}
.actions {
  white-space: nowrap;
  text-align: right;
}
.ico.avatar-sm {
  width: 40px;
  height: 40px;
  border-radius: 12px;
  background: #64748b;
  box-shadow: 0 6px 14px -6px rgb(0 0 0 / 25%);
}
.ico.avatar-sm img {
  padding: 6px;
}
.ico.avatar-sm.on {
  background: linear-gradient(135deg, #2563eb, #60a5fa);
}
.ico.avatar-sm.img {
  background: var(--bn-glass-strong);
  border: 1px solid var(--bn-border-soft);
  overflow: hidden;
}
.ico.avatar-sm.img:not(.on) img {
  filter: grayscale(1);
  opacity: 0.6;
}
</style>
