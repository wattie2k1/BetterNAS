<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Card from 'primevue/card'
import ProgressBar from 'primevue/progressbar'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import PoolDialog from '@/components/storage/PoolDialog.vue'
import ConfirmDeleteDialog from '@/components/storage/ConfirmDeleteDialog.vue'
import SnapraidDialog from '@/components/storage/SnapraidDialog.vue'
import SnapraidPanel from '@/components/storage/SnapraidPanel.vue'
import ReplaceDiskDialog from '@/components/storage/ReplaceDiskDialog.vue'
import BtrfsPanel, { type BtrfsAction } from '@/components/storage/BtrfsPanel.vue'
import BtrfsActionDialog, { type BtrfsPayload } from '@/components/storage/BtrfsActionDialog.vue'
import BtrfsScheduleDialog from '@/components/storage/BtrfsScheduleDialog.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { useStorageStore } from '@/stores/storage'
import { POLICIES, profileLabel, type BtrfsScrubSchedule, type PoolInput, type PoolView, type SnapraidInput } from '@/api/storage'
import { formatBytes } from '@/utils/format'

const store = useStorageStore()
const toast = useToast()
const showDialog = ref(false)
const editing = ref<PoolView | null>(null)
const deleting = ref<PoolView | null>(null)
const busy = ref(false)
const snapPool = ref<PoolView | null>(null)
const disabling = ref<PoolView | null>(null)
const replacing = ref<PoolView | null>(null)
const cancelling = ref<PoolView | null>(null)
const refreshKey = ref(0)
const btrfsAction = ref<{ kind: BtrfsAction; pool: PoolView } | null>(null)
const showSchedule = ref(false)
const schedule = ref<BtrfsScrubSchedule>({ interval: 'monthly', hour: 3 })

const isBtrfs = (p: PoolView) => p.type === 'btrfs'
const hasBtrfs = computed(() => store.pools.some(isBtrfs))

onMounted(() => store.refresh())

const diskLabel = (id: string) => {
  const d = store.disks.find((x) => x.id === id)
  return d ? `${d.model || id} (${formatBytes(d.sizeBytes)})${d.state === 'missing' ? ' – fehlt' : ''}` : id
}
const policyLabel = (v: string) => POLICIES.find((p) => p.value === v)?.label ?? v
const used = (p: PoolView) => (p.sizeBytes ? Math.round(((p.sizeBytes - p.freeBytes) / p.sizeBytes) * 100) : 0)

function openNew() {
  editing.value = null
  showDialog.value = true
}
function openEdit(p: PoolView) {
  editing.value = p
  showDialog.value = true
}

async function run(fn: () => Promise<void>, done: string) {
  busy.value = true
  try {
    await fn()
    toast.add({ severity: 'success', summary: done, life: 4000 })
    showDialog.value = false
    deleting.value = null
    snapPool.value = null
    disabling.value = null
    replacing.value = null
    cancelling.value = null
    btrfsAction.value = null
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    refreshKey.value++
    busy.value = false
  }
}

const actionLabel = { sync: 'Synchronisierung abgeschlossen', scrub: 'Prüfung abgeschlossen', diff: 'Vergleich abgeschlossen' }

function runSnapraid(p: PoolView, action: 'sync' | 'scrub' | 'diff', force: boolean) {
  run(() => store.runSnapraid(p.name, action, force), actionLabel[action])
}

function replaceDisk(v: { oldDisk: string; newDisk: string }) {
  run(() => store.replaceDisk(replacing.value!.name, v.oldDisk, v.newDisk), 'Plattenersatz abgeschlossen')
}

function saveSnapraid(input: SnapraidInput) {
  run(() => store.enableSnapraid(snapPool.value!.name, input), 'SnapRAID gespeichert')
}

function save(input: PoolInput) {
  if (editing.value) run(() => store.updatePool(editing.value!.name, input), 'Pool gespeichert')
  else if (input.type === 'btrfs') {
    // Erst die Vorschau (liefert das Bestätigungs-Token für das Löschen der Platten), dann anlegen.
    run(async () => {
      const p = await store.previewPool(input.branches)
      await store.createPool({ ...input, confirmToken: p.confirmToken })
    }, 'btrfs-Pool angelegt')
  } else run(() => store.createPool(input), 'Pool angelegt')
}

