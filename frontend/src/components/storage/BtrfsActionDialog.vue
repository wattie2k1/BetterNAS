<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Checkbox from 'primevue/checkbox'
import Message from 'primevue/message'
import { BTRFS_PROFILES, profileLabel, profileMin, profileRedundancy, type BtrfsProfile, type DiskView, type PoolView } from '@/api/storage'
import { formatBytes } from '@/utils/format'

export type BtrfsPayload =
  | { kind: 'replace'; oldDisk: string; newDisk: string; confirmName: string }
  | { kind: 'add'; newDisk: string; balance: boolean; confirmName: string }
  | { kind: 'remove'; disk: string; confirmName: string }
  | { kind: 'balance'; usage: number }
  | { kind: 'convert'; profile: BtrfsProfile; confirmName: string }

const props = defineProps<{
  visible: boolean
  kind: 'replace' | 'add' | 'remove' | 'balance' | 'convert' | null
  pool: PoolView | null
  disks: DiskView[]
  busy?: boolean
}>()
const emit = defineEmits<{ 'update:visible': [boolean]; submit: [BtrfsPayload] }>()

const member = ref('')
const newDisk = ref('')
const balance = ref(true)
const usage = ref(50)
const profile = ref<BtrfsProfile>('raid1')
const confirm = ref('')

watch(
  () => [props.visible, props.kind, props.pool?.name],
  () => {
    member.value = ''
    newDisk.value = ''
    balance.value = true
    usage.value = 50
    confirm.value = ''
    profile.value = (BTRFS_PROFILES.find((p) => p.value !== props.pool?.profile && profileMin(p.value) <= (props.pool?.branches.length ?? 0))?.value ?? 'raid1') as BtrfsProfile
  },
  { immediate: true },
)

const titles = {
  replace: 'Platte ersetzen',
  add: 'Platte hinzufügen',
  remove: 'Platte entfernen',
  balance: 'Daten neu verteilen',
  convert: 'Schutz ändern',
}
const name = computed(() => props.pool?.name ?? '')
const label = (d: DiskView) => `${d.model || d.id} · ${d.device || 'nicht angeschlossen'} · ${formatBytes(d.sizeBytes)}`

const members = computed(() =>
  (props.pool?.branches ?? []).map((id) => {
    const d = props.disks.find((x) => x.id === id)
    return { id, label: d ? label(d) + (d.state === 'missing' ? ' – fehlt' : '') : id }
  }),
)
const free = computed(() => props.disks.filter((d) => d.role === 'unassigned' && d.state === 'online' && d.manageable && !d.system))
const freeOptions = computed(() => free.value.map((d) => ({ id: d.id, label: label(d), parts: d.partitions })))
const picked = computed(() => free.value.find((d) => d.id === newDisk.value))

const profiles = computed(() =>
  BTRFS_PROFILES.filter((p) => p.value !== props.pool?.profile).map((p) => ({
    ...p,
    disabled: p.min > (props.pool?.branches.length ?? 0),
  })),
)
const reduces = computed(() => profileRedundancy(profile.value) < profileRedundancy(props.pool?.profile ?? 'single'))
const minAfterRemove = computed(() => profileMin(props.pool?.profile ?? 'single'))
const cannotRemove = computed(() => (props.pool?.branches.length ?? 0) - 1 < minAfterRemove.value)

const needsName = computed(() => props.kind === 'replace' || props.kind === 'add' || props.kind === 'remove' || (props.kind === 'convert' && reduces.value))
const valid = computed(() => {
  if (needsName.value && confirm.value !== name.value) return false
  switch (props.kind) {
    case 'replace':
      return !!member.value && !!newDisk.value
    case 'add':
      return !!newDisk.value
    case 'remove':
      return !!member.value && !cannotRemove.value
    case 'convert':
      return !!profile.value && !profiles.value.find((p) => p.value === profile.value)?.disabled
    default:
      return props.kind === 'balance'
  }
})

function submit() {
  if (!valid.value || !props.kind) return
  switch (props.kind) {
    case 'replace':
      return emit('submit', { kind: 'replace', oldDisk: member.value, newDisk: newDisk.value, confirmName: confirm.value })
    case 'add':
      return emit('submit', { kind: 'add', newDisk: newDisk.value, balance: balance.value, confirmName: confirm.value })
    case 'remove':
      return emit('submit', { kind: 'remove', disk: member.value, confirmName: confirm.value })
    case 'balance':
      return emit('submit', { kind: 'balance', usage: usage.value })
    case 'convert':
      return emit('submit', { kind: 'convert', profile: profile.value, confirmName: confirm.value })
  }
}
</script>

