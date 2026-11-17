<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Menu from 'primevue/menu'
import { useToast } from 'primevue/usetoast'
import LogsDialog from '@/components/docker/LogsDialog.vue'
import StackDialog, { type SubmitMode } from '@/components/docker/StackDialog.vue'
import { useDockerStore } from '@/stores/docker'
import { useVmsStore } from '@/stores/vms'
import type { ContainerAction, ContainerInfo, StackCheck, StackFiles } from '@/api/docker'
import type { VmAction, VmInfo } from '@/api/vms'

const docker = useDockerStore()
const vms = useVmsStore()
const toast = useToast()
const menu = ref<InstanceType<typeof Menu>>()
const items = ref<Record<string, unknown>[]>([])
const busy = ref(false)
const logsFor = ref<ContainerInfo | null>(null)
const showYaml = ref(false)
const yaml = ref<StackFiles | null>(null)
const check = ref<StackCheck | null>(null)

const containers = computed(() => [...docker.containers].sort((a, b) => Number(b.state === 'running') - Number(a.state === 'running') || a.name.localeCompare(b.name)))
const machines = computed(() => [...vms.vms].sort((a, b) => Number(b.state === 'running') - Number(a.state === 'running') || a.name.localeCompare(b.name)))
const stackNames = computed(() => docker.stacks.map((s) => s.name))

onMounted(() => {
  void docker.refresh()
  void vms.refresh()
})

const fail = (e: unknown) =>
  toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })

async function run(fn: () => Promise<unknown>, ok: string, detail: string) {
  busy.value = true
  try {
    await fn()
    toast.add({ severity: 'success', summary: ok, detail, life: 3000 })
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

const containerDone: Record<string, string> = { start: 'Container gestartet', stop: 'Container gestoppt', restart: 'Container neu gestartet' }
const containerAct = (c: ContainerInfo, a: ContainerAction) => run(() => docker.containerAction(c.id, a), containerDone[a], c.name)
const vmDone: Record<string, string> = { start: 'VM gestartet', shutdown: 'Herunterfahren angefordert', reboot: 'Neustart angefordert', destroy: 'VM hart ausgeschaltet' }
const vmAct = (v: VmInfo, a: VmAction) => run(() => vms.act(v.name, a), vmDone[a], v.name)

async function editYaml(c: ContainerInfo) {
  try {
    yaml.value = await docker.load(c.stack)
    check.value = null
    showYaml.value = true
  } catch (e) {
    fail(e)
  }
}

async function submitYaml(files: StackFiles, mode: SubmitMode) {
  busy.value = true
  try {
    await docker.update(files)
    yaml.value = files
    if (mode === 'check') {
      check.value = await docker.validate(files.name)
      return
    }
    showYaml.value = false
    toast.add({ severity: 'success', summary: 'Stack gespeichert', detail: files.name, life: 3000 })
    if (mode === 'start') await docker.run(files.name, 'up')
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

function openContainer(e: Event, c: ContainerInfo) {
  const running = c.state === 'running'
  items.value = [
    { label: c.name, disabled: true },
    { label: 'YAML bearbeiten', icon: 'pi pi-file-edit', disabled: !c.stack, command: () => editYaml(c) },
    { label: 'Logs', icon: 'pi pi-align-left', command: () => (logsFor.value = c) },
    { separator: true },
    { label: 'Starten', icon: 'pi pi-play', disabled: running, command: () => containerAct(c, 'start') },
    { label: 'Stoppen', icon: 'pi pi-stop', disabled: !running, command: () => containerAct(c, 'stop') },
    { label: 'Neu starten', icon: 'pi pi-refresh', command: () => containerAct(c, 'restart') },
  ]
  menu.value?.toggle(e)
}

function openVm(e: Event, v: VmInfo) {
  const running = v.state === 'running'
  items.value = [
    { label: v.name, disabled: true },
    { label: 'Starten', icon: 'pi pi-play', disabled: running, command: () => vmAct(v, 'start') },
    { label: 'Herunterfahren', icon: 'pi pi-power-off', disabled: !running, command: () => vmAct(v, 'shutdown') },
    { label: 'Neu starten', icon: 'pi pi-refresh', disabled: !running, command: () => vmAct(v, 'reboot') },
    { label: 'Hart ausschalten', icon: 'pi pi-stop', disabled: !running, command: () => vmAct(v, 'destroy') },
    { separator: true },
    { label: 'Alle VMs', icon: 'pi pi-desktop', url: '/vms' },
  ]
  menu.value?.toggle(e)
}

const vmLabel: Record<string, string> = { running: 'läuft', shutoff: 'aus', paused: 'pausiert', shuttingdown: 'fährt herunter', crashed: 'abgestürzt', other: 'unbekannt' }
</script>

<template>
  <template v-if="containers.length || machines.length">
    <h3 class="section">Container &amp; virtuelle Maschinen</h3>
    <div class="apps" data-test="quick-tiles">
      <button
        v-for="c in containers"
        :key="`c-${c.id}`"
        type="button"
        class="app glass"
        :data-test="`quick-container-${c.name}`"
        :aria-label="`Container ${c.name}: Aktionen`"
        @click="openContainer($event, c)"
      >
        <span class="app-icon" :class="{ off: c.state !== 'running' }" :style="c.state === 'running' ? { background: 'linear-gradient(135deg, #2563eb, #60a5fa)' } : {}"><i class="pi pi-box" /></span>
        <span class="app-text">
          <strong>{{ c.name }}</strong>
          <small>{{ c.state === 'running' ? 'running' : 'stopped' }}<template v-if="c.status"> · {{ c.status }}</template></small>
        </span>
      </button>
      <button
        v-for="v in machines"
        :key="`v-${v.uuid}`"
        type="button"
        class="app glass"
        :data-test="`quick-vm-${v.name}`"
        :aria-label="`VM ${v.name}: Aktionen`"
        @click="openVm($event, v)"
      >
        <span class="app-icon" :class="{ off: v.state !== 'running' }" :style="v.state === 'running' ? { background: 'linear-gradient(135deg, #f97316, #fb923c)' } : {}"><i class="pi pi-desktop" /></span>
        <span class="app-text">
          <strong>{{ v.name }}</strong>
          <small>VM · {{ vmLabel[v.state] ?? v.state }}</small>
        </span>
      </button>
    </div>
  </template>
  <Menu ref="menu" :model="items" popup append-to="body" />
  <LogsDialog :visible="!!logsFor" :container="logsFor" @update:visible="(v) => !v && (logsFor = null)" />
  <StackDialog v-model:visible="showYaml" :stack="yaml" :existing="stackNames" :busy="busy" :check="check" @submit="submitYaml" />
</template>

<style scoped>
.section {
  margin: 30px 0 12px;
  font-size: 1rem;
  color: var(--bn-text-soft);
  font-weight: 650;
}
.apps {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
  gap: 14px;
}
.app {
  all: unset;
  box-sizing: border-box;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 18px;
  color: var(--p-text-color);
  transition: transform 0.18s ease, background 0.18s ease;
}
.app:hover {
  transform: translateY(-3px);
  background: var(--bn-tile-hover);
}
.app:focus-visible {
  outline: 2px solid var(--bn-accent-1);
}
.app-icon {
  width: 50px;
  height: 50px;
  flex: none;
  border-radius: 16px;
  display: grid;
  place-items: center;
  color: #fff;
  font-size: 1.3rem;
  background: #64748b;
  box-shadow: 0 8px 20px -8px rgb(0 0 0 / 50%);
}
.app-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.app-text strong,
.app-text small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.app-text small {
  color: var(--bn-text-soft);
}
</style>
