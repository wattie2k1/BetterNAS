<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Message from 'primevue/message'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { useDockerStore } from '@/stores/docker'
import type { ResourceKind } from '@/api/docker'
import { formatBytes } from '@/utils/format'

const store = useDockerStore()
const toast = useToast()
const busy = ref(false)
const tab = ref<ResourceKind>('images')

type Item = { ref: string; label: string }
type Pending = { kind: ResourceKind; ref?: string; label: string; prune?: boolean; bulk?: Item[] }
const pending = ref<Pending | null>(null)

// Auswahl je Reiter; nur ungenutzte, nicht eingebaute Einträge sind wählbar.
const selected = ref<Record<ResourceKind, string[]>>({ images: [], networks: [], volumes: [] })

const selectable = computed<Record<ResourceKind, Item[]>>(() => ({
  images: store.images.filter((i) => i.unused).map((i) => ({ ref: i.ref, label: i.ref })),
  networks: store.networks.filter((n) => n.unused && !n.builtin).map((n) => ({ ref: n.id, label: n.name })),
  volumes: store.volumes.filter((v) => v.unused).map((v) => ({ ref: v.name, label: v.name })),
}))

function isSelected(kind: ResourceKind, ref: string) {
  return selected.value[kind].includes(ref)
}
function toggle(kind: ResourceKind, ref: string) {
  const l = selected.value[kind]
  selected.value[kind] = l.includes(ref) ? l.filter((r) => r !== ref) : [...l, ref]
}
function allSelected(kind: ResourceKind) {
  const all = selectable.value[kind]
  return all.length > 0 && all.every((i) => isSelected(kind, i.ref))
}
function toggleAll(kind: ResourceKind) {
  selected.value[kind] = allSelected(kind) ? [] : selectable.value[kind].map((i) => i.ref)
}
const selectedItems = computed(() => {
  const kind = tab.value
  return selectable.value[kind].filter((i) => isSelected(kind, i.ref))
})
function askBulk() {
  pending.value = { kind: tab.value, label: `${selectedItems.value.length} Einträge`, bulk: selectedItems.value }
}

onMounted(async () => {
  await store.refresh()
  await store.loadResources()
})

const dockerDown = computed(() => store.status && (!store.status.installed || !store.status.active))

const pruneText: Record<ResourceKind, string> = {
  images: 'Alle Images ohne Namen und Tag, die von keinem Container verwendet werden, werden gelöscht. Sie lassen sich bei Bedarf neu laden.',
  networks: 'Alle Netzwerke, die von keinem Container verwendet werden, werden gelöscht.',
  volumes:
    'ACHTUNG: Alle Volumes, die von keinem Container verwendet werden, werden samt ihren Daten gelöscht. Das lässt sich nicht rückgängig machen. Ein Volume kann auch zu einem Stack gehören, der gerade gestoppt ist.',
}

const header = computed(() => (pending.value?.prune ? 'Ungenutzte Einträge aufräumen' : pending.value?.bulk ? 'Ausgewählte Einträge entfernen' : 'Eintrag entfernen'))
const message = computed(() => {
  const p = pending.value
  if (!p) return ''
  if (p.prune) return pruneText[p.kind]
  if (p.bulk) {
    const names = p.bulk.map((i) => `„${i.label}“`).join(', ')
    const tail = p.kind === 'volumes' ? ' Die Volumes werden samt allen Daten gelöscht. Das lässt sich nicht rückgängig machen.' : ''
    return `${p.bulk.length} Einträge werden entfernt: ${names}.${tail}`
  }
  if (p.kind === 'volumes') return `Das Volume „${p.label}“ wird samt allen Daten gelöscht. Das lässt sich nicht rückgängig machen.`
  return `„${p.label}“ wird entfernt.`
})

