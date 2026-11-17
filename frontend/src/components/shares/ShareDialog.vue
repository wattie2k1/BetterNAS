<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import ToggleSwitch from 'primevue/toggleswitch'
import Message from 'primevue/message'
import { isReservedShareName, SHARE_NAME_RE, subdirError, validNfsClient, type Access, type ShareAcl, type ShareInput, type ShareView } from '@/api/shares'
import type { PoolView } from '@/api/storage'
import type { GroupView, UserView } from '@/api/users'

const props = defineProps<{
  visible: boolean
  pools: PoolView[]
  users: UserView[]
  groups: GroupView[]
  shares: ShareView[]
  share?: ShareView | null
  /** Vorbelegung der Rechte für neue Freigaben (z. B. der angemeldete Administrator mit Schreibrecht). */
  defaultWriter?: string
  busy?: boolean
}>()
const emit = defineEmits<{ 'update:visible': [boolean]; submit: [ShareInput] }>()

const editing = computed(() => !!props.share)
const name = ref('')
const pool = ref('')
const subdir = ref('')
const subdirTouched = ref(false)
const comment = ref('')
const browsable = ref(true)
const guest = ref(false)
const guestWrite = ref(false)
const recycle = ref(true)
const fruit = ref(true)
const nfsEnabled = ref(false)
const nfsClients = ref('')
const nfsReadOnly = ref(false)
const nfsSquash = ref<'root' | 'all'>('root')
/** Schlüssel "user:anna" bzw. "group:familie" → Zugriff; fehlend = kein Eintrag. */
const access = ref<Record<string, Access>>({})

const key = (type: string, n: string) => `${type}:${n}`

function reset() {
  const s = props.share
  name.value = s?.name ?? ''
  pool.value = s?.pool ?? props.pools[0]?.name ?? ''
  subdir.value = s?.subdir ?? ''
  subdirTouched.value = !!s
  comment.value = s?.comment ?? ''
  browsable.value = s?.browsable ?? true
  guest.value = s?.guest ?? false
  guestWrite.value = s?.guestWrite ?? false
  recycle.value = s?.recycle ?? true
  fruit.value = s?.fruit ?? true
  nfsEnabled.value = s?.nfs?.enabled ?? false
  nfsClients.value = (s?.nfs?.clients ?? []).join('\n')
  nfsReadOnly.value = s?.nfs?.readOnly ?? false
  nfsSquash.value = s?.nfs?.squash ?? 'root'
  access.value = {}
  for (const a of s?.acl ?? []) access.value[key(a.type, a.name)] = a.access
  if (!s && props.defaultWriter) access.value[key('user', props.defaultWriter)] = 'rw'
}
watch(() => [props.visible, props.share], reset, { immediate: true })

// Der Ordner heißt wie die Freigabe, bis er von Hand geändert wird.
watch(name, (n) => {
  if (!subdirTouched.value) subdir.value = n
})

const accessOptions = computed(() => [
  { value: '', label: guest.value ? (guestWrite.value ? 'Wie Gast' : 'Nur Lesen (Gast)') : 'Kein Zugriff' },
  { value: 'ro', label: 'Lesen' },
  { value: 'rw', label: 'Lesen und Schreiben' },
  ...(guest.value ? [{ value: 'none', label: 'Gesperrt' }] : []),
])

const nameError = computed(() => {
  if (editing.value || name.value === '') return ''
  if (!SHARE_NAME_RE.test(name.value)) return 'Buchstaben, Ziffern, _ und -, höchstens 32 Zeichen.'
  if (isReservedShareName(name.value)) return 'Dieser Name ist für Samba reserviert.'
  if (props.shares.some((s) => s.name.toLowerCase() === name.value.toLowerCase())) return 'Eine Freigabe mit diesem Namen existiert bereits.'
  return ''
})
const folderError = computed(() => (subdir.value === '' ? '' : subdirError(subdir.value)))
const commentError = computed(() => (/[\\%]/.test(comment.value) || [...comment.value].length > 128 ? 'Höchstens 128 Zeichen, ohne \\ und %.' : ''))

