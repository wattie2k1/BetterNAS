<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, computed, watch } from 'vue'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import FormatDialog from '@/components/storage/FormatDialog.vue'
import DiskDetailDialog from '@/components/storage/DiskDetailDialog.vue'
import SelfTestScheduleDialog from '@/components/storage/SelfTestScheduleDialog.vue'
import UsageBar from '@/components/storage/UsageBar.vue'
import DiskBulkBar from '@/components/storage/DiskBulkBar.vue'
import { useStorageStore } from '@/stores/storage'
import type { DiskView, FormatPreview, FormatRequest } from '@/api/storage'
import { formatBytes } from '@/utils/format'
import { ioRates, formatRate, type IORate } from '@/utils/diskio'
import { pollWhileVisible } from '@/utils/poll'
import { storageApi, type DiskIOStats } from '@/api/storage'
import { SELFTEST_KIND_LABEL, SPINDOWN_CHOICES, SSD_WEAR_WARN_PERCENT, spindownLabel } from '@/api/storage'

const store = useStorageStore()
const toast = useToast()
const preview = ref<FormatPreview | null>(null)
const showFormat = ref(false)
const busy = ref<string | null>(null)

// Auswahl für Sammelaktionen: nur angeschlossene Platten; verschwundene werden wieder abgewählt.
const selected = ref<Set<string>>(new Set())
const selectable = computed(() => store.disks.filter((d) => d.state === 'online'))
const selectedDisks = computed(() => selectable.value.filter((d) => selected.value.has(d.id)))
const allSelected = computed(() => selectable.value.length > 0 && selectedDisks.value.length === selectable.value.length)
const someSelected = computed(() => selectedDisks.value.length > 0 && !allSelected.value)
function toggle(id: string, on: boolean) {
  const next = new Set(selected.value)
  if (on) next.add(id)
  else next.delete(id)
  selected.value = next
}
function toggleAll(on: boolean) {
  selected.value = on ? new Set(selectable.value.map((d) => d.id)) : new Set()
}
watch(selectable, (list) => {
  const ids = new Set(list.map((d) => d.id))
  const kept = [...selected.value].filter((id) => ids.has(id))
  if (kept.length !== selected.value.size) selected.value = new Set(kept)
})

const detailDisk = ref<DiskView | null>(null)
const showDetail = ref(false)
const showSchedule = ref(false)

async function toggleIdentify(d: DiskView) {
  try {
    if (d.identifying) {
      await store.stopIdentify(d.id)
    } else {
      await store.startIdentify(d.id)
      toast.add({
        severity: 'info',
        summary: 'Platte wird angesprochen',
        detail: `Die Aktivitäts-LED von ${d.model || d.id} (${d.device}) blinkt jetzt 30 Sekunden lang. Die Platte läuft dafür an.`,
        life: 8000,
      })
    }
  } catch (e) {
    fail(e)
  }
}

async function dismissNew(d: DiskView) {
  try {
    await store.dismissNew(d.id)
  } catch (e) {
    fail(e)
  }
}

function openDetail(d: DiskView) {
  detailDisk.value = d
  showDetail.value = true
}

// Läuft irgendwo ein Selbsttest, den Fortschritt alle 15 s nachladen (die Werte kommen aus dem Zwischenspeicher des Servers).
const anyTestRunning = computed(() => store.disks.some((d) => d.smart?.selfTest?.running || d.fsCheck?.running || d.bench?.running))
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void store.refresh()
  timer = setInterval(() => {
    if (anyTestRunning.value) void store.refresh()
  }, 15000)
})
onBeforeUnmount(() => clearInterval(timer))

// Live-I/O: solange der Schalter an ist, alle 2 s die Zähler holen und daraus Raten pro Platte bilden.
const live = ref(false)
const rates = ref<Record<string, IORate>>({})
let prevIO: DiskIOStats | null = null
let stopLive: (() => void) | undefined
async function sampleIO() {
  try {
    const cur = await storageApi.diskIO()
    if (prevIO) rates.value = ioRates(prevIO, cur)
    prevIO = cur
  } catch {
    // eine ausgefallene Messung lässt die letzten Werte stehen
  }
}
watch(live, (on) => {
  stopLive?.()
  stopLive = undefined
  prevIO = null
  rates.value = {}
  if (on) {
    void sampleIO()
    stopLive = pollWhileVisible(() => void sampleIO(), 2000)
  }
})
onBeforeUnmount(() => stopLive?.())
const rateOf = (d: DiskView): IORate | undefined => (d.device ? rates.value[d.device.replace('/dev/', '')] : undefined)

