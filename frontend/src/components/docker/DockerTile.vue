<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Menu from 'primevue/menu'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import LogsDialog from './LogsDialog.vue'
import StackDialog, { type SubmitMode } from './StackDialog.vue'
import { useDockerStore } from '@/stores/docker'
import type { ContainerAction, ContainerInfo, StackCheck, StackFiles } from '@/api/docker'

const store = useDockerStore()
const toast = useToast()
const menu = ref<InstanceType<typeof Menu>>()
const selected = ref<ContainerInfo | null>(null)
const logsFor = ref<ContainerInfo | null>(null)
const showYaml = ref(false)
const yaml = ref<StackFiles | null>(null)
const check = ref<StackCheck | null>(null)
const busy = ref(false)
const dockerDown = computed(() => store.status && (!store.status.installed || !store.status.active))
const names = computed(() => store.stacks.map((s) => s.name))
const sorted = computed(() => [...store.containers].sort((a, b) => Number(b.state === 'running') - Number(a.state === 'running') || a.name.localeCompare(b.name)))

onMounted(() => store.refresh())

const fail = (e: unknown) =>
  toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
const done: Record<string, string> = { start: 'Container gestartet', stop: 'Container gestoppt', restart: 'Container neu gestartet' }

async function act(c: ContainerInfo, action: ContainerAction) {
  busy.value = true
  try {
    await store.containerAction(c.id, action)
    toast.add({ severity: 'success', summary: done[action], detail: c.name, life: 4000 })
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

async function editYaml(c: ContainerInfo) {
  try {
    yaml.value = await store.load(c.stack)
    check.value = null
    showYaml.value = true
  } catch (e) {
    fail(e)
  }
}

async function submitYaml(files: StackFiles, mode: SubmitMode) {
  busy.value = true
  try {
    await store.update(files)
    yaml.value = files
    if (mode === 'check') {
      check.value = await store.validate(files.name)
      return
    }
    showYaml.value = false
    toast.add({ severity: 'success', summary: 'Stack gespeichert', detail: files.name, life: 4000 })
    if (mode === 'start') await store.run(files.name, 'up')
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

const items = computed(() => {
  const c = selected.value
  if (!c) return []
  const running = c.state === 'running'
  return [
    { label: 'YAML bearbeiten', icon: 'pi pi-file-edit', disabled: !c.stack, command: () => editYaml(c) },
    { label: 'Logs', icon: 'pi pi-align-left', command: () => (logsFor.value = c) },
    { separator: true },
    { label: 'Starten', icon: 'pi pi-play', disabled: running, command: () => act(c, 'start') },
    { label: 'Stoppen', icon: 'pi pi-stop', disabled: !running, command: () => act(c, 'stop') },
    { label: 'Neu starten', icon: 'pi pi-refresh', command: () => act(c, 'restart') },
  ]
})

function open(e: Event, c: ContainerInfo) {
  selected.value = c
  menu.value?.toggle(e)
}
const initial = (c: ContainerInfo) => c.name.slice(0, 1).toUpperCase()
</script>

<template>
  <div data-test="docker-tile-body">
    <Message v-if="dockerDown" severity="warn" :closable="false">
      {{ store.status?.installed ? 'Der Docker-Dienst läuft nicht.' : 'Docker ist nicht installiert.' }}
    </Message>
    <p v-else-if="!sorted.length" class="soft">Keine Container vorhanden.</p>
    <ul v-else class="list">
      <li v-for="c in sorted" :key="c.id" :data-test="`dock-${c.name}`">
        <span class="ico" :class="{ on: c.state === 'running', img: !!store.iconUrlFor(c) }" :data-test="`dock-icon-${c.name}`">
          <img v-if="store.iconUrlFor(c)" :src="store.iconUrlFor(c)!" alt="" loading="lazy" @error="($event.target as HTMLImageElement).style.display = 'none'" />
          <template v-else>{{ initial(c) }}</template>
        </span>
        <span class="txt">
          <strong>{{ c.name }}</strong>
          <small>
            <span class="state" :class="{ on: c.state === 'running' }">{{ c.state === 'running' ? 'running' : 'stopped' }}</span>
            <template v-if="c.status"> · {{ c.status }}</template>
          </small>
        </span>
        <Button icon="pi pi-ellipsis-v" text rounded size="small" :aria-label="`Aktionen für ${c.name}`" :disabled="busy" @click="open($event, c)" />
      </li>
    </ul>
    <Menu ref="menu" :model="items" popup append-to="body" />
    <LogsDialog :visible="!!logsFor" :container="logsFor" @update:visible="(v) => !v && (logsFor = null)" />
    <StackDialog v-model:visible="showYaml" :stack="yaml" :existing="names" :busy="busy" :check="check" @submit="submitYaml" />
  </div>
</template>

<style scoped>
.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 16rem;
  overflow-y: auto;
}
li {
  display: flex;
  align-items: center;
  gap: 10px;
}
.ico {
  width: 36px;
  height: 36px;
  flex: none;
  border-radius: 11px;
  display: grid;
  place-items: center;
  color: #fff;
  font-weight: 700;
  background: #64748b;
}
.ico.on {
  background: linear-gradient(135deg, #2563eb, #60a5fa);
}
.ico.img {
  background: var(--bn-glass-strong);
  border: 1px solid var(--bn-border-soft);
  overflow: hidden;
}
.ico img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  padding: 4px;
}
.ico.img:not(.on) img {
  filter: grayscale(1);
  opacity: 0.6;
}
.txt {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.txt strong,
.txt small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.txt small {
  color: var(--bn-text-soft);
}
.state.on {
  color: var(--bn-ok);
}
.soft {
  color: var(--bn-text-soft);
  margin: 0;
  font-size: 0.85rem;
}
</style>
