<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import Button from 'primevue/button'
import InputNumber from 'primevue/inputnumber'
import Message from 'primevue/message'
import Select from 'primevue/select'
import Textarea from 'primevue/textarea'
import ToggleSwitch from 'primevue/toggleswitch'
import { useToast } from 'primevue/usetoast'
import { sshApi, type SshKey, type SshSettings } from '@/api/firewall'
import { usersApi } from '@/api/users'

const toast = useToast()
const loaded = ref<SshSettings | null>(null)
const port = ref(22)
const passwordAuth = ref(true)
const rootLogin = ref(false)
const error = ref('')
const busy = ref(false)

const users = ref<string[]>([])
const user = ref('')
const keys = ref<SshKey[]>([])
const newKey = ref('')
const keyError = ref('')

const dirty = computed(
  () => !!loaded.value && (port.value !== loaded.value.port || passwordAuth.value !== loaded.value.passwordAuth || rootLogin.value !== (loaded.value.permitRootLogin === 'prohibit-password')),
)

function fill(s: SshSettings) {
  loaded.value = s
  port.value = s.port
  passwordAuth.value = s.passwordAuth
  rootLogin.value = s.permitRootLogin === 'prohibit-password'
}

async function load() {
  try {
    fill(await sshApi.get())
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

async function loadKeys() {
  if (!user.value) return
  try {
    keys.value = await sshApi.keys(user.value)
    keyError.value = ''
  } catch (e) {
    keyError.value = e instanceof Error ? e.message : String(e)
  }
}

onMounted(async () => {
  void load()
  try {
    users.value = (await usersApi.users()).map((u) => u.name)
    user.value = users.value[0] ?? ''
  } catch (e) {
    keyError.value = e instanceof Error ? e.message : String(e)
  }
})
watch(user, loadKeys)

async function save() {
  busy.value = true
  error.value = ''
  try {
    await sshApi.set({ port: port.value, passwordAuth: passwordAuth.value, permitRootLogin: rootLogin.value ? 'prohibit-password' : 'no' })
    toast.add({ severity: 'success', summary: 'SSH-Einstellungen gespeichert', life: 3000 })
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function addKey() {
  keyError.value = ''
  try {
    await sshApi.addKey(user.value, newKey.value.trim())
    newKey.value = ''
    await loadKeys()
  } catch (e) {
    keyError.value = e instanceof Error ? e.message : String(e)
  }
}

async function removeKey(k: SshKey) {
  keyError.value = ''
  try {
    await sshApi.removeKey(user.value, k.fingerprint)
    await loadKeys()
  } catch (e) {
    keyError.value = e instanceof Error ? e.message : String(e)
  }
}
</script>

<template>
  <div class="stack" data-test="settings-ssh">
    <section class="card">
      <h3>SSH-Server</h3>
      <Message v-if="error" severity="error" :closable="false" data-test="ssh-error">{{ error }}</Message>
      <Message v-if="loaded && !loaded.active" severity="warn" :closable="false" data-test="ssh-inactive">
        Der SSH-Dienst läuft nicht. Starte ihn unter „Dienste“; die Einstellungen gelten dann beim Start.
      </Message>
      <form v-if="loaded" class="grid" @submit.prevent="save">
        <label for="ssh-port">Port</label>
        <div>
          <InputNumber v-model="port" input-id="ssh-port" :min="1" :max="65535" :use-grouping="false" :disabled="loaded.socketActivated" data-test="ssh-port" />
          <div v-if="loaded.socketActivated" class="soft">SSH wird über ssh.socket gestartet; der Port ist hier nicht änderbar.</div>
        </div>
        <label for="ssh-pw">Anmeldung mit Passwort erlauben</label>
        <ToggleSwitch v-model="passwordAuth" input-id="ssh-pw" data-test="ssh-password" />
        <label for="ssh-root">Anmeldung als root (nur mit Schlüssel)</label>
        <ToggleSwitch v-model="rootLogin" input-id="ssh-root" data-test="ssh-root" />
        <span />
        <Button type="submit" data-test="ssh-save" label="Speichern" icon="pi pi-check" :disabled="!dirty" :loading="busy" class="save" />
      </form>
      <Message severity="info" :closable="false">
        Die Passwort-Anmeldung lässt sich nur abschalten, wenn mindestens ein Schlüssel hinterlegt ist. Bestehende Verbindungen bleiben beim Speichern offen. Ändert sich der Port, folgt die Firewall-Regel automatisch.
      </Message>
    </section>

    <section class="card">
      <h3>Öffentliche Schlüssel</h3>
      <Message v-if="keyError" severity="error" :closable="false" data-test="ssh-key-error">{{ keyError }}</Message>
      <div class="grid">
        <label for="ssh-user">Benutzer</label>
        <Select v-model="user" input-id="ssh-user" :options="users" data-test="ssh-user" />
      </div>
      <ul v-if="keys.length" class="keys" data-test="ssh-keys">
        <li v-for="k in keys" :key="k.fingerprint">
          <div>
            <strong>{{ k.comment || 'ohne Kommentar' }}</strong> <span class="soft">{{ k.type }}</span>
            <div class="soft fp">{{ k.fingerprint }}</div>
          </div>
          <Button icon="pi pi-trash" severity="danger" text :aria-label="`Schlüssel ${k.comment || k.fingerprint} entfernen`" data-test="ssh-key-remove" @click="removeKey(k)" />
        </li>
      </ul>
      <p v-else-if="user" class="soft" data-test="ssh-no-keys">Für {{ user }} ist kein Schlüssel hinterlegt.</p>
      <form class="add" @submit.prevent="addKey">
        <Textarea v-model="newKey" rows="3" placeholder="ssh-ed25519 AAAA… name@rechner" spellcheck="false" aria-label="Öffentlicher Schlüssel" data-test="ssh-key-input" />
        <Button type="submit" data-test="ssh-key-add" label="Schlüssel hinzufügen" icon="pi pi-plus" :disabled="!newKey.trim() || !user" />
      </form>
    </section>
  </div>
</template>

<style scoped>
.stack {
  display: flex;
  flex-direction: column;
  gap: 18px;
  max-width: 760px;
}
.card {
  padding: 20px 22px;
}
h3 {
  margin: 0 0 12px;
  font-size: 1rem;
}
.grid {
  display: grid;
  grid-template-columns: minmax(200px, 300px) 1fr;
  gap: 12px 14px;
  align-items: center;
  margin-bottom: 14px;
}
.soft {
  color: var(--bn-text-soft);
  font-size: 0.85rem;
}
.save {
  justify-self: start;
}
.keys {
  list-style: none;
  margin: 0 0 14px;
  padding: 0;
}
.keys li {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  padding: 6px 0;
}
.fp {
  font-family: ui-monospace, monospace;
  word-break: break-all;
}
.add {
  display: flex;
  flex-direction: column;
  gap: 10px;
  align-items: flex-start;
}
.add :deep(textarea) {
  width: 100%;
}
@media (max-width: 600px) {
  .grid {
    grid-template-columns: 1fr;
  }
}
</style>