/** Startet einen langen Auftrag, schließt den Dialog sofort und meldet das Ergebnis, wenn er fertig ist. */
async function startLong(start: () => Promise<{ done: Promise<void> }>, started: string, done: string) {
  busy.value = true
  try {
    const job = await start()
    btrfsAction.value = null
    toast.add({ severity: 'info', summary: started, life: 5000 })
    job.done
      .then(() => toast.add({ severity: 'success', summary: done, life: 6000 }))
      .catch((e) => toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 12000 }))
      .finally(() => refreshKey.value++)
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = false
  }
}

function openBtrfs(p: PoolView, kind: BtrfsAction) {
  btrfsAction.value = { kind, pool: p }
}

function submitBtrfs(v: BtrfsPayload) {
  const pool = btrfsAction.value!.pool.name
  switch (v.kind) {
    case 'replace':
      return startLong(
        async () => {
          const p = await store.previewPool([v.newDisk])
          return store.btrfsReplace(pool, { diskId: v.newDisk, oldDiskId: v.oldDisk, confirmToken: p.confirmToken, confirmName: v.confirmName })
        },
        'Plattenersatz gestartet',
        'Plattenersatz abgeschlossen',
      )
    case 'add':
      return startLong(
        async () => {
          const p = await store.previewPool([v.newDisk])
          return store.btrfsAddDisk(pool, { diskId: v.newDisk, balance: v.balance, confirmToken: p.confirmToken, confirmName: v.confirmName })
        },
        'Platte wird hinzugefügt',
        'Platte hinzugefügt',
      )
    case 'remove':
      return startLong(() => store.btrfsRemoveDisk(pool, v.disk, v.confirmName), 'Platte wird entfernt', 'Platte entfernt')
    case 'balance':
      return startLong(() => store.btrfsBalance(pool, v.usage), 'Balance gestartet', 'Balance abgeschlossen')
    case 'convert':
      return startLong(() => store.btrfsConvert(pool, v.profile, v.confirmName), 'Schutz wird geändert', 'Schutz geändert')
  }
}

async function openSchedule() {
  try {
    schedule.value = await store.scrubSchedule()
  } catch {
    /* Standardwerte anzeigen */
  }
  showSchedule.value = true
}
function saveSchedule(s: BtrfsScrubSchedule) {
  run(async () => {
    await store.saveScrubSchedule(s)
    showSchedule.value = false
  }, 'Zeitplan gespeichert')
}
</script>

