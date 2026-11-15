<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Message from 'primevue/message'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
const username = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  error.value = ''
  busy.value = true
  try {
    await auth.login(username.value.trim(), password.value)
    const target = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/'
    await router.replace(target)
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Anmeldung fehlgeschlagen'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="login">
    <form class="card" @submit.prevent="submit">
      <h1><i class="pi pi-server" /> BetterNAS</h1>
      <label for="username">Benutzername</label>
      <InputText id="username" v-model="username" autocomplete="username" autofocus required />
      <label for="password">Passwort</label>
      <Password
        v-model="password"
        input-id="password"
        :feedback="false"
        toggle-mask
        autocomplete="current-password"
        required
        fluid
      />
      <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
      <Button type="submit" label="Anmelden" :loading="busy" :disabled="busy" />
    </form>
  </main>
</template>

<style scoped>
.login {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 16px;
  background: var(--p-surface-100);
}
.card {
  width: min(360px, 100%);
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
  padding: 2rem;
  border-radius: 12px;
  background: var(--p-surface-0);
  box-shadow: 0 4px 24px rgb(0 0 0 / 8%);
}
h1 {
  margin: 0 0 1rem;
  font-size: 1.5rem;
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
label {
  font-size: 0.875rem;
  color: var(--p-text-muted-color);
}
@media (prefers-color-scheme: dark) {
  .login {
    background: var(--p-surface-950);
  }
  .card {
    background: var(--p-surface-900);
  }
}
</style>
