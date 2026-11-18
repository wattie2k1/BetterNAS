<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Checkbox from 'primevue/checkbox'
import { MIN_PASSWORD_LEN, NAME_RE, type GroupView, type UserInput, type UserView } from '@/api/users'

const props = defineProps<{ visible: boolean; groups: GroupView[]; users: UserView[]; user?: UserView | null; busy?: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; submit: [UserInput] }>()

const editing = computed(() => !!props.user)
const name = ref('')
const fullName = ref('')
const password = ref('')
const repeat = ref('')
const groups = ref<string[]>([])
const admin = ref(false)

function reset() {
  name.value = props.user?.name ?? ''
  fullName.value = props.user?.fullName ?? ''
  password.value = ''
  repeat.value = ''
  groups.value = [...(props.user?.groups ?? [])]
  admin.value = props.user?.admin ?? false
}
watch(() => [props.visible, props.user], reset, { immediate: true })

const nameError = computed(() => {
  if (editing.value || name.value === '') return ''
  if (!NAME_RE.test(name.value)) return 'Kleinbuchstaben, Ziffern, _ und -, beginnend mit einem Buchstaben (max. 32 Zeichen).'
  if (props.users.some((u) => u.name === name.value)) return 'Ein Benutzer mit diesem Namen existiert bereits.'
  return ''
})
const passwordError = computed(() => {
  if (editing.value || password.value === '') return ''
  if ([...password.value].length < MIN_PASSWORD_LEN) return `Mindestens ${MIN_PASSWORD_LEN} Zeichen.`
  if (password.value === name.value) return 'Das Passwort darf nicht dem Benutzernamen entsprechen.'
  return ''
})
const repeatError = computed(() => (repeat.value !== '' && repeat.value !== password.value ? 'Die Passwörter stimmen nicht überein.' : ''))
const valid = computed(
  () =>
    editing.value ||
    (name.value !== '' && !nameError.value && password.value !== '' && !passwordError.value && repeat.value === password.value),
)

function submit() {
  if (!valid.value) return
  emit('submit', { name: name.value, fullName: fullName.value, password: password.value, groups: groups.value, admin: admin.value })
}
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="editing ? `Benutzer „${user?.name}“ bearbeiten` : 'Neuer Benutzer'"
    :style="{ width: 'min(520px, 95vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <div class="form">
      <label for="user-name">Benutzername</label>
      <InputText id="user-name" v-model="name" :disabled="editing" autocomplete="off" fluid />
      <small v-if="nameError" class="err">{{ nameError }}</small>

      <label for="user-full">Vollständiger Name (optional)</label>
      <InputText id="user-full" v-model="fullName" autocomplete="off" fluid />

      <template v-if="!editing">
        <label for="user-pw">Passwort</label>
        <Password input-id="user-pw" v-model="password" :feedback="false" toggle-mask autocomplete="new-password" fluid />
        <small v-if="passwordError" class="err">{{ passwordError }}</small>
        <label for="user-pw2">Passwort wiederholen</label>
        <Password input-id="user-pw2" v-model="repeat" :feedback="false" toggle-mask autocomplete="new-password" fluid />
        <small v-if="repeatError" class="err">{{ repeatError }}</small>
      </template>

      <span class="label">Gruppen</span>
      <p v-if="!props.groups.length" class="hint">Noch keine Gruppen angelegt.</p>
      <div v-for="g in props.groups" :key="g.name" class="row" data-test="group-option">
        <Checkbox v-model="groups" :value="g.name" :input-id="`g-${g.name}`" />
        <label :for="`g-${g.name}`">{{ g.name }}</label>
      </div>

      <div class="row admin">
        <Checkbox v-model="admin" binary input-id="user-admin" />
        <label for="user-admin">Administrator (darf BetterNAS verwalten)</label>
      </div>
    </div>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button
        data-test="user-submit"
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
.row {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
.row label {
  font-weight: 400;
}
.admin {
  margin-top: 1rem;
}
.hint {
  color: var(--bn-text-soft);
}
.err {
  color: var(--bn-danger);
}
</style>
