<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import Button from 'primevue/button'
import Message from 'primevue/message'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { useNotificationsStore } from '@/stores/notifications'
import type { MailConfig, NotifyConfig, NtfyConfig } from '@/api/notifications'
import { formatDateTime } from '@/utils/format'

const store = useNotificationsStore()
const toast = useToast()

const mail = ref<MailConfig | null>(null)
const ntfy = ref<NtfyConfig | null>(null)
const tempWarnC = ref(55)
const recipients = ref('')
const password = ref('')
const token = ref('')
const saving = ref(false)
const results = ref<Record<string, { ok: boolean; text: string }>>({})

/** Zustand zum Zeitpunkt des Ladens/Speicherns, um ungespeicherte Änderungen zu erkennen. */
const saved = ref('')

function snapshot() {
  return JSON.stringify([mail.value, ntfy.value, tempWarnC.value, recipients.value, password.value, token.value])
}

function adopt(c: NotifyConfig) {
  mail.value = { ...c.mail }
  ntfy.value = { ...c.ntfy }
  tempWarnC.value = c.tempWarnC
  recipients.value = c.mail.to.join(', ')
  password.value = ''
  token.value = ''
  saved.value = snapshot()
}

onMounted(async () => {
  await Promise.all([store.loadConfig(), store.loadHistory(), store.loadAlerts()])
  if (store.config) adopt(store.config)
})
watch(() => store.config, (c) => c && !mail.value && adopt(c))

const dirty = computed(() => snapshot() !== saved.value)

function payload(): NotifyConfig {
  return {
    mail: {
      ...mail.value!,
      password: password.value,
      to: recipients.value.split(/[,;\s]+/).filter(Boolean),
    },
    ntfy: { ...ntfy.value!, token: token.value },
    tempWarnC: Number(tempWarnC.value),
  }
}

