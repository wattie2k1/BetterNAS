<script setup lang="ts">
import { onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import FormatDialog from '@/components/storage/FormatDialog.vue'
import { useStorageStore } from '@/stores/storage'
import type { DiskView, FormatPreview, FormatRequest } from '@/api/storage'
import { formatBytes } from '@/utils/format'
import { SPINDOWN_CHOICES, spindownLabel } from '@/api/storage'

const store = useStorageStore()
const toast = useToast()
const preview = ref<FormatPreview | null>(null)
const showFormat = ref(false)
const busy = ref<string | null>(null)

onMounted(() => store.refresh())

const roleLabel: Record<string, { label: string; severity: string }> = {
  system: { label: 'System', severity: 'secondary' },
  data: { label: 'Datenplatte', severity: 'success' },
  parity: { label: 'Parität', severity: 'info' },
  unassigned: { label: 'Nicht zugewiesen', severity: 'warn' },
}

function fail(e: unknown) {
  toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 8000 })
}

async function act(id: string, fn: () => Promise<void>, done: string) {
  busy.value = id
  try {
    await fn()
    toast.add({ severity: 'success', summary: done, life: 4000 })
  } catch (e) {
    fail(e)
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
  await act(id, () => store.format(id, req), 'Datenträger formatiert')
  showFormat.value = false
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
  <div class="toolbar" />
  <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>
  <div class="table-wrap">
    <table class="disks">
      <thead>
        <tr>
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
          <td>
            <div class="name">
              <i :class="d.system ? 'pi pi-lock' : d.rotational ? 'pi pi-database' : 'pi pi-bolt'" />
              <strong>{{ d.model || 'Unbekannt' }}</strong>
            </div>
            <div class="sub">{{ d.device || '—' }} · {{ d.serial || d.id }}</div>
          </td>
          <td>{{ formatBytes(d.sizeBytes) }}<div class="sub">{{ d.rotational ? 'HDD' : 'SSD' }}</div></td>
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
</template>

<style scoped>
.table-wrap {
  overflow-x: auto;
}
.disks {
  width: 100%;
  border-collapse: collapse;
}
th,
td {
  text-align: left;
  padding: 0.7rem 0.9rem;
  border-bottom: 1px solid var(--bn-border-soft);
  vertical-align: top;
}
th {
  font-size: 0.8rem;
  color: var(--p-text-muted-color);
  font-weight: 600;
}
.name {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
.sub {
  font-size: 0.8rem;
  color: var(--p-text-muted-color);
  margin-top: 0.15rem;
}
.spin {
  padding: 0.3rem 0.4rem;
  border-radius: 8px;
  border: 1px solid var(--bn-border);
  background: var(--bn-glass);
  color: inherit;
  font: inherit;
}
.standby {
  margin-left: 0.3rem;
}
.warn {
  color: var(--p-orange-400);
}
.actions {
  display: flex;
  gap: 0.4rem;
  justify-content: flex-end;
  flex-wrap: wrap;
}
</style>