/** Ein Client je Zeile; Leerzeilen und Leerraum außen werden entfernt. */
const clientList = computed(() => nfsClients.value.split('\n').map((c) => c.trim()).filter(Boolean))
const nfsError = computed(() => {
  if (!nfsEnabled.value) return ''
  if (!clientList.value.length) return 'Mindestens ein Client ist nötig (IP-Adresse, Netz wie 192.168.1.0/24 oder Rechnername).'
  if (clientList.value.length > 32) return 'Höchstens 32 Clients.'
  const bad = clientList.value.find((c) => !validNfsClient(c))
  return bad ? `Ungültiger Client: ${bad} (kein *, keine Optionen).` : ''
})

const acl = computed<ShareAcl[]>(() => {
  const out: ShareAcl[] = []
  for (const [k, a] of Object.entries(access.value)) {
    if (!a) continue
    const i = k.indexOf(':')
    out.push({ type: k.slice(0, i) as 'user' | 'group', name: k.slice(i + 1), access: a })
  }
  return out
})
const hasRight = computed(() => acl.value.some((a) => a.access !== 'none'))

const valid = computed(
  () =>
    name.value !== '' &&
    !nameError.value &&
    pool.value !== '' &&
    subdir.value !== '' &&
    !folderError.value &&
    !commentError.value &&
    !nfsError.value &&
    (hasRight.value || guest.value),
)

function submit() {
  if (!valid.value) return
  emit('submit', {
    name: name.value,
    pool: pool.value,
    subdir: subdir.value,
    comment: comment.value,
    browsable: browsable.value,
    guest: guest.value,
    guestWrite: guest.value && guestWrite.value,
    recycle: recycle.value,
    fruit: fruit.value,
    acl: acl.value,
    nfs: { enabled: nfsEnabled.value, clients: nfsEnabled.value ? clientList.value : [], readOnly: nfsReadOnly.value, squash: nfsSquash.value },
  })
}

