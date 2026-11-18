<script setup lang="ts">
import { onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Card from 'primevue/card'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import ShareDialog from '@/components/shares/ShareDialog.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { useSharesStore } from '@/stores/shares'
import { useStorageStore } from '@/stores/storage'
import { useUsersStore } from '@/stores/users'
import { useAuthStore } from '@/stores/auth'
import type { ShareInput, ShareView } from '@/api/shares'

const store = useSharesStore()
const storage = useStorageStore()
const usersStore = useUsersStore()
const auth = useAuthStore()
const toast = useToast()
const showDialog = ref(false)
const editing = ref<ShareView | null>(null)
const deleting = ref<ShareView | null>(null)
const busy = ref(false)

onMounted(() => Promise.all([store.refresh(), storage.refresh(), usersStore.refresh()]))

async function run(fn: () => Promise<void>, done: string) {
  busy.value = true
  try {
    await fn()
    toast.add({ severity: 'success', summary: done, life: 4000 })
    showDialog.value = false
    deleting.value = null
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = false
  }
}

function openNew() {
  editing.value = null
  showDialog.value = true
}
function openEdit(s: ShareView) {
  editing.value = s
  showDialog.value = true
}
function save(input: ShareInput) {
  if (editing.value) {
    const name = editing.value.name
    run(() => store.update(name, input), 'Freigabe gespeichert')
  } else run(() => store.create(input), 'Freigabe angelegt')
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.add({ severity: 'info', summary: 'Adresse kopiert', life: 2500 })
  } catch {
    toast.add({ severity: 'warn', summary: 'Kopieren nicht möglich', detail: text, life: 8000 })
  }
}
const nfsCommand = (s: ShareView) => `mount -t nfs ${s.nfsMount} /mnt`
/** Mac-Adresse: einfache Hostnamen werden per Bonjour als „.local“ erreicht; FQDN und IP bleiben unverändert. */
const smbUrl = (s: ShareView) => {
  const host = s.uncPath.split('\\')[2] ?? ''
  return `smb://${host.includes('.') || host.includes(':') ? host : `${host}.local`}/${s.name}`
}
</script>

<template>
  <div class="toolbar">
    <Button label="Neue Freigabe" icon="pi pi-plus" @click="openNew" />
  </div>
  <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>
  <p v-if="!store.shares.length && !store.loading" class="empty">
    Noch keine Freigaben. Eine Freigabe macht einen Ordner deines Speicherpools im Netzwerk verfügbar (Windows, macOS, Linux).
  </p>
  <div class="grid">
    <Card v-for="s in store.shares" :key="s.name" :data-test="`share-${s.name}`">
      <template #title>
        <div class="title">
          <span><i class="pi pi-folder-open" /> {{ s.name }}</span>
          <Tag v-if="!s.poolMounted" value="Pool fehlt" severity="danger" :title="`Der Speicherpool „${s.pool}“ ist nicht eingehängt.`" />
        </div>
      </template>
      <template #content>
        <Message v-if="!s.poolMounted" severity="warn" :closable="false" data-test="pool-missing">
          Der Speicherpool „{{ s.pool }}“ ist nicht eingehängt, daher ist diese Freigabe im Netzwerk nicht erreichbar.
          Prüfe unter Datenträger und Speicherpools, ob die Platten angeschlossen sind.
        </Message>
        <p v-if="s.comment" class="sub">{{ s.comment }}</p>
        <p class="sub">{{ s.path }}</p>
        <div class="tags">
          <Tag v-if="s.guest" value="Gast" severity="warn" />
          <Tag v-if="s.recycle" value="Papierkorb" severity="secondary" />
          <Tag v-if="s.fruit" value="macOS" severity="secondary" />
          <Tag v-if="s.nfs?.enabled" value="NFS" severity="info" />
          <Tag v-if="!s.browsable" value="Versteckt" severity="secondary" />
        </div>
        <div class="addr">
          <code>{{ s.uncPath }}</code>
          <Button icon="pi pi-copy" size="small" text rounded aria-label="Windows-Adresse kopieren" @click="copy(s.uncPath)" />
        </div>
        <div class="addr">
          <code>{{ smbUrl(s) }}</code>
          <Button icon="pi pi-copy" size="small" text rounded aria-label="Mac-Adresse kopieren" @click="copy(smbUrl(s))" />
        </div>
        <div v-if="s.nfsMount" class="addr" data-test="nfs-mount">
          <code>{{ nfsCommand(s) }}</code>
          <Button icon="pi pi-copy" size="small" text rounded aria-label="NFS-Befehl kopieren" @click="copy(nfsCommand(s))" />
        </div>
      </template>
      <template #footer>
        <div class="actions">
          <Button label="Bearbeiten" icon="pi pi-pencil" text @click="openEdit(s)" />
          <Button label="Löschen" icon="pi pi-trash" text severity="danger" @click="deleting = s" />
        </div>
      </template>
    </Card>
  </div>
  <ShareDialog
    v-model:visible="showDialog"
    :pools="storage.pools"
    :users="usersStore.users"
    :groups="usersStore.groups"
    :shares="store.shares"
    :share="editing"
    :default-writer="auth.user ?? undefined"
    :busy="busy"
    @submit="save"
  />
  <ConfirmDialog
    :visible="!!deleting"
    header="Freigabe löschen"
    :message="`Die Freigabe „${deleting?.name}“ wird entfernt. Die Dateien im Ordner bleiben erhalten.`"
    :busy="busy"
    @update:visible="(v) => !v && (deleting = null)"
    @confirm="run(() => store.remove(deleting!.name), 'Freigabe gelöscht')"
  />
</template>

<style scoped>
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: 1rem;
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
.tags {
  display: flex;
  gap: 0.3rem;
  flex-wrap: wrap;
  margin: 0.5rem 0;
}
.addr {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}
.addr code {
  overflow-wrap: anywhere;
  font-size: 0.85rem;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.25rem;
}
</style>
