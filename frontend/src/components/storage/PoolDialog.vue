<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Checkbox from 'primevue/checkbox'
import Message from 'primevue/message'
import SelectButton from 'primevue/selectbutton'
import {
  BTRFS_PROFILES,
  btrfsUsable,
  MIN_FREE_RE,
  minFreeBytes,
  POLICIES,
  POOL_NAME_RE,
  type BtrfsProfile,
  type DiskView,
  type PoolInput,
  type PoolType,
  type PoolView,
} from '@/api/storage'
import { formatBytes } from '@/utils/format'

const props = defineProps<{ visible: boolean; disks: DiskView[]; pools: PoolView[]; pool?: PoolView | null; busy?: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; submit: [PoolInput] }>()

const editing = computed(() => !!props.pool)
const name = ref('')
const branches = ref<string[]>([])
const policy = ref('epmfs')
const minFree = ref('20G')
const type = ref<PoolType>('mergerfs')
const profile = ref<BtrfsProfile>('single')
const compress = ref<'off' | 'zstd:1' | 'zstd:3'>('off')
const confirmName = ref('')
/** Hat der Nutzer das Profil selbst gewählt, ändern wir es nicht mehr von allein. */
const profileTouched = ref(false)
const isBtrfs = computed(() => !editing.value && type.value === 'btrfs')

const TYPES = [
  { value: 'mergerfs', label: 'Flexibel (mergerfs)' },
  { value: 'btrfs', label: 'Redundant (btrfs)' },
]
const COMPRESS = [
  { value: 'off', label: 'Aus (empfohlen für Filme und Fotos)' },
  { value: 'zstd:1', label: 'zstd leicht' },
  { value: 'zstd:3', label: 'zstd stärker' },
]

function reset() {
  name.value = props.pool?.name ?? ''
  branches.value = [...(props.pool?.branches ?? [])]
  policy.value = props.pool?.createPolicy ?? 'epmfs'
  minFree.value = props.pool?.minFree ?? '20G'
  type.value = 'mergerfs'
  profile.value = 'single'
  compress.value = 'off'
  confirmName.value = ''
  profileTouched.value = false
}
watch(() => [props.visible, props.pool], reset, { immediate: true })

/** Wählbar: eingehängte Datenplatten ohne Pool – beim Bearbeiten zusätzlich die eigenen. */
const options = computed(() =>
  isBtrfs.value
    ? // btrfs legt das Dateisystem neu an: freie, nicht eingerichtete Platten (sie werden gelöscht)
      props.disks.filter((d) => d.role === 'unassigned' && d.state === 'online' && d.manageable && !d.system)
    : props.disks.filter(
        (d) => d.role === 'data' && d.state === 'online' && (!d.pool || (editing.value && d.pool === props.pool?.name)),
      ),
)

// Wechselt der Typ, passt die bisherige Auswahl nicht mehr.
watch(type, () => (branches.value = []))

/** Profile, die mit der aktuellen Plattenzahl möglich sind. */
const profileOptions = computed(() =>
  BTRFS_PROFILES.map((p) => ({ ...p, disabled: branches.value.length > 0 && branches.value.length < p.min })),
)
// Vorschlag: ab zwei Platten RAID1. Ein ungültig gewordenes Profil wird immer ersetzt.
watch(
  () => branches.value.length,
  (n) => {
    const cur = BTRFS_PROFILES.find((p) => p.value === profile.value)
    if (cur && n >= cur.min && profileTouched.value) return
    profile.value = n >= 2 ? 'raid1' : 'single'
  },
)
const profileHint = computed(() => BTRFS_PROFILES.find((p) => p.value === profile.value)?.hint ?? '')
const selectedDisks = computed(() => options.value.filter((d) => branches.value.includes(d.id)))
const capacity = computed(() => btrfsUsable(profile.value, selectedDisks.value.map((d) => d.sizeBytes)))
const wipes = computed(() => selectedDisks.value.filter((d) => d.partitions.length > 0))
const lopsided = computed(() => {
  const sizes = selectedDisks.value.map((d) => d.sizeBytes)
  return sizes.length > 1 && Math.max(...sizes) > Math.min(...sizes) * 1.25 && capacity.value.unused > 0
})