<template>
  <Dialog
    :visible="visible && !!kind"
    modal
    append-to="self"
    :header="`${kind ? titles[kind] : ''} – ${name}`"
    :style="{ width: 'min(560px, 95vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <div class="form" data-test="btrfs-action">
      <template v-if="kind === 'replace' || kind === 'remove'">
        <label for="m-disk">{{ kind === 'replace' ? 'Zu ersetzende Platte' : 'Platte entfernen' }}</label>
        <Select v-model="member" input-id="m-disk" :options="members" option-label="label" option-value="id" placeholder="Platte wählen" append-to="self" fluid data-test="btrfs-member" />
      </template>

      <template v-if="kind === 'replace' || kind === 'add'">
        <label for="n-disk">Neue Platte</label>
        <p v-if="!freeOptions.length" class="hint" data-test="btrfs-no-free">Keine freie Platte vorhanden. Schließe eine neue Platte an oder gib eine Platte unter „Datenträger“ frei.</p>
        <Select v-else v-model="newDisk" input-id="n-disk" :options="freeOptions" option-label="label" option-value="id" placeholder="Platte wählen" append-to="self" fluid data-test="btrfs-new-disk" />
        <Message severity="error" :closable="false" data-test="btrfs-wipe">
          Die neue Platte wird <strong>vollständig gelöscht</strong>.
          <template v-if="picked?.partitions.length">
            Vorhandene Daten gehen verloren: {{ picked.partitions.map((p) => [p.fsType, p.label].filter(Boolean).join(' ')).join(', ') }}.
          </template>
        </Message>
        <p v-if="kind === 'replace'" class="hint">Die Daten werden im laufenden Betrieb auf die neue Platte kopiert. Das kann Stunden dauern; der Pool bleibt nutzbar.</p>
        <div v-if="kind === 'add'" class="check">
          <Checkbox v-model="balance" binary input-id="bal" />
          <label for="bal">Danach die Daten gleichmäßig auf alle Platten verteilen (empfohlen)</label>
        </div>
      </template>

      <template v-if="kind === 'remove'">
        <Message v-if="cannotRemove" severity="warn" :closable="false" data-test="btrfs-remove-min">
          Das Profil {{ profileLabel(pool?.profile) }} braucht mindestens {{ minAfterRemove }} Platten. Ändere zuerst den Schutz.
        </Message>
        <p class="hint">btrfs verschiebt die Daten der Platte vorher auf die übrigen. Das braucht genug freien Platz und kann Stunden dauern. Eine fehlende Platte wird ohne Verschieben entfernt.</p>
      </template>

      <template v-if="kind === 'balance'">
        <label for="usage">Umfang</label>
        <Select
          v-model="usage"
          input-id="usage"
          :options="[
            { v: 50, l: 'Schnell: nur wenig gefüllte Blöcke' },
            { v: 0, l: 'Gründlich: alles neu verteilen' },
          ]"
          option-label="l"
          option-value="v"
          append-to="self"
          fluid
        />
        <p class="hint">Verteilt die Daten neu, zum Beispiel nach dem Hinzufügen einer Platte. Der Pool bleibt nutzbar, ist aber währenddessen langsamer.</p>
      </template>

      <template v-if="kind === 'convert'">
        <label for="prof">Neues Profil</label>
        <Select v-model="profile" input-id="prof" :options="profiles" option-label="label" option-value="value" option-disabled="disabled" append-to="self" fluid data-test="btrfs-profile" />
        <p class="hint">{{ BTRFS_PROFILES.find((p) => p.value === profile)?.hint }}</p>
        <Message v-if="reduces" severity="warn" :closable="false" data-test="btrfs-reduces">
          Das neue Profil bietet <strong>weniger Ausfallsicherheit</strong> als {{ profileLabel(pool?.profile) }}.
        </Message>
        <p class="hint">Die Daten werden dafür im Hintergrund umgeschichtet. Das kann Stunden dauern.</p>
      </template>

      <template v-if="needsName">
        <label for="a-confirm">Zur Bestätigung den Poolnamen eingeben</label>
        <InputText id="a-confirm" v-model="confirm" autocomplete="off" fluid data-test="btrfs-action-confirm" />
      </template>
    </div>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button data-test="btrfs-action-submit" :label="kind ? titles[kind].replace(' …', '') : ''" :disabled="!valid || busy" :loading="busy" @click="submit" />
    </template>
  </Dialog>
</template>

<style scoped>
.form {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.form > label {
  font-weight: 600;
  margin-top: 0.4rem;
}
.hint {
  color: var(--bn-text-soft);
  margin: 0;
  font-size: 0.875rem;
}
.check {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
</style>