function set(type: string, n: string, v: string | null) {
  const k = key(type, n)
  if (!v) delete access.value[k]
  else access.value[k] = v as Access
}
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="editing ? `Freigabe „${share?.name}“ bearbeiten` : 'Neue Freigabe'"
    :style="{ width: 'min(640px, 95vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <div class="form">
      <label for="share-name">Name</label>
      <InputText id="share-name" v-model="name" :disabled="editing" autocomplete="off" fluid />
      <small v-if="nameError" class="err">{{ nameError }}</small>
      <small v-else-if="!editing" class="hint">So erscheint die Freigabe im Netzwerk.</small>

      <label for="share-pool">Speicherpool</label>
      <Select v-model="pool" input-id="share-pool" :options="pools" option-label="name" option-value="name" append-to="self" fluid />
      <p v-if="!pools.length" class="hint">Es gibt noch keinen Speicherpool. Lege zuerst einen Pool an.</p>

      <label for="share-subdir">Ordner im Pool</label>
      <InputText id="share-subdir" v-model="subdir" autocomplete="off" fluid @input="subdirTouched = true" />
      <small v-if="folderError" class="err">{{ folderError }}</small>
      <small v-else class="hint">Wird angelegt, falls er fehlt. Unterordner mit / trennen, z. B. Medien/Filme.</small>

      <label for="share-comment">Beschreibung (optional)</label>
      <InputText id="share-comment" v-model="comment" fluid />
      <small v-if="commentError" class="err">{{ commentError }}</small>

      <span class="label">Zugriff</span>
      <div v-for="u in users" :key="`u-${u.name}`" class="acl" data-test="acl-row">
        <span><i class="pi pi-user" /> {{ u.name }}<span v-if="u.fullName" class="hint"> · {{ u.fullName }}</span></span>
        <Select
          :model-value="access[key('user', u.name)] ?? ''"
          :options="accessOptions"
          option-label="label"
          option-value="value"
          :input-id="`acl-user-${u.name}`"
          append-to="self"
          class="sel"
          @update:model-value="set('user', u.name, $event)"
        />
      </div>
      <div v-for="g in groups" :key="`g-${g.name}`" class="acl" data-test="acl-row">
        <span><i class="pi pi-users" /> {{ g.name }} <span class="hint">(Gruppe)</span></span>
        <Select
          :model-value="access[key('group', g.name)] ?? ''"
          :options="accessOptions"
          option-label="label"
          option-value="value"
          :input-id="`acl-group-${g.name}`"
          append-to="self"
          class="sel"
          @update:model-value="set('group', g.name, $event)"
        />
      </div>
      <Message v-if="!hasRight && !guest" severity="warn" :closable="false">Mindestens ein Benutzer oder eine Gruppe braucht Zugriff.</Message>

      <span class="label">Optionen</span>
      <div class="opt">
        <ToggleSwitch v-model="browsable" input-id="opt-browsable" />
        <label for="opt-browsable">In der Netzwerkumgebung sichtbar</label>
      </div>
      <div class="opt">
        <ToggleSwitch v-model="guest" input-id="opt-guest" />
        <label for="opt-guest">Gastzugriff (ohne Anmeldung)</label>
      </div>
      <div v-if="guest" class="opt">
        <Select
          v-model="guestWrite"
          input-id="opt-guest-write"
          :options="[{ value: false, label: 'Nur lesend (Read-Only)' }, { value: true, label: 'Lesend und schreibend (Read-Write)' }]"
          option-label="label"
          option-value="value"
          append-to="self"
          class="sel"
        />
      </div>
      <div class="opt">
        <ToggleSwitch v-model="recycle" input-id="opt-recycle" />
        <label for="opt-recycle">Papierkorb (gelöschte Dateien landen in .papierkorb)</label>
      </div>
      <div class="opt">
        <ToggleSwitch v-model="fruit" input-id="opt-fruit" />
        <label for="opt-fruit">macOS-Unterstützung</label>
      </div>

      <span class="label">NFS (Linux, macOS, andere NAS)</span>
      <div class="opt">
        <ToggleSwitch v-model="nfsEnabled" input-id="opt-nfs" />
        <label for="opt-nfs">Zusätzlich per NFS freigeben</label>
      </div>
      <template v-if="nfsEnabled">
        <label for="nfs-clients">Erlaubte Clients (einer pro Zeile)</label>
        <textarea id="nfs-clients" v-model="nfsClients" rows="3" placeholder="192.168.1.0/24&#10;pc.local" class="area" spellcheck="false" />
        <small v-if="nfsError" class="err" data-test="nfs-error">{{ nfsError }}</small>
        <Message severity="warn" :closable="false">
          Bei NFS entscheidet nur die Client-Liste über den Zugriff – die Benutzerrechte dieser Freigabe (Samba-Rechte gelten hier nicht)
          werden nicht geprüft. Trage nur Rechner ein, denen du vertraust.
        </Message>
        <div class="opt">
          <ToggleSwitch v-model="nfsReadOnly" input-id="nfs-readonly" />
          <label for="nfs-readonly">Nur lesen</label>
        </div>
        <label for="nfs-squash">Benutzerabbildung</label>
        <select id="nfs-squash" v-model="nfsSquash" class="area">
          <option value="root">Nur root wird abgebildet (Standard)</option>
          <option value="all">Alle Zugriffe laufen als anonymer Benutzer</option>
        </select>
      </template>
    </div>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button data-test="share-submit" :label="editing ? 'Speichern' : 'Anlegen'" :disabled="!valid || busy" :loading="busy" @click="submit" />
    </template>
  </Dialog>
</template>

<style scoped>
.form {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.form > label,
.label {
  font-weight: 600;
  margin-top: 0.8rem;
}
.acl {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
}
.sel {
  min-width: 14rem;
}
.opt {
  display: flex;
  align-items: center;
  gap: 0.6rem;
}
.area {
  padding: 0.5rem 0.6rem;
  border-radius: 8px;
  border: 1px solid var(--bn-border);
  background: var(--bn-glass);
  color: inherit;
  font: inherit;
}
.hint {
  color: var(--p-text-muted-color);
}
.err {
  color: var(--p-red-500);
}
</style>