const nameError = computed(() => {
  if (editing.value || name.value === '') return ''
  if (!POOL_NAME_RE.test(name.value)) return 'Nur Kleinbuchstaben, Ziffern und Bindestrich, beginnend mit einem Buchstaben (max. 32 Zeichen).'
  if (props.pools.some((p) => p.name === name.value)) return 'Ein Pool mit diesem Namen existiert bereits.'
  return ''
})
const minFreeError = computed(() => (MIN_FREE_RE.test(minFree.value) ? '' : 'Zahl mit K, M oder G, z. B. 20G'))
const profileMin = computed(() => BTRFS_PROFILES.find((p) => p.value === profile.value)?.min ?? 1)
const valid = computed(() => {
  if (name.value === '' || nameError.value || branches.value.length === 0) return false
  if (isBtrfs.value) return branches.value.length >= profileMin.value && confirmName.value === name.value
  return !minFreeError.value
})
/** Platten, die nicht größer als der Mindestfreiraum sind, bekommen keine neuen Dateien (mergerfs). */
const tooSmall = computed(() => {
  const limit = minFreeBytes(minFree.value)
  return options.value.filter((d) => branches.value.includes(d.id) && limit > 0 && d.sizeBytes <= limit)
})
/** Mit aktivem SnapRAID dürfen vorhandene Platten nicht entfernt werden. */
const locked = (id: string) => editing.value && !!props.pool?.snapraid && props.pool.branches.includes(id)

const policyHint = computed(() => POLICIES.find((p) => p.value === policy.value)?.hint ?? '')

