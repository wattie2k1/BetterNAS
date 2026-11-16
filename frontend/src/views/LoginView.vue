<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Message from 'primevue/message'
import { useAuthStore } from '@/stores/auth'
import { safeNext } from '@/utils/redirect'

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
    await router.replace(safeNext(route.query.next))
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Anmeldung fehlgeschlagen'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="login">
    <form class="card glass" @submit.prevent="submit">
      <div class="logo"><i class="pi pi-server" /></div>
      <h1>BetterNAS</h1>
      <p class="tag">Melde dich an, um deinen Server zu verwalten.</p>
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
}
.card {
  width: min(400px, 100%);
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
  padding: 2.4rem 2.2rem 2.2rem;
  border-radius: 30px;
}
.logo {
  width: 64px;
  height: 64px;
  border-radius: 21px;
  display: grid;
  place-items: center;
  color: #fff;
  font-size: 1.8rem;
  background: linear-gradient(135deg, var(--bn-accent-1), var(--bn-accent-3));
  box-shadow: 0 14px 32px -10px rgb(139 92 246 / 75%);
}
h1 {
  margin: 0.8rem 0 0;
  font-size: 1.8rem;
  letter-spacing: -0.03em;
}
.tag {
  margin: 0 0 1rem;
  color: var(--bn-text-soft);
}
label {
  font-size: 0.85rem;
  font-weight: 600;
  color: var(--bn-text-soft);
}
</style>