const roleLabel: Record<string, { label: string; severity: string }> = {
  system: { label: 'System', severity: 'secondary' },
  data: { label: 'Datenplatte', severity: 'success' },
  parity: { label: 'Parität', severity: 'info' },
  unassigned: { label: 'Nicht zugewiesen', severity: 'warn' },
}

function fail(e: unknown) {
  toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
}

/** Führt eine Aktion aus; liefert true, wenn sie gelingt (Dialoge bleiben bei Fehlern offen). */
async function act(id: string, fn: () => Promise<void>, done: string): Promise<boolean> {
  busy.value = id
  try {
    await fn()
    toast.add({ severity: 'success', summary: done, life: 4000 })
    return true
  } catch (e) {
    fail(e)
    return false
  } finally {
    busy.value = null
  }
}

async function openFormat(d: DiskView) {
  try {
    preview.value = await store.previewFormat(d.id)
    showFormat.value = true
  } catch (e) {
    fail(e)
  }
}

async function doFormat(req: FormatRequest) {
  const id = preview.value!.diskId
  if (await act(id, () => store.format(id, req), 'Datenträger formatiert')) showFormat.value = false
}

const spinError = ref<Record<string, string>>({})

async function setSpindown(d: DiskView, ev: Event) {
  const el = ev.target as HTMLSelectElement
  const minutes = Number(el.value)
  delete spinError.value[d.id]
  try {
    await store.setSpindown(d.id, minutes)
  } catch (e) {
    spinError.value[d.id] = e instanceof Error ? e.message : String(e)
    el.value = String(d.spindown ?? 0) // Auswahl auf den tatsächlichen Wert zurücksetzen
  }
}

const hasBadSectors = (d: DiskView) => (d.smart?.pendingSectors ?? 0) > 0 || (d.smart?.reallocatedSectors ?? 0) > 0
const canSpin = (d: DiskView) => (d.role === 'data' || d.role === 'parity') && d.state === 'online'

const canImport = (d: DiskView) => d.partitions.some((p) => ['ext4', 'xfs', 'btrfs'].includes(p.fsType))
</script>