<template>
  <div class="toolbar">
    <Button label="Neuer Pool" icon="pi pi-plus" @click="openNew" />
    <Button v-if="hasBtrfs" data-test="scrub-schedule-open" label="Prüfzeitplan" icon="pi pi-clock" text @click="openSchedule" />
  </div>
  <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>
  <p v-if="!store.pools.length && !store.loading" class="empty">
    Noch keine Speicherpools. Ein Pool fasst mehrere Datenplatten zu einem gemeinsamen Speicherort zusammen.
  </p>
  <div class="grid">
    <Card v-for="p in store.pools" :key="p.name">
      <template #title>
        <div class="title">
          <span><i class="pi pi-box" /> {{ p.name }}</span>
          <span class="tags">
            <Tag v-if="isBtrfs(p)" :value="`btrfs ${profileLabel(p.profile)}`" severity="secondary" />
            <Tag :value="p.mounted ? 'Eingehängt' : 'Nicht eingehängt'" :severity="p.mounted ? 'success' : 'danger'" />
          </span>
        </div>
      </template>
      <template #content>
        <p class="sub">{{ p.path }}</p>
        <template v-if="p.mounted">
          <ProgressBar :value="used(p)" :show-value="false" />
          <p class="sub">{{ formatBytes(p.sizeBytes - p.freeBytes) }} von {{ formatBytes(p.sizeBytes) }} belegt</p>
        </template>
        <ul>
          <li v-for="b in p.branches" :key="b">{{ diskLabel(b) }}</li>
        </ul>
        <p v-if="!isBtrfs(p)" class="sub">Verteilung: {{ policyLabel(p.createPolicy) }} · mind. {{ p.minFree }} frei</p>
        <BtrfsPanel
          v-if="isBtrfs(p)"
          :pool="p"
          @scrub="(a) => run(() => store.btrfsScrub(p.name, a), a === 'start' ? 'Prüfung gestartet' : 'Prüfung abgebrochen')"
          @action="(k) => openBtrfs(p, k)"
          @mount-degraded="run(() => store.btrfsMountDegraded(p.name), 'Pool degradiert eingehängt')"
          @reset-errors="run(() => store.btrfsResetErrors(p.name), 'Fehlerzähler zurückgesetzt')"
        />
        <SnapraidPanel
          v-else
          :pool="p"
          :refresh-key="refreshKey"
          @configure="snapPool = p"
          @run="(action, force) => runSnapraid(p, action, force)"
          @disable="disabling = p"
          @replace="replacing = p"
          @retry="run(() => store.retryRecovery(p.name), 'Wiederherstellung abgeschlossen')"
          @cancel-recovery="cancelling = p"
        />
      </template>
      <template #footer>
        <div class="actions">
          <Button v-if="!isBtrfs(p)" label="Bearbeiten" icon="pi pi-pencil" text @click="openEdit(p)" />
          <Button label="Löschen" icon="pi pi-trash" text severity="danger" @click="deleting = p" />
        </div>
      </template>
    </Card>
  </div>
  <PoolDialog v-model:visible="showDialog" :disks="store.disks" :pools="store.pools" :pool="editing" :busy="busy" @submit="save" />
  <ReplaceDiskDialog
    :visible="!!replacing"
    :pool="replacing"
    :disks="store.disks"
    :busy="busy"
    @update:visible="(v) => !v && (replacing = null)"
    @submit="replaceDisk"
  />
  <ConfirmDialog
    :visible="!!cancelling"
    header="Wiederherstellung abbrechen"
    :message="`Die Wiederherstellung für „${cancelling?.name}“ wird abgebrochen. Dateien der ersetzten Platte, die noch nicht zurückgeholt wurden, bleiben verloren; Synchronisierung und Prüfung laufen danach wieder.`"
    confirm-label="Wiederherstellung abbrechen"
    :busy="busy"
    @update:visible="(v) => !v && (cancelling = null)"
    @confirm="run(() => store.cancelRecovery(cancelling!.name), 'Wiederherstellung abgebrochen')"
  />
  <SnapraidDialog
    :visible="!!snapPool"
    :pool="snapPool"
    :disks="store.disks"
    :busy="busy"
    @update:visible="(v) => !v && (snapPool = null)"
    @submit="saveSnapraid"
  />
  <ConfirmDeleteDialog
    :visible="!!disabling"
    :name="disabling?.name ?? ''"
    header="SnapRAID abschalten"
    :text="`SnapRAID wird für „${disabling?.name}“ abgeschaltet: Zeitpläne entfallen, die Parität wird nicht mehr aktualisiert. Die Daten bleiben erhalten.`"
    confirm-label="Abschalten"
    :busy="busy"
    @update:visible="(v) => !v && (disabling = null)"
    @confirm="run(() => store.disableSnapraid(disabling!.name), 'SnapRAID abgeschaltet')"
  />
  <ConfirmDeleteDialog
    :visible="!!deleting"
    :name="deleting?.name ?? ''"
    :text="deleting && isBtrfs(deleting) ? `Der btrfs-Pool „${deleting.name}“ wird aufgelöst und seine Platten werden freigegeben. Die Daten sind danach nicht mehr erreichbar, weil sie über alle Platten verteilt liegen.` : undefined"
    :busy="busy"
    @update:visible="(v) => !v && (deleting = null)"
    @confirm="run(() => store.deletePool(deleting!.name, isBtrfs(deleting!) ? deleting!.name : undefined), 'Pool gelöscht')"
  />
  <BtrfsActionDialog
    :visible="!!btrfsAction"
    :kind="btrfsAction?.kind ?? null"
    :pool="btrfsAction?.pool ?? null"
    :disks="store.disks"
    :busy="busy"
    @update:visible="(v) => !v && (btrfsAction = null)"
    @submit="submitBtrfs"
  />
  <BtrfsScheduleDialog v-model:visible="showSchedule" :schedule="schedule" :busy="busy" @submit="saveSchedule" />
</template>

<style scoped>
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 1rem;
}
.tags {
  display: flex;
  gap: 0.35rem;
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
ul {
  padding-left: 1.2rem;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.25rem;
}
</style>
