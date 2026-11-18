<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import Password from 'primevue/password'
import { MIN_PASSWORD_LEN } from '@/api/users'

const props = defineProps<{ visible: boolean; user: string; busy?: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; submit: [string] }>()
const password = ref('')
const repeat = ref('')
watch(() => props.visible, () => ((password.value = ''), (repeat.value = '')))

const tooShort = computed(() => password.value !== '' && [...password.value].length < MIN_PASSWORD_LEN)
const mismatch = computed(() => repeat.value !== '' && repeat.value !== password.value)
const valid = computed(() => password.value !== '' && !tooShort.value && repeat.value === password.value && password.value !== props.user)
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="`Passwort für „${user}“ ändern`"
    :style="{ width: 'min(480px, 95vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <div class="form">
      <label for="pw-new">Neues Passwort</label>
      <Password input-id="pw-new" v-model="password" :feedback="false" toggle-mask autocomplete="new-password" fluid />
      <small v-if="tooShort" class="err">Mindestens {{ MIN_PASSWORD_LEN }} Zeichen.</small>
      <label for="pw-repeat">Wiederholen</label>
      <Password input-id="pw-repeat" v-model="repeat" :feedback="false" toggle-mask autocomplete="new-password" fluid />
      <small v-if="mismatch" class="err">Die Passwörter stimmen nicht überein.</small>
      <small class="hint">Das Passwort gilt für die Anmeldung und für Freigaben im Netzwerk.</small>
    </div>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button data-test="password-submit" label="Ändern" :disabled="!valid || busy" :loading="busy" @click="emit('submit', password)" />
    </template>
  </Dialog>
</template>

<style scoped>
.form {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.form > label {
  font-weight: 600;
  margin-top: 0.6rem;
}
.hint {
  color: var(--bn-text-soft);
  margin-top: 0.6rem;
}
.err {
  color: var(--bn-danger);
}
</style>