<template>
  <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>
  <Message v-for="d in store.newDisks" :key="d.id" severity="info" :closable="false" class="new-disk" :data-test="`new-disk-${d.id}`">
    <div class="new-body">
      <div>
        <strong>Neue Platte erkannt:</strong> {{ d.model || 'Unbekannt' }} ({{ formatBytes(d.sizeBytes) }}, {{ d.device }}).
        <span class="sub">Richte sie ein, damit sie genutzt werden kann.</span>
      </div>
      <div class="new-actions">
        <Button label="Formatieren" size="small" severity="danger" outlined :loading="busy === d.id" :data-test="`new-format-${d.id}`" @click="openFormat(d)" />
        <Button v-if="canImport(d)" label="Übernehmen" size="small" outlined :loading="busy === d.id" :data-test="`new-import-${d.id}`" @click="act(d.id, () => store.importDisk(d.id), 'Datenträger übernommen')" />
        <Button :label="d.identifying ? 'Stopp' : 'Finden'" size="small" text :data-test="`new-identify-${d.id}`" @click="toggleIdentify(d)" />
        <Button label="Ignorieren" size="small" text :data-test="`new-dismiss-${d.id}`" @click="dismissNew(d)" />
      </div>
    </div>
  </Message>
  <p v-if="!store.disks.length && !store.loading" class="empty">Keine Datenträger gefunden.</p>
  <div v-else class="table-wrap">
    <DiskBulkBar v-if="selectedDisks.length" :disks="selectedDisks" @clear="toggleAll(false)" />
    <div class="toolbar">
      <label class="live"><input v-model="live" type="checkbox" data-test="live-toggle" /> Live-I/O</label>
      <Button label="Selbsttests planen" icon="pi pi-calendar" size="small" outlined data-test="open-schedule" @click="showSchedule = true" />
    </div>
    <table class="disks">
      <thead>
        <tr>
          <th class="check">
            <input type="checkbox" :checked="allSelected" :indeterminate="someSelected" aria-label="Alle Platten auswählen" data-test="select-all" @change="toggleAll(($event.target as HTMLInputElement).checked)" />
          </th>
          <th>Datenträger</th>
          <th>Größe</th>
          <th>Rolle</th>
          <th>Zustand</th>
          <th>SMART</th>
          <th>Ruhezustand</th>
          <th />
        </tr>
      </thead>
      <tbody>
        <tr v-for="d in store.disks" :key="d.id" :data-test="`disk-${d.id}`">
          <td class="check">
            <input
              v-if="d.state === 'online'"
              type="checkbox"
              :checked="selected.has(d.id)"
              :aria-label="`${d.model || d.id} auswählen`"
              :data-test="`select-${d.id}`"
              @change="toggle(d.id, ($event.target as HTMLInputElement).checked)"
            />
          </td>
          <td>
            <div class="name">
              <i :class="d.system ? 'pi pi-lock' : d.rotational ? 'pi pi-database' : 'pi pi-bolt'" />
              <strong>{{ d.model || 'Unbekannt' }}</strong>
              <Tag v-if="d.note?.name" :value="d.note.name" severity="info" :data-test="`note-name-${d.id}`" />
            </div>
            <div class="sub">{{ d.device || '—' }} · {{ d.serial || d.id }}</div>
            <div v-if="live && rateOf(d)" class="sub io" :data-test="`io-${d.id}`">
              <i class="pi pi-arrow-down" /> {{ formatRate(rateOf(d)!.readBps, formatBytes) }}
              <i class="pi pi-arrow-up" /> {{ formatRate(rateOf(d)!.writeBps, formatBytes) }}
              · {{ Math.round(rateOf(d)!.busyPct) }} % ausgelastet
            </div>
          </td>
          <td>
            {{ formatBytes(d.sizeBytes) }}<div class="sub">{{ d.rotational ? 'HDD' : 'SSD' }}</div>
            <UsageBar v-if="d.usage" :usage="d.usage" compact class="usage" />
          </td>
          <td>
            <Tag :value="roleLabel[d.role].label" :severity="roleLabel[d.role].severity" />
            <div v-if="d.pool" class="sub">Pool {{ d.pool }}</div>
            <div v-else-if="d.fsType" class="sub">{{ d.fsType }}</div>
          </td>
          <td>
            <Tag v-if="d.state === 'missing'" value="Fehlt" severity="danger" />
            <span v-else-if="!d.manageable && !d.system" class="sub" :title="d.reason">{{ d.reason }}</span>
            <span v-else>Online</span>
          </td>
          <td>
            <template v-if="d.smart">
              <Tag :value="d.smart.passed ? 'OK' : 'Warnung'" :severity="d.smart.passed ? 'success' : 'danger'" />
              <Tag v-if="d.smart.standby" value="schläft" severity="secondary" class="standby" />
              <div v-if="d.smart.temperatureC" class="sub">{{ d.smart.temperatureC }} °C</div>
              <div v-if="d.smart.wear?.percentUsed !== undefined" class="sub" :class="{ warn: d.smart.wear.percentUsed >= SSD_WEAR_WARN_PERCENT }" :data-test="`wear-${d.id}`">
                Verschleiß {{ d.smart.wear.percentUsed }} %
              </div>
              <div v-if="d.smart.selfTest?.running" class="sub" :data-test="`selftest-running-${d.id}`">
                <i class="pi pi-spin pi-spinner" /> Selbsttest läuft ({{ 100 - (d.smart.selfTest.remainingPercent ?? 0) }} %)
              </div>
              <div v-else-if="d.smart.selfTest?.last?.result === 'failed'" class="sub warn" :data-test="`selftest-failed-${d.id}`">
                <i class="pi pi-exclamation-triangle" /> {{ SELFTEST_KIND_LABEL[d.smart.selfTest.last.type] }} fehlgeschlagen
              </div>
              <div v-if="d.fsCheck?.running" class="sub" :data-test="`fscheck-running-${d.id}`"><i class="pi pi-spin pi-spinner" /> Dateisystem wird geprüft</div>
              <div v-else-if="d.fsCheck?.result === 'errors'" class="sub warn" :data-test="`fscheck-errors-${d.id}`">
                <i class="pi pi-exclamation-triangle" /> Dateisystem-Prüfung fand Fehler
              </div>
              <div v-if="d.bench?.running" class="sub" :data-test="`bench-running-${d.id}`"><i class="pi pi-spin pi-spinner" /> Benchmark läuft</div>
              <div v-if="hasBadSectors(d)" class="sub warn" :data-test="`sectors-${d.id}`">
                <i class="pi pi-exclamation-triangle" /> Defekte Sektoren ({{ d.smart.reallocatedSectors ?? 0 }} neu zugewiesen, {{ d.smart.pendingSectors ?? 0 }} instabil)
              </div>
            </template>
            <span v-else class="sub">unbekannt</span>
          </td>
          <td>
            <template v-if="canSpin(d)">
              <select :data-test="`spindown-${d.id}`" :value="String(d.spindown ?? 0)" :aria-label="`Ruhezustand für ${d.model || d.id}`" class="spin" @change="setSpindown(d, $event)">
                <option v-for="m in SPINDOWN_CHOICES" :key="m" :value="String(m)">{{ spindownLabel(m) }}</option>
              </select>
              <div v-if="spinError[d.id]" class="sub warn" :data-test="`spindown-error-${d.id}`">{{ spinError[d.id] }}</div>
            </template>
            <span v-else class="sub">—</span>
          </td>
          <td class="actions">
            <Button
              v-if="d.state === 'online' && d.device"
              :label="d.identifying ? 'Stopp' : 'Finden'"
              :icon="d.identifying ? 'pi pi-stop-circle' : 'pi pi-lightbulb'"
              size="small"
              text
              :severity="d.identifying ? 'warn' : undefined"
              :title="d.identifying ? 'Blinken beenden' : 'Aktivitäts-LED 30 Sekunden blinken lassen'"
              :data-test="`identify-${d.id}`"
              @click="toggleIdentify(d)"
            />
            <Button
              v-if="d.state === 'online'"
              label="Details"
              size="small"
              text
              :data-test="`details-${d.id}`"
              @click="openDetail(d)"
            />
            <template v-if="d.role === 'unassigned' && d.manageable && d.state === 'online'">
              <Button label="Formatieren" size="small" severity="danger" outlined :loading="busy === d.id" @click="openFormat(d)" />
              <Button
                v-if="canImport(d)"
                label="Übernehmen"
                size="small"
                outlined
                :loading="busy === d.id"
                @click="act(d.id, () => store.importDisk(d.id), 'Datenträger übernommen')"
              />
            </template>
            <Button
              v-else-if="(d.role === 'data' || d.role === 'parity') && !d.pool"
              label="Freigeben"
              size="small"
              text
              :loading="busy === d.id"
              @click="act(d.id, () => store.release(d.id), 'Datenträger freigegeben')"
            />
          </td>
        </tr>
      </tbody>
    </table>
  </div>
  <FormatDialog v-model:visible="showFormat" :preview="preview" :busy="!!busy" @submit="doFormat" />
  <DiskDetailDialog v-model:visible="showDetail" :disk="detailDisk" />
  <SelfTestScheduleDialog v-model:visible="showSchedule" />
</template>

<style scoped>
.disks {
  width: 100%;
  border-collapse: collapse;
}
.name {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
.check {
  width: 2rem;
}
.new-disk {
  margin-bottom: 0.6rem;
}
.new-body {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.8rem;
  flex-wrap: wrap;
}
.new-actions {
  display: flex;
  gap: 0.4rem;
  flex-wrap: wrap;
}
.toolbar {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 1rem;
  margin-bottom: 0.6rem;
}
.live {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  font-size: 0.9rem;
  cursor: pointer;
}
.io {
  font-variant-numeric: tabular-nums;
}
.usage {
  margin-top: 0.4rem;
}
.sub {
  font-size: 0.8rem;
  color: var(--bn-text-soft);
  margin-top: 0.15rem;
}
.spin {
  padding: 0.3rem 0.5rem;
}
.standby {
  margin-left: 0.3rem;
}
.warn {
  color: var(--bn-warn);
}
.actions {
  display: flex;
  gap: 0.4rem;
  justify-content: flex-end;
  flex-wrap: wrap;
}
</style>
