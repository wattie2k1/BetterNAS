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
/** Gesetzt, sobald das Passwort stimmt und ein zweiter Faktor verlangt wird. */
const mfaToken = ref<string | null>(null)
const code = ref('')
const useRecovery = ref(false)

function back() {
  mfaToken.value = null
  code.value = ''
  useRecovery.value = false
  error.value = ''
}

async function submit() {
  error.value = ''
  busy.value = true
  try {
    if (mfaToken.value) {
      await auth.loginTotp(mfaToken.value, code.value.trim())
    } else {
      const token = await auth.login(username.value.trim(), password.value)
      if (token) {
        mfaToken.value = token
        password.value = ''
        return
      }
    }
    await router.replace(safeNext(route.query.next))
  } catch (e) {
    const msg = e instanceof Error ? e.message : 'Anmeldung fehlgeschlagen'
    if (mfaToken.value && /erneut anmelden/i.test(msg)) back() // Token abgelaufen oder gesperrt
    error.value = msg
    code.value = ''
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
      <template v-if="!mfaToken">
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
      </template>
      <template v-else>
        <label for="totp-code">{{ useRecovery ? 'Wiederherstellungscode' : 'Code aus deiner Authenticator-App' }}</label>
        <InputText
          id="totp-code"
          v-model="code"
          autocomplete="one-time-code"
          :inputmode="useRecovery ? 'text' : 'numeric'"
          :placeholder="useRecovery ? 'XXXXX-XXXXX' : '123456'"
          autofocus
          required
        />
        <button type="button" class="link" data-test="use-recovery" @click="useRecovery = !useRecovery; code = ''">
          {{ useRecovery ? 'Code aus der App verwenden' : 'Wiederherstellungscode verwenden' }}
        </button>
        <button type="button" class="link" data-test="totp-back" @click="back">Zurück</button>
      </template>
      <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
      <Button type="submit" :label="mfaToken ? 'Bestätigen' : 'Anmelden'" :loading="busy" :disabled="busy" />
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
.link {
  all: unset;
  align-self: flex-start;
  cursor: pointer;
  font-size: 0.85rem;
  color: var(--p-primary-color);
}
.link:focus-visible {
  outline: 2px solid var(--p-primary-color);
  outline-offset: 2px;
}
label {
  font-size: 0.85rem;
  font-weight: 600;
  color: var(--bn-text-soft);
}
</style>
