<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import Button from 'primevue/button'
import Checkbox from 'primevue/checkbox'
import Dialog from 'primevue/dialog'
import Menu from 'primevue/menu'
import Message from 'primevue/message'
import Tag from 'primevue/tag'
import ToggleSwitch from 'primevue/toggleswitch'
import { useToast } from 'primevue/usetoast'
import ConsoleDialog from '@/components/vms/ConsoleDialog.vue'
import IsoDialog from '@/components/vms/IsoDialog.vue'
import BridgeDialog from '@/components/vms/BridgeDialog.vue'
import SnapshotsDialog from '@/components/vms/SnapshotsDialog.vue'
import VmWizard from '@/components/vms/VmWizard.vue'
import InputText from 'primevue/inputtext'
import { useVmsStore } from '@/stores/vms'
import { VM_NAME_RE, type VmAction, type VmInfo } from '@/api/vms'

const store = useVmsStore()
const toast = useToast()
const busy = ref<string | null>(null)
const removing = ref<VmInfo | null>(null)
const deleteDisks = ref(false)
const wizard = ref(false)
const isoDialog = ref(false)
const bridgeDialog = ref(false)
const consoleFor = ref<string | null>(null)
const snapshotsFor = ref<string | null>(null)
const cloning = ref<VmInfo | null>(null)
const cloneName = ref('')
const cloneError = ref('')
const menu = ref<InstanceType<typeof Menu> | null>(null)
const menuFor = ref<VmInfo | null>(null)
const down = computed(() => store.status && (!store.status.installed || !store.status.active))

onMounted(async () => {
  await store.refresh()
  store.startPolling()
})
onUnmounted(() => store.stopPolling())

const stateLabel: Record<string, string> = {
  running: 'Läuft',
  shutoff: 'Aus',
  paused: 'Pausiert',
  shuttingdown: 'Fährt herunter',
  crashed: 'Abgestürzt',
  other: 'Unbekannt',
}
const severity = (s: string) => (s === 'running' ? 'success' : s === 'shutoff' ? 'secondary' : s === 'crashed' ? 'danger' : 'warn')
const gib = (mib: number) => (mib % 1024 === 0 ? `${mib / 1024} GiB` : `${mib} MiB`)

const done: Record<VmAction, string> = {
  start: 'VM gestartet',
  shutdown: 'Herunterfahren angefordert',
  destroy: 'VM hart ausgeschaltet',
  reboot: 'Neustart angefordert',
  autostart: 'Autostart aktiviert',
  noautostart: 'Autostart deaktiviert',
}

