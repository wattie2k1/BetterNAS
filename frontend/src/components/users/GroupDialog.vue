<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Checkbox from 'primevue/checkbox'
import { NAME_RE, type GroupInput, type GroupView, type UserView } from '@/api/users'

const props = defineProps<{ visible: boolean; users: UserView[]; groups: GroupView[]; group?: GroupView | null; busy?: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; submit: [GroupInput] }>()

const editing = computed(() => !!props.group)
const name = ref('')
const members = ref<string[]>([])

watch(
  () => [props.visible, props.group],
  () => {
    name.value = props.group?.name ?? ''
    members.value = [...(props.group?.members ?? [])]
  },
  { immediate: true },
)

const nameError = computed(() => {
  if (editing.value || name.value === '') return ''
  if (!NAME_RE.test(name.value)) return 'Kleinbuchstaben, Ziffern, _ und -, beginnend mit einem Buchstaben (max. 32 Zeichen).'
  if (props.groups.some((g) => g.name === name.value)) return 'Eine Gruppe mit diesem Namen existiert bereits.'
  return ''
})
const valid = computed(() => editing.value || (name.value !== '' && !nameError.value))
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="editing ? `Gruppe „${group?.name}“ bearbeiten` : 'Neue Gruppe'"
    :style="{ width: 'min(480px, 95vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <div class="form">
      <label for="group-name">Name</label>
      <InputText id="group-name" v-model="name" :disabled="editing" autocomplete="off" fluid />
      <small v-if="nameError" class="err">{{ nameError }}</small>

      <span class="label">Mitglieder</span>
      <div v-for="u in users" :key="u.name" class="row" data-test="member-option">
        <Checkbox v-model="members" :value="u.name" :input-id="`m-${u.name}`" />
        <label :for="`m-${u.name}`">{{ u.name }}<span v-if="u.fullName" class="hint"> · {{ u.fullName }}</span></label>
      </div>
    </div>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button
        data-test="group-submit"
        :label="editing ? 'Speichern' : 'Anlegen'"
        :disabled="!valid || busy"
        :loading="busy"
        @click="emit('submit', { name, members })"
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
.row {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
.row label {
  font-weight: 400;
}
.hint {
  color: var(--p-text-muted-color);
}
.err {
  color: var(--p-red-500);
}
</style>
