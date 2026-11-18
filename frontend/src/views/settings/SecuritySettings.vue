<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import InputNumber from 'primevue/inputnumber'
import Message from 'primevue/message'
import ToggleSwitch from 'primevue/toggleswitch'
import { useToast } from 'primevue/usetoast'
import { securityApi, type AuthPolicy, type SessionInfo } from '@/api/security'
import { formatDateTime } from '@/utils/format'

const toast = useToast()
const loaded = ref<AuthPolicy | null>(null)
const form = ref<AuthPolicy>({ sessionHours: 12, maxFailures: 5, lockoutMinutes: 5, requireTotp: false })
const sessions = ref<SessionInfo[]>([])
const error = ref('')
const sessionError = ref('')
const busy = ref(false)

const dirty = computed(() => !!loaded.value && JSON.stringify(loaded.value) !== JSON.stringify(form.value))
const others = computed(() => sessions.value.filter((s) => !s.current).length)

async function load() {
  try {
    const p = await securityApi.policy()
    loaded.value = p
    form.value = { ...p }
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
  await loadSessions()
}

async function loadSessions() {
  try {
    sessions.value = await securityApi.sessions()
    sessionError.value = ''
  } catch (e) {
    sessionError.value = e instanceof Error ? e.message : String(e)
  }
}
onMounted(load)

async function save() {
  busy.value = true
  error.value = ''
  try {
    await securityApi.setPolicy({ ...form.value })
    loaded.value = { ...form.value }
    toast.add({ severity: 'success', summary: 'Anmelde-Einstellungen gespeichert', life: 4000 })
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function revoke(s: SessionInfo) {
  try {
    await securityApi.revoke(s.id)
    await loadSessions()
  } catch (e) {
    sessionError.value = e instanceof Error ? e.message : String(e)
  }
}

async function revokeOthers() {
  try {
    const r = await securityApi.revokeOthers()
    toast.add({ severity: 'info', summary: `${r.revoked} Sitzung(en) beendet`, life: 4000 })
    await loadSessions()
  } catch (e) {
    sessionError.value = e instanceof Error ? e.message : String(e)
  }
}
</script>

<template>
  <div data-test="settings-security">
    <div class="card">
      <h2>Anmeldung</h2>
      <Message v-if="error" severity="error" :closable="false" data-test="security-error">{{ error }}</Message>
      <form v-if="loaded" class="grid" @submit.prevent="save">
        <label for="s-hours">Sitzungsdauer (Stunden)</label>
        <InputNumber v-model="form.sessionHours" input-id="s-hours" :min="1" :max="720" show-buttons />
        <label for="s-fail">Fehlversuche bis zur Sperre</label>
        <InputNumber v-model="form.maxFailures" input-id="s-fail" :min="3" :max="20" show-buttons />
        <label for="s-lock">Sperrzeit (Minuten)</label>
        <InputNumber v-model="form.lockoutMinutes" input-id="s-lock" :min="1" :max="60" show-buttons />
        <label for="s-totp">Zwei-Faktor-Anmeldung für alle verlangen</label>
        <span class="inline">
          <ToggleSwitch v-model="form.requireTotp" input-id="s-totp" data-test="security-require-totp" />
          <span class="soft">Wer noch keine hat, muss sie nach der Anmeldung zuerst im Profil einrichten.</span>
        </span>
        <span />
        <Button type="submit" data-test="security-save" label="Speichern" icon="pi pi-check" :disabled="!dirty" :loading="busy" class="save" />
      </form>
      <p class="soft note">Die Sperre gilt je Client-Adresse. Eine geänderte Sitzungsdauer gilt auch für bereits angemeldete Sitzungen.</p>
    </div>

    <div class="card">
      <div class="head">
        <h2>Aktive Sitzungen</h2>
        <Button data-test="sessions-revoke-others" label="Alle anderen beenden" icon="pi pi-sign-out" severity="secondary" size="small" :disabled="others === 0" @click="revokeOthers" />
      </div>
      <Message v-if="sessionError" severity="error" :closable="false">{{ sessionError }}</Message>
      <table v-if="sessions.length" class="table" data-test="sessions-table">
        <thead>
          <tr><th>Benutzer</th><th>Angemeldet</th><th>Adresse</th><th /></tr>
        </thead>
        <tbody>
          <tr v-for="s in sessions" :key="s.id" :data-test="`session-${s.id}`">
            <td>{{ s.username }} <span v-if="s.current" class="badge">diese Sitzung</span></td>
            <td>{{ s.createdAt ? formatDateTime(s.createdAt) : '–' }}</td>
            <td>{{ s.ip || '–' }}</td>
            <td class="right">
              <Button v-if="!s.current" :data-test="`session-revoke-${s.id}`" label="Beenden" text size="small" severity="danger" @click="revoke(s)" />
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.card {
  max-width: 760px;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 6px;
}
.grid {
  display: grid;
  grid-template-columns: minmax(180px, 280px) 1fr;
  gap: 12px 14px;
  align-items: center;
}
.inline {
  display: flex;
  align-items: center;
  gap: 10px;
}
.soft {
  color: var(--bn-text-soft);
  font-size: 0.85rem;
}
.note {
  margin: 14px 0 0;
}
.save {
  justify-self: start;
}
.table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.875rem;
}
.table th {
  text-align: left;
  font-weight: 600;
  color: var(--bn-text-soft);
  padding: 6px 8px;
}
.table td {
  padding: 6px 8px;
  border-top: 1px solid var(--bn-border, rgba(128, 128, 128, 0.2));
}
.right {
  text-align: right;
}
.badge {
  margin-left: 6px;
  padding: 1px 8px;
  border-radius: 999px;
  font-size: 0.72rem;
  background: color-mix(in srgb, var(--bn-accent-1) 18%, transparent);
}
@media (max-width: 600px) {
  .grid {
    grid-template-columns: 1fr;
  }
}
</style>