async function act(v: VmInfo, action: VmAction) {
  busy.value = v.name
  try {
    await store.act(v.name, action)
    toast.add({ severity: 'success', summary: done[action], detail: v.name, life: 3000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = null
  }
}

async function confirmRemove() {
  const v = removing.value
  if (!v) return
  busy.value = v.name
  try {
    await store.remove(v.name, deleteDisks.value)
    toast.add({ severity: 'success', summary: 'VM gelöscht', detail: v.name, life: 3000 })
    removing.value = null
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = null
  }
}

function askRemove(v: VmInfo) {
  deleteDisks.value = false
  removing.value = v
}

function askClone(v: VmInfo) {
  cloneName.value = `${v.name}-kopie`.slice(0, 40)
  cloneError.value = ''
  cloning.value = v
}

async function confirmClone() {
  const v = cloning.value
  if (!v) return
  busy.value = v.name
  cloneError.value = ''
  try {
    await store.clone(v.name, cloneName.value)
    toast.add({ severity: 'success', summary: 'VM geklont', detail: `${v.name} → ${cloneName.value}`, life: 3000 })
    cloning.value = null
  } catch (e) {
    cloneError.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = null
  }
}

function items(v: VmInfo) {
  const on = v.state === 'running'
  return [
    { label: 'Starten', icon: 'pi pi-play', disabled: on, command: () => act(v, 'start') },
    { label: 'Herunterfahren', icon: 'pi pi-power-off', disabled: !on, command: () => act(v, 'shutdown') },
    { label: 'Neu starten', icon: 'pi pi-refresh', disabled: !on, command: () => act(v, 'reboot') },
    { label: 'Hart ausschalten', icon: 'pi pi-stop', disabled: v.state === 'shutoff', command: () => act(v, 'destroy') },
    { separator: true },
    { label: 'Konsole', icon: 'pi pi-desktop', disabled: v.state !== 'running' && v.state !== 'paused', command: () => (consoleFor.value = v.name) },
    { label: 'Snapshots', icon: 'pi pi-camera', command: () => (snapshotsFor.value = v.name) },
    { label: 'Klonen', icon: 'pi pi-clone', disabled: v.state !== 'shutoff', command: () => askClone(v) },
    { separator: true },
    { label: 'Löschen', icon: 'pi pi-trash', disabled: v.state !== 'shutoff', command: () => askRemove(v) },
  ]
}

function openMenu(e: Event, v: VmInfo) {
  menuFor.value = v
  menu.value?.toggle(e)
}
</script>

<template>
  <div class="bar">
    <span class="spacer" />
    <Button data-test="vm-bridge" label="Bridge" icon="pi pi-sitemap" text :disabled="!!down" @click="bridgeDialog = true" />
    <Button data-test="vm-isos" label="ISO-Dateien" icon="pi pi-upload" text :disabled="!!down" @click="isoDialog = true" />
    <Button data-test="vm-new" label="Neue VM" icon="pi pi-plus" :disabled="!!down" @click="wizard = true" />
  </div>
  <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>
  <Message v-if="down" severity="warn" :closable="false" data-test="vm-down">
    {{ store.status?.installed ? 'Der libvirt-Dienst läuft nicht.' : 'libvirt ist nicht installiert.' }}
  </Message>
  <Message v-else-if="store.status && !store.status.kvm" severity="info" :closable="false" data-test="vm-nokvm">
    Keine Hardware-Beschleunigung (KVM) verfügbar – VMs laufen sehr langsam.
  </Message>
  <p v-if="!down && !store.vms.length && !store.loading" class="empty">Noch keine virtuelle Maschine.</p>

  <div v-if="store.vms.length" class="grid">
    <article v-for="v in store.vms" :key="v.name" class="vm glass" :data-test="`vm-${v.name}`">
      <header>
        <strong>{{ v.name }}</strong>
        <Tag :value="stateLabel[v.state] ?? v.state" :severity="severity(v.state)" />
      </header>
      <p class="sub">{{ v.vcpus }} vCPU · {{ gib(v.memoryMiB) }}</p>
      <footer>
        <label class="auto">
          <ToggleSwitch
            :model-value="v.autostart"
            :disabled="busy === v.name"
            :aria-label="`Autostart ${v.name}`"
            data-test="vm-autostart"
            @update:model-value="act(v, v.autostart ? 'noautostart' : 'autostart')"
          />
          <span>Autostart</span>
        </label>
        <span class="spacer" />
        <Button
          v-if="v.state === 'running' || v.state === 'paused'"
          icon="pi pi-desktop"
          text
          rounded
          aria-label="Konsole"
          title="Konsole"
          data-test="vm-console"
          @click="consoleFor = v.name"
        />
        <Button
          v-if="v.state === 'shutoff'"
          icon="pi pi-play"
          text
          rounded
          aria-label="Starten"
          title="Starten"
          data-test="vm-start"
          :disabled="busy === v.name"
          @click="act(v, 'start')"
        />
        <Button
          v-else-if="v.state === 'running'"
          icon="pi pi-power-off"
          text
          rounded
          aria-label="Herunterfahren"
          title="Herunterfahren"
          data-test="vm-shutdown"
          :disabled="busy === v.name"
          @click="act(v, 'shutdown')"
        />
        <Button icon="pi pi-ellipsis-v" text rounded aria-label="Weitere Aktionen" data-test="vm-menu" @click="openMenu($event, v)" />
      </footer>
    </article>
  </div>
  <Menu ref="menu" :model="menuFor ? items(menuFor) : []" popup append-to="self" />

  <VmWizard v-model:visible="wizard" />
  <IsoDialog v-model:visible="isoDialog" />
  <BridgeDialog v-model:visible="bridgeDialog" />
  <ConsoleDialog :visible="!!consoleFor" :name="consoleFor" @update:visible="(v) => !v && (consoleFor = null)" />
  <SnapshotsDialog :visible="!!snapshotsFor" :name="snapshotsFor" @update:visible="(v) => !v && (snapshotsFor = null)" />
  <Dialog :visible="!!cloning" modal append-to="self" header="VM klonen" :style="{ width: 'min(480px, 95vw)' }" @update:visible="(v) => !v && (cloning = null)">
    <p>Die ausgeschaltete VM „{{ cloning?.name }}“ wird mit allen Festplatten kopiert.</p>
    <label for="vm-clone-name">Name der Kopie</label>
    <InputText id="vm-clone-name" v-model="cloneName" class="full" autocomplete="off" data-test="clone-name" :invalid="!!cloneName && !VM_NAME_RE.test(cloneName)" />
    <Message v-if="cloneError" severity="error" :closable="false" data-test="clone-error">{{ cloneError }}</Message>
    <template #footer>
      <Button label="Abbrechen" text @click="cloning = null" />
      <Button data-test="clone-submit" label="Klonen" :disabled="!VM_NAME_RE.test(cloneName) || busy === cloning?.name" @click="confirmClone" />
    </template>
  </Dialog>
  <Dialog :visible="!!removing" modal append-to="self" header="VM löschen" :style="{ width: 'min(480px, 95vw)' }" @update:visible="(v) => !v && (removing = null)">
    <p>Die virtuelle Maschine „{{ removing?.name }}“ wird gelöscht.</p>
    <label class="check">
      <Checkbox v-model="deleteDisks" binary input-id="vm-del-disks" data-test="vm-delete-disks" />
      <span>Festplatten ebenfalls löschen (nicht rückgängig zu machen)</span>
    </label>
    <template #footer>
      <Button label="Abbrechen" text @click="removing = null" />
      <Button data-test="confirm-submit" label="Löschen" severity="danger" :disabled="busy === removing?.name" @click="confirmRemove" />
    </template>
  </Dialog>
</template>

<style scoped>
.bar {
  display: flex;
  margin-bottom: 1rem;
}
.spacer {
  flex: 1;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 18px;
}
.vm {
  padding: 18px 20px;
}
.vm header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem;
}
.vm footer {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  margin-top: 0.75rem;
}
.auto {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.85rem;
  color: var(--bn-text-soft);
}
.sub,
.empty {
  color: var(--bn-text-soft);
  font-size: 0.85rem;
  margin: 0.4rem 0 0;
}
.full {
  width: 100%;
  margin-top: 0.4rem;
}
.check {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-top: 0.75rem;
}
</style>
