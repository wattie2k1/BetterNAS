<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Message from 'primevue/message'
import Tag from 'primevue/tag'
import { totpApi, type TotpSetup } from '@/api/totp'

const emit = defineEmits<{ changed: [] }>()

type Mode = 'off' | 'setup' | 'codes' | 'on' | 'disable'

const mode = ref<Mode>('off')
const setup = ref<TotpSetup | null>(null)
const qr = ref('')
const code = ref('')
const recovery = ref<string[]>([])
const busy = ref(false)
const error = ref('')

const msg = (e: unknown) => (e instanceof Error ? e.message : String(e))
const sixDigits = computed(() => /^\d{6}$/.test(code.value))

onMounted(async () => {
  try {
    mode.value = (await totpApi.status()).enabled ? 'on' : 'off'
  } catch (e) {
    error.value = msg(e)
  }
})

async function begin() {
  busy.value = true
  error.value = ''
  try {
    setup.value = await totpApi.setup()
    code.value = ''
    mode.value = 'setup'
    try {
      const QRCode = (await import('qrcode')).default
      qr.value = await QRCode.toDataURL(setup.value.uri, { margin: 1, width: 192 })
    } catch {
      qr.value = '' // ohne QR bleibt das Geheimnis als Text
    }
  } catch (e) {
    error.value = msg(e)
  } finally {
    busy.value = false
  }
}

async function enable() {
  busy.value = true
  error.value = ''
  try {
    recovery.value = (await totpApi.enable(code.value)).recoveryCodes
    code.value = ''
    mode.value = 'codes'
    emit('changed')
  } catch (e) {
    error.value = msg(e)
  } finally {
    busy.value = false
  }
}

async function disable() {
  busy.value = true
  error.value = ''
  try {
    await totpApi.disable(code.value.trim())
    code.value = ''
    mode.value = 'off'
  } catch (e) {
    error.value = msg(e)
  } finally {
    busy.value = false
  }
}

async function copyCodes() {
  try {
    await navigator.clipboard.writeText(recovery.value.join('\n'))
  } catch {
    /* Zwischenablage nicht verfügbar */
  }
}

function downloadCodes() {
  const blob = new Blob([`BetterNAS – Wiederherstellungscodes\n\n${recovery.value.join('\n')}\n`], { type: 'text/plain' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = 'betternas-wiederherstellungscodes.txt'
  a.click()
  URL.revokeObjectURL(a.href)
}

function done() {
  recovery.value = []
  mode.value = 'on'
}
</script>

<template>
  <section class="card glass" data-test="totp-card">
    <h2>Zwei-Faktor-Anmeldung</h2>
    <Message v-if="error" severity="error" :closable="false" data-test="totp-error">{{ error }}</Message>

    <template v-if="mode === 'off'">
      <p class="hint">Schütze deine Anmeldung zusätzlich mit einem Code aus einer Authenticator-App (z. B. Aegis, 2FAS, Google Authenticator).</p>
      <Button label="Einrichten" icon="pi pi-shield" data-test="totp-setup" :loading="busy" @click="begin" />
    </template>

    <template v-else-if="mode === 'setup' && setup">
      <ol class="steps">
        <li>Scanne den QR-Code mit deiner Authenticator-App oder gib das Geheimnis von Hand ein.</li>
        <li>Gib den 6-stelligen Code aus der App zur Bestätigung ein.</li>
      </ol>
      <img v-if="qr" :src="qr" alt="QR-Code für die Authenticator-App" width="192" height="192" data-test="totp-qr" class="qr" />
      <p class="secret"><code data-test="totp-secret">{{ setup.secret }}</code></p>
      <label for="totp-confirm">Code</label>
      <input id="totp-confirm" v-model="code" inputmode="numeric" autocomplete="one-time-code" maxlength="6" placeholder="123456" class="field" />
      <div class="row">
        <Button label="Aktivieren" icon="pi pi-check" data-test="totp-enable" :disabled="!sixDigits || busy" :loading="busy" @click="enable" />
        <Button label="Abbrechen" text @click="mode = 'off'" />
      </div>
    </template>

    <template v-else-if="mode === 'codes'">
      <Message severity="warn" :closable="false">
        Bewahre diese Wiederherstellungscodes sicher auf (z. B. im Passwortmanager). Jeder Code funktioniert einmal, falls du dein Gerät
        verlierst. Sie sind danach nicht mehr einsehbar.
      </Message>
      <ul class="codes">
        <li v-for="c in recovery" :key="c" data-test="recovery-code"><code>{{ c }}</code></li>
      </ul>
      <div class="row">
        <Button label="Kopieren" icon="pi pi-copy" outlined data-test="codes-copy" @click="copyCodes" />
        <Button label="Herunterladen" icon="pi pi-download" outlined @click="downloadCodes" />
        <Button label="Fertig" data-test="codes-done" @click="done" />
      </div>
    </template>

    <template v-else-if="mode === 'on'">
      <p><Tag value="aktiv" severity="success" /> Bei der Anmeldung wird zusätzlich ein Code abgefragt.</p>
      <Button label="Abschalten …" text severity="danger" data-test="totp-disable-open" @click="(mode = 'disable'), (code = ''), (error = '')" />
    </template>

    <template v-else>
      <p class="hint">Zum Abschalten gib einen aktuellen Code aus der App oder einen Wiederherstellungscode ein.</p>
      <label for="totp-disable-code">Code</label>
      <input id="totp-disable-code" v-model="code" autocomplete="one-time-code" class="field" />
      <div class="row">
        <Button label="Abschalten" severity="danger" data-test="totp-disable" :disabled="!code.trim() || busy" :loading="busy" @click="disable" />
        <Button label="Abbrechen" text @click="mode = 'on'" />
      </div>
    </template>
  </section>
</template>

<style scoped>
.card {
  padding: 1.2rem 1.4rem;
  border-radius: 16px;
}
h2 {
  margin: 0 0 0.8rem;
  font-size: 1.1rem;
}
.hint {
  color: var(--p-text-muted-color);
}
.steps {
  padding-left: 1.2rem;
}
.qr {
  display: block;
  background: #fff;
  padding: 6px;
  border-radius: 10px;
}
.secret code {
  letter-spacing: 0.08em;
  word-break: break-all;
}
.field {
  display: block;
  width: 100%;
  max-width: 14rem;
  margin: 0.2rem 0 0.8rem;
  padding: 0.5rem 0.6rem;
  border-radius: 8px;
  border: 1px solid var(--bn-border);
  background: var(--bn-glass);
  color: inherit;
  font: inherit;
}
.row {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
}
.codes {
  columns: 2;
  list-style: none;
  padding: 0;
  margin: 0.6rem 0 1rem;
}
</style>