async function save() {
  saving.value = true
  try {
    await store.save(payload())
    adopt(store.config!)
    toast.add({ severity: 'success', summary: 'Gespeichert', life: 4000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Speichern fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    saving.value = false
  }
}

async function test(channel: 'mail' | 'ntfy') {
  results.value[channel] = { ok: true, text: 'Wird gesendet …' }
  try {
    await store.test(channel)
    results.value[channel] = { ok: true, text: 'Testnachricht wurde gesendet.' }
  } catch (e) {
    results.value[channel] = { ok: false, text: e instanceof Error ? e.message : String(e) }
  }
  void store.loadHistory()
}

const levelLabel = { info: 'Info', warn: 'Warnung', crit: 'Kritisch' } as const
const levelSeverity = { info: 'info', warn: 'warn', crit: 'danger' } as const
const channelLabel = { mail: 'Mail', ntfy: 'ntfy' } as const
</script>

<template>
  <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>

  <section class="card">
    <h2>Aktive Meldungen</h2>
    <p v-if="!store.alerts.length" class="ok" data-test="alerts-none"><i class="pi pi-check-circle" /> Alles in Ordnung.</p>
    <ul v-else class="alerts">
      <li v-for="a in store.alerts" :key="a.key" data-test="alert-item">
        <Tag :value="levelLabel[a.level]" :severity="levelSeverity[a.level]" />
        <div>
          <strong>{{ a.title }}</strong>
          <div class="sub">{{ a.body }} · seit {{ formatDateTime(a.since) }}</div>
        </div>
      </li>
    </ul>
  </section>

  <template v-if="mail && ntfy">
    <section class="card">
      <header class="head">
        <h2>Mail</h2>
        <label class="switch"><input v-model="mail.enabled" type="checkbox" data-test="mail-enabled" /> aktiv</label>
      </header>
      <div class="grid">
        <div class="field"><label for="mail-host">SMTP-Server</label><input id="mail-host" v-model="mail.host" type="text" autocomplete="off" /></div>
        <div class="field"><label for="mail-port">Port</label><input id="mail-port" v-model.number="mail.port" type="number" min="1" max="65535" /></div>
        <div class="field">
          <label for="mail-security">Verschlüsselung</label>
          <select id="mail-security" v-model="mail.security">
            <option value="starttls">STARTTLS (587)</option>
            <option value="tls">TLS (465)</option>
            <option value="none">keine</option>
          </select>
        </div>
        <div class="field"><label for="mail-username">Benutzername</label><input id="mail-username" v-model="mail.username" type="text" autocomplete="off" /></div>
        <div class="field">
          <label for="mail-password">Passwort</label>
          <input id="mail-password" v-model="password" type="password" autocomplete="new-password" :placeholder="mail.passwordSet ? 'gespeichert – leer lassen zum Behalten' : ''" />
        </div>
        <div class="field"><label for="mail-from">Absender</label><input id="mail-from" v-model="mail.from" type="email" /></div>
        <div class="field wide"><label for="mail-to">Empfänger (mit Komma trennen)</label><input id="mail-to" v-model="recipients" type="text" /></div>
      </div>
      <div class="actions">
        <Button label="Testnachricht senden" icon="pi pi-send" outlined size="small" data-test="mail-test" :disabled="dirty || !mail.enabled" @click="test('mail')" />
        <span v-if="results.mail" class="result" :class="{ bad: !results.mail.ok }" data-test="mail-test-result">{{ results.mail.text }}</span>
      </div>
    </section>

    <section class="card">
      <header class="head">
        <h2>ntfy</h2>
        <label class="switch"><input v-model="ntfy.enabled" type="checkbox" data-test="ntfy-enabled" /> aktiv</label>
      </header>
      <div class="grid">
        <div class="field"><label for="ntfy-server">Server</label><input id="ntfy-server" v-model="ntfy.server" type="text" placeholder="https://ntfy.sh" /></div>
        <div class="field"><label for="ntfy-topic">Topic</label><input id="ntfy-topic" v-model="ntfy.topic" type="text" autocomplete="off" /></div>
        <div class="field">
          <label for="ntfy-token">Zugriffstoken (optional)</label>
          <input id="ntfy-token" v-model="token" type="password" autocomplete="new-password" :placeholder="ntfy.tokenSet ? 'gespeichert – leer lassen zum Behalten' : ''" />
        </div>
      </div>
      <div class="actions">
        <Button label="Testnachricht senden" icon="pi pi-send" outlined size="small" data-test="ntfy-test" :disabled="dirty || !ntfy.enabled" @click="test('ntfy')" />
        <span v-if="results.ntfy" class="result" :class="{ bad: !results.ntfy.ok }" data-test="ntfy-test-result">{{ results.ntfy.text }}</span>
      </div>
    </section>

    <section class="card">
      <h2>Schwellwerte</h2>
      <div class="field narrow">
        <label for="temp-warn">Temperatur-Warnung ab (°C)</label>
        <input id="temp-warn" v-model.number="tempWarnC" type="number" min="30" max="90" />
      </div>
    </section>

    <div class="save">
      <Button label="Speichern" icon="pi pi-check" data-test="notify-save" :loading="saving" :disabled="!dirty" @click="save" />
    </div>
  </template>

  <section class="card">
    <h2>Verlauf</h2>
    <p v-if="!store.history.length" class="sub">Noch keine Meldungen verschickt.</p>
    <table v-else class="hist">
      <thead><tr><th>Zeit</th><th>Stufe</th><th>Meldung</th><th>Zustellung</th></tr></thead>
      <tbody>
        <tr v-for="h in store.history" :key="h.id" data-test="history-row">
          <td>{{ formatDateTime(h.ts) }}</td>
          <td><Tag :value="levelLabel[h.level]" :severity="levelSeverity[h.level]" /></td>
          <td>{{ h.title }}</td>
          <td>
            <span v-if="!h.results.length" class="sub">kein Kanal aktiv</span>
            <span
              v-for="r in h.results"
              :key="r.channel"
              class="res"
              :class="{ bad: !r.ok }"
              :data-test="`result-${r.channel}`"
              :data-ok="String(r.ok)"
              :title="r.error"
            >
              {{ channelLabel[r.channel] }} {{ r.ok ? '✓' : '✗' }}
            </span>
          </td>
        </tr>
      </tbody>
    </table>
  </section>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.switch {
  display: flex;
  gap: 0.4rem;
  align-items: center;
  font-size: 0.9rem;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 0.8rem 1rem;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}
.field.wide {
  grid-column: 1 / -1;
}
.field.narrow {
  max-width: 14rem;
}
.field label {
  font-size: 0.8rem;
  color: var(--bn-text-soft);
}
.actions {
  display: flex;
  gap: 0.8rem;
  align-items: center;
  margin-top: 0.9rem;
  flex-wrap: wrap;
}
.result {
  font-size: 0.85rem;
  color: var(--bn-ok);
}
.result.bad,
.res.bad {
  color: var(--bn-danger);
}
.res {
  margin-right: 0.6rem;
  color: var(--bn-ok);
  font-size: 0.85rem;
}
.save {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 1rem;
}
.sub {
  color: var(--bn-text-soft);
  font-size: 0.85rem;
}
.ok {
  color: var(--bn-ok);
  font-weight: 600;
  margin: 0;
}
.alerts {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}
.alerts li {
  display: flex;
  gap: 0.7rem;
  align-items: flex-start;
}
.hist {
  width: 100%;
  border-collapse: collapse;
}
th,
td {
  text-align: left;
  padding: 0.45rem 0.6rem;
  border-bottom: 1px solid var(--bn-border-soft);
  vertical-align: top;
}
th {
  font-size: 0.8rem;
  color: var(--bn-text-soft);
}
</style>
