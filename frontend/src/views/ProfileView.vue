<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Message from 'primevue/message'
import Password from 'primevue/password'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { MIN_PASSWORD_LEN } from '@/api/users'
import { useAuthStore } from '@/stores/auth'
import { useUsersStore } from '@/stores/users'

const auth = useAuthStore()
const store = useUsersStore()
const toast = useToast()

const password = ref('')
const repeat = ref('')
const busy = ref(false)

onMounted(() => store.refresh())

const me = computed(() => store.users.find((u) => u.name === auth.user) ?? null)
const tooShort = computed(() => password.value !== '' && [...password.value].length < MIN_PASSWORD_LEN)
const mismatch = computed(() => repeat.value !== '' && repeat.value !== password.value)
const valid = computed(() => password.value !== '' && !tooShort.value && repeat.value === password.value && password.value !== auth.user)

async function changePassword() {
  if (!auth.user || !valid.value) return
  busy.value = true
  try {
    await store.setPassword(auth.user, password.value)
    toast.add({ severity: 'success', summary: 'Passwort geändert', life: 4000 })
    password.value = ''
    repeat.value = ''
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="profile">
    <section class="card glass">
      <h2>Account</h2>
      <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>
      <dl>
        <dt>Benutzername</dt>
        <dd data-test="profile-name">{{ auth.user }}</dd>
        <template v-if="me?.fullName">
          <dt>Name</dt>
          <dd>{{ me.fullName }}</dd>
        </template>
        <dt>Rollen und Gruppen</dt>
        <dd class="tags" data-test="profile-groups">
          <Tag v-if="me?.admin" value="Administrator" severity="info" />
          <Tag v-for="g in me?.groups ?? []" :key="g" :value="g" severity="secondary" />
          <span v-if="me && !me.admin && !me.groups.length" class="hint">Keine Gruppen</span>
        </dd>
      </dl>
    </section>

    <section class="card glass">
      <h2>Passwort ändern</h2>
      <form class="form" @submit.prevent="changePassword">
        <label for="profile-pw">Neues Passwort</label>
        <Password input-id="profile-pw" v-model="password" :feedback="false" toggle-mask autocomplete="new-password" fluid />
        <small v-if="tooShort" class="err">Mindestens {{ MIN_PASSWORD_LEN }} Zeichen.</small>
        <label for="profile-pw2">Wiederholen</label>
        <Password input-id="profile-pw2" v-model="repeat" :feedback="false" toggle-mask autocomplete="new-password" fluid />
        <small v-if="mismatch" class="err">Die Passwörter stimmen nicht überein.</small>
        <small class="hint">Das Passwort gilt für die Anmeldung und für Freigaben im Netzwerk.</small>
        <Button data-test="profile-submit" type="submit" label="Passwort ändern" :disabled="!valid || busy" :loading="busy" class="submit" />
      </form>
    </section>
  </div>
</template>

<style scoped>
.profile {
  display: grid;
  gap: 1rem;
  max-width: 640px;
}
.card {
  padding: 1.2rem 1.4rem;
  border-radius: 16px;
}
h2 {
  margin: 0 0 0.8rem;
  font-size: 1.1rem;
}
dl {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 0.5rem 1.2rem;
  margin: 0;
}
dt {
  font-weight: 600;
}
dd {
  margin: 0;
}
.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
}
.form {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.form > label {
  font-weight: 600;
  margin-top: 0.6rem;
}
.submit {
  align-self: flex-start;
  margin-top: 0.8rem;
}
.hint {
  color: var(--p-text-muted-color);
}
.err {
  color: var(--p-red-500);
}
</style>