async function confirm() {
  const p = pending.value
  if (!p) return
  busy.value = true
  try {
    if (p.prune) {
      const res = await store.prune(p.kind, p.kind === 'volumes')
      toast.add({ severity: 'success', summary: 'Aufgeräumt', detail: res.reclaimed ? `${res.reclaimed} freigegeben` : undefined, life: 5000 })
    } else if (p.bulk) {
      const failed: string[] = []
      for (const it of p.bulk) {
        try {
          await store.removeResource(p.kind, it.ref)
        } catch {
          failed.push(it.label)
        }
      }
      selected.value[p.kind] = []
      const ok = p.bulk.length - failed.length
      if (ok) toast.add({ severity: 'success', summary: 'Entfernt', detail: `${ok} Einträge`, life: 4000 })
      if (failed.length) toast.add({ severity: 'error', summary: 'Nicht entfernt', detail: failed.join(', '), life: 10000 })
    } else {
      await store.removeResource(p.kind, p.ref!)
      toast.add({ severity: 'success', summary: 'Entfernt', detail: p.label, life: 4000 })
    }
    pending.value = null
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="toolbar">
    <Button v-if="tab === 'images'" data-test="prune-images" label="Unbenannte Images aufräumen" icon="pi pi-trash" severity="secondary" @click="pending = { kind: 'images', label: 'Images', prune: true }" />
    <Button v-if="tab === 'networks'" data-test="prune-networks" label="Ungenutzte aufräumen" icon="pi pi-trash" severity="secondary" @click="pending = { kind: 'networks', label: 'Netzwerke', prune: true }" />
    <Button v-if="tab === 'volumes'" data-test="prune-volumes" label="Ungenutzte Volumes löschen" icon="pi pi-trash" severity="danger" outlined @click="pending = { kind: 'volumes', label: 'Volumes', prune: true }" />
    <Button v-if="selectedItems.length" data-test="remove-selected" :label="`Ausgewählte entfernen (${selectedItems.length})`" icon="pi pi-trash" severity="danger" @click="askBulk" />
  </div>
  <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>
  <Message v-if="dockerDown" severity="warn" :closable="false" data-test="docker-down">
    {{ store.status?.installed ? 'Der Docker-Dienst läuft nicht.' : 'Docker ist nicht installiert.' }}
  </Message>
  <Tabs v-model:value="tab">
    <TabList>
      <Tab value="images">Images ({{ store.images.length }})</Tab>
      <Tab value="networks">Netzwerke ({{ store.networks.length }})</Tab>
      <Tab value="volumes">Volumes ({{ store.volumes.length }})</Tab>
    </TabList>
    <TabPanels>
      <TabPanel value="images">
        <div class="table-wrap">
          <table>
            <thead><tr><th class="sel"><input type="checkbox" data-test="select-all" aria-label="Alle auswählen" :checked="allSelected('images')" :disabled="!selectable.images.length" @change="toggleAll('images')" /></th><th>Image</th><th>Größe</th><th>Erstellt</th><th>Verwendung</th><th /></tr></thead>
            <tbody>
              <tr v-for="im in store.images" :key="im.id + im.ref" :data-test="`image-${im.ref}`">
                <td class="sel"><input type="checkbox" aria-label="Auswählen" :checked="isSelected('images', im.ref)" :disabled="!im.unused" @change="toggle('images', im.ref)" /></td>
                <td>
                  <strong v-if="!im.dangling">{{ im.ref }}</strong>
                  <Tag v-else value="ohne Namen" severity="secondary" />
                  <div class="sub">{{ im.id }}</div>
                </td>
                <td>{{ formatBytes(im.sizeBytes) }}</td>
                <td>{{ im.created }}</td>
                <td><Tag :value="im.unused ? 'Ungenutzt' : 'In Benutzung'" :severity="im.unused ? 'warn' : 'success'" /></td>
                <td class="actions">
                  <Button icon="pi pi-trash" text rounded severity="danger" aria-label="Entfernen" :disabled="!im.unused" :title="im.unused ? 'Entfernen' : 'In Benutzung – kann nicht entfernt werden'" @click="pending = { kind: 'images', ref: im.ref, label: im.ref }" />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-if="!store.images.length" class="empty">Keine Images vorhanden.</p>
      </TabPanel>
      <TabPanel value="networks">
        <div class="table-wrap">
          <table>
            <thead><tr><th class="sel"><input type="checkbox" data-test="select-all" aria-label="Alle auswählen" :checked="allSelected('networks')" :disabled="!selectable.networks.length" @change="toggleAll('networks')" /></th><th>Name</th><th>Treiber</th><th>Bereich</th><th>Verwendung</th><th /></tr></thead>
            <tbody>
              <tr v-for="n in store.networks" :key="n.id" :data-test="`network-${n.name}`">
                <td class="sel"><input type="checkbox" aria-label="Auswählen" :checked="isSelected('networks', n.id)" :disabled="!n.unused || n.builtin" @change="toggle('networks', n.id)" /></td>
                <td>
                  <strong>{{ n.name }}</strong>
                  <Tag v-if="n.builtin" class="tag" value="Eingebaut" severity="secondary" />
                  <div class="sub">{{ n.id }}</div>
                </td>
                <td>{{ n.driver }}</td>
                <td>{{ n.scope }}</td>
                <td><Tag :value="n.unused ? 'Ungenutzt' : 'In Benutzung'" :severity="n.unused ? 'warn' : 'success'" /></td>
                <td class="actions">
                  <Button v-if="!n.builtin" icon="pi pi-trash" text rounded severity="danger" aria-label="Entfernen" :disabled="!n.unused" :title="n.unused ? 'Entfernen' : 'In Benutzung – kann nicht entfernt werden'" @click="pending = { kind: 'networks', ref: n.id, label: n.name }" />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </TabPanel>
      <TabPanel value="volumes">
        <div class="table-wrap">
          <table>
            <thead><tr><th class="sel"><input type="checkbox" data-test="select-all" aria-label="Alle auswählen" :checked="allSelected('volumes')" :disabled="!selectable.volumes.length" @change="toggleAll('volumes')" /></th><th>Name</th><th>Treiber</th><th>Verwendung</th><th /></tr></thead>
            <tbody>
              <tr v-for="v in store.volumes" :key="v.name" :data-test="`volume-${v.name}`">
                <td class="sel"><input type="checkbox" aria-label="Auswählen" :checked="isSelected('volumes', v.name)" :disabled="!v.unused" @change="toggle('volumes', v.name)" /></td>
                <td><strong class="mono">{{ v.name }}</strong></td>
                <td>{{ v.driver }}</td>
                <td><Tag :value="v.unused ? 'Ungenutzt' : 'In Benutzung'" :severity="v.unused ? 'warn' : 'success'" /></td>
                <td class="actions">
                  <Button icon="pi pi-trash" text rounded severity="danger" aria-label="Entfernen" :disabled="!v.unused" :title="v.unused ? 'Löschen' : 'In Benutzung – kann nicht gelöscht werden'" @click="pending = { kind: 'volumes', ref: v.name, label: v.name }" />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-if="!store.volumes.length" class="empty">Keine Volumes vorhanden.</p>
      </TabPanel>
    </TabPanels>
  </Tabs>
  <ConfirmDialog
    :visible="!!pending"
    :header="header"
    :message="message"
    :confirm-label="pending?.prune ? 'Aufräumen' : 'Entfernen'"
    :busy="busy"
    @update:visible="(v) => !v && (pending = null)"
    @confirm="confirm"
  />
</template>

<style scoped>
.empty {
  color: var(--bn-text-soft);
  font-size: 0.875rem;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.85rem;
  overflow-wrap: anywhere;
}
.tag {
  margin-left: 0.4rem;
}
.sel {
  width: 2rem;
}
.actions {
  white-space: nowrap;
  text-align: right !important;
}
</style>