function submit() {
  if (!valid.value) return
  // Reihenfolge der Platten wie angezeigt, unabhängig von der Klickreihenfolge
  const ordered = options.value.map((d) => d.id).filter((id) => branches.value.includes(id))
  if (isBtrfs.value) {
    emit('submit', {
      name: name.value,
      type: 'btrfs',
      branches: ordered,
      createPolicy: 'epmfs',
      minFree: '20G',
      profile: profile.value,
      compress: compress.value === 'off' ? '' : compress.value,
      confirmName: confirmName.value,
    })
    return
  }
  emit('submit', { name: name.value, branches: ordered, createPolicy: policy.value, minFree: minFree.value })
}
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="editing ? `Pool „${pool?.name}“ bearbeiten` : 'Neuer Speicherpool'"
    :style="{ width: 'min(600px, 95vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <div class="form">
      <template v-if="!editing">
        <span class="label first">Art des Pools</span>
        <SelectButton v-model="type" :options="TYPES" option-label="label" option-value="value" :allow-empty="false" data-test="pool-type" />
        <small class="hint">
          <template v-if="type === 'mergerfs'">Beliebige Platten, jede Datei liegt auf einer Platte und bleibt dort lesbar. Schutz gegen Ausfall mit SnapRAID.</template>
          <template v-else>Alle Platten bilden ein gemeinsames Dateisystem mit Prüfsummen und eingebauter Redundanz (RAID1 und mehr).</template>
        </small>
      </template>
      <label for="pool-name">Name</label>
      <InputText id="pool-name" v-model="name" :disabled="editing" placeholder="z. B. medien" fluid />
      <small v-if="nameError" class="err">{{ nameError }}</small>
      <small v-else-if="!editing" class="hint">Erreichbar unter /srv/pools/{{ name || '…' }}</small>

      <span class="label">Datenträger</span>
      <p v-if="!options.length && isBtrfs" class="hint" data-test="btrfs-no-disks">
        Keine freien Platten. Ein btrfs-Pool braucht Platten, die noch nicht eingerichtet sind (neu angeschlossen oder freigegeben).
      </p>
      <p v-else-if="!options.length" class="hint">
        Keine freien Datenplatten. Unter „Datenträger“ zuerst eine Platte formatieren oder übernehmen.
      </p>
      <div
        v-for="d in options"
        :key="d.id"
        class="branch"
        data-test="branch-option"
        :data-id="d.id"
      >
        <Checkbox v-model="branches" :value="d.id" :input-id="`b-${d.id}`" :disabled="locked(d.id)" />
        <label :for="`b-${d.id}`">{{ d.model || d.id }} · {{ d.device }} · {{ formatBytes(d.sizeBytes) }}</label>
      </div>

      <Message v-if="editing && pool?.snapraid" severity="info" :closable="false" data-test="snapraid-lock">
        Solange SnapRAID aktiv ist, können Datenplatten nicht entfernt werden – dafür SnapRAID zuerst deaktivieren.
      </Message>

      <template v-if="isBtrfs">
        <label for="pool-profile">Schutz</label>
        <Select
          v-model="profile"
          input-id="pool-profile"
          :options="profileOptions"
          option-label="label"
          option-value="value"
          option-disabled="disabled"
          append-to="self"
          data-test="btrfs-profile"
          fluid
          @update:model-value="profileTouched = true"
        />
        <small class="hint">{{ profileHint }}</small>
        <small v-if="branches.length > 0 && branches.length < profileMin" class="err">Dieses Profil braucht mindestens {{ profileMin }} Platten.</small>

        <label for="pool-compress">Kompression</label>
        <Select v-model="compress" input-id="pool-compress" :options="COMPRESS" option-label="label" option-value="value" append-to="self" fluid />

        <p v-if="selectedDisks.length" class="capacity" data-test="btrfs-capacity">
          Nutzbar: <strong>{{ formatBytes(capacity.usable) }}</strong> von {{ formatBytes(selectedDisks.reduce((a, d) => a + d.sizeBytes, 0)) }} Rohkapazität
        </p>
        <Message v-if="lopsided" severity="warn" :closable="false" data-test="btrfs-lopsided">
          Die Platten sind unterschiedlich groß: etwa {{ formatBytes(capacity.unused) }} der größeren Platten bleiben ungenutzt.
        </Message>
        <Message severity="error" :closable="false" data-test="btrfs-wipe">
          Beim Anlegen werden die gewählten Platten <strong>vollständig gelöscht</strong>.
          <template v-if="wipes.length">
            Vorhandene Daten gehen verloren:
            <span v-for="d in wipes" :key="d.id" class="wipe">
              {{ d.model || d.id }} ({{ d.partitions.map((p) => [p.fsType, p.label].filter(Boolean).join(' ')).join(', ') }})
            </span>
          </template>
        </Message>
        <label for="pool-confirm">Zur Bestätigung den Poolnamen eingeben</label>
        <InputText id="pool-confirm" v-model="confirmName" autocomplete="off" fluid data-test="btrfs-confirm" />
      </template>

      <template v-if="!isBtrfs">
      <label for="pool-policy">Verteilung neuer Dateien</label>
      <Select
        v-model="policy"
        input-id="pool-policy"
        :options="POLICIES"
        option-label="label"
        option-value="value"
        append-to="self"
        fluid
      />
      <small class="hint">{{ policyHint }}</small>

      <label for="pool-minfree">Mindestens frei lassen je Platte</label>
      <InputText id="pool-minfree" v-model="minFree" fluid />
      <small v-if="minFreeError" class="err">{{ minFreeError }}</small>

      <Message v-if="tooSmall.length" severity="warn" :closable="false" data-test="minfree-warning">
        {{ tooSmall.length === 1 ? 'Die gewählte Platte ist' : 'Gewählte Platten sind' }} nicht größer als der Mindestfreiraum
        ({{ minFree }}). Dort werden keine neuen Dateien angelegt. Wähle einen kleineren Wert, z. B. 1G.
      </Message>
      <Message severity="info" :closable="false">
        mergerfs allein bietet keine Ausfallsicherheit – SnapRAID lässt sich nach dem Anlegen am Pool einrichten.
      </Message>
      </template>
    </div>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button
        data-test="pool-submit"
        :label="editing ? 'Speichern' : isBtrfs ? 'Anlegen und Platten löschen' : 'Anlegen'"
        :disabled="!valid || busy"
        :loading="busy"
        @click="submit"
      />
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
.label.first {
  margin-top: 0;
}
.capacity {
  margin: 0.4rem 0 0;
}
.wipe {
  display: block;
}
.branch {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
.hint {
  color: var(--bn-text-soft);
}
.err {
  color: var(--bn-danger);
}
</style>
