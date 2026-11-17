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

type Pending = { kind: ResourceKind; ref?: string; label: string; prune?: boolean }
const pending = ref<Pending | null>(null)

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

const header = computed(() => (pending.value?.prune ? 'Ungenutzte Einträge aufräumen' : 'Eintrag entfernen'))
const message = computed(() => {
  const p = pending.value
  if (!p) return ''
  if (p.prune) return pruneText[p.kind]
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
            <thead><tr><th>Image</th><th>Größe</th><th>Erstellt</th><th /></tr></thead>
            <tbody>
              <tr v-for="im in store.images" :key="im.id + im.ref" :data-test="`image-${im.ref}`">
                <td>
                  <strong v-if="!im.dangling">{{ im.ref }}</strong>
                  <Tag v-else value="ohne Namen" severity="secondary" />
                  <div class="sub">{{ im.id }}</div>
                </td>
                <td>{{ formatBytes(im.sizeBytes) }}</td>
                <td>{{ im.created }}</td>
                <td class="actions">
                  <Button icon="pi pi-trash" text rounded severity="danger" aria-label="Entfernen" title="Entfernen" @click="pending = { kind: 'images', ref: im.ref, label: im.ref }" />
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
            <thead><tr><th>Name</th><th>Treiber</th><th>Bereich</th><th /></tr></thead>
            <tbody>
              <tr v-for="n in store.networks" :key="n.id" :data-test="`network-${n.name}`">
                <td>
                  <strong>{{ n.name }}</strong>
                  <Tag v-if="n.builtin" class="tag" value="Eingebaut" severity="secondary" />
                  <div class="sub">{{ n.id }}</div>
                </td>
                <td>{{ n.driver }}</td>
                <td>{{ n.scope }}</td>
                <td class="actions">
                  <Button v-if="!n.builtin" icon="pi pi-trash" text rounded severity="danger" aria-label="Entfernen" title="Entfernen" @click="pending = { kind: 'networks', ref: n.id, label: n.name }" />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </TabPanel>
      <TabPanel value="volumes">
        <div class="table-wrap">
          <table>
            <thead><tr><th>Name</th><th>Treiber</th><th>Verwendung</th><th /></tr></thead>
            <tbody>
              <tr v-for="v in store.volumes" :key="v.name" :data-test="`volume-${v.name}`">
                <td><strong class="mono">{{ v.name }}</strong></td>
                <td>{{ v.driver }}</td>
                <td><Tag :value="v.unused ? 'Ungenutzt' : 'In Benutzung'" :severity="v.unused ? 'warn' : 'success'" /></td>
                <td class="actions">
                  <Button icon="pi pi-trash" text rounded severity="danger" aria-label="Entfernen" title="Löschen" @click="pending = { kind: 'volumes', ref: v.name, label: v.name }" />
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
  color: var(--p-text-muted-color);
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
.actions {
  white-space: nowrap;
  text-align: right !important;
}
</style>
