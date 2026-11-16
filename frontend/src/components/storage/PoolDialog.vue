<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Checkbox from 'primevue/checkbox'
import Message from 'primevue/message'
import { MIN_FREE_RE, minFreeBytes, POLICIES, POOL_NAME_RE, type DiskView, type PoolInput, type PoolView } from '@/api/storage'
import { formatBytes } from '@/utils/format'

const props = defineProps<{ visible: boolean; disks: DiskView[]; pools: PoolView[]; pool?: PoolView | null; busy?: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; submit: [PoolInput] }>()

const editing = computed(() => !!props.pool)
const name = ref('')
const branches = ref<string[]>([])
const policy = ref('epmfs')
const minFree = ref('20G')

function reset() {
  name.value = props.pool?.name ?? ''
  branches.value = [...(props.pool?.branches ?? [])]
  policy.value = props.pool?.createPolicy ?? 'epmfs'
  minFree.value = props.pool?.minFree ?? '20G'
}
watch(() => [props.visible, props.pool], reset, { immediate: true })

/** Wählbar: eingehängte Datenplatten ohne Pool – beim Bearbeiten zusätzlich die eigenen. */
const options = computed(() =>
  props.disks.filter(
    (d) => d.role === 'data' && d.state === 'online' && (!d.pool || (editing.value && d.pool === props.pool?.name)),
  ),
)

const nameError = computed(() => {
  if (editing.value || name.value === '') return ''
  if (!POOL_NAME_RE.test(name.value)) return 'Nur Kleinbuchstaben, Ziffern und Bindestrich, beginnend mit einem Buchstaben (max. 32 Zeichen).'
  if (props.pools.some((p) => p.name === name.value)) return 'Ein Pool mit diesem Namen existiert bereits.'
  return ''
})
const minFreeError = computed(() => (MIN_FREE_RE.test(minFree.value) ? '' : 'Zahl mit K, M oder G, z. B. 20G'))
const valid = computed(
  () => name.value !== '' && !nameError.value && !minFreeError.value && branches.value.length > 0,
)
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
      <label for="pool-name">Name</label>
      <InputText id="pool-name" v-model="name" :disabled="editing" placeholder="z. B. medien" fluid />
      <small v-if="nameError" class="err">{{ nameError }}</small>
      <small v-else-if="!editing" class="hint">Erreichbar unter /srv/pools/{{ name || '…' }}</small>

      <span class="label">Datenträger</span>
      <p v-if="!options.length" class="hint">
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
    </div>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button
        data-test="pool-submit"
        :label="editing ? 'Speichern' : 'Anlegen'"
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
.branch {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
.hint {
  color: var(--p-text-muted-color);
}
.err {
  color: var(--p-red-500);
}
</style>
