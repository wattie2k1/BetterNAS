<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import { api } from '@/api/client'
import { maintenanceApi } from '@/api/security'
import { formatUptime } from '@/utils/format'

interface Info {
  version: string
  hostname: string
  kernel: string
  uptimeSeconds: number
}

const WORD = 'ZURÜCKSETZEN'
const info = ref<Info | null>(null)
const error = ref('')
const asking = ref(false)
const typed = ref('')
const busy = ref(false)
const resetError = ref('')
const done = ref(false)
const canConfirm = computed(() => typed.value === WORD && !busy.value)

onMounted(async () => {
  try {
    info.value = await api.get<Info>('/api/v1/system/info')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
})

async function reset() {
  busy.value = true
  resetError.value = ''
  try {
    await maintenanceApi.factoryReset(typed.value)
    asking.value = false
    done.value = true
    setTimeout(() => window.location.assign('/'), 8000)
  } catch (e) {
    resetError.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div data-test="settings-about">
    <div class="card">
      <h2>Über BetterNAS</h2>
      <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
      <dl v-if="info" class="facts" data-test="about-facts">
        <dt>Version</dt><dd>{{ info.version }}</dd>
        <dt>Hostname</dt><dd>{{ info.hostname }}</dd>
        <dt>Kernel</dt><dd>{{ info.kernel }}</dd>
        <dt>Laufzeit</dt><dd>{{ formatUptime(info.uptimeSeconds) }}</dd>
        <dt>Lizenz</dt><dd>GNU General Public License v3.0</dd>
        <dt>Quellcode</dt><dd><a href="https://github.com/wattie2k1/BetterNAS" target="_blank" rel="noopener">github.com/wattie2k1/BetterNAS</a></dd>
      </dl>
    </div>

    <div class="card danger">
      <h2>Werkseinstellungen</h2>
      <Message v-if="done" severity="success" :closable="false" data-test="reset-done">
        Die Konfiguration wird zurückgesetzt. BetterNAS startet neu – du wirst gleich zur Anmeldung und zum Einrichtungsassistenten geleitet.
      </Message>
      <p class="soft">
        Setzt die Konfiguration von BetterNAS zurück: Freigaben, Pools, Zeitpläne, Benachrichtigungen, 2FA und Einstellungen. Deine Daten auf den Datenträgern, Docker-Container und Systembenutzer bleiben erhalten. Die alte Konfiguration bleibt als Datei auf dem NAS liegen. Eine Sicherung unter „Sicherung“ kannst du danach wiederherstellen.
      </p>
      <Button data-test="reset-open" label="Auf Werkseinstellungen zurücksetzen" icon="pi pi-exclamation-triangle" severity="danger" @click="(asking = true), (typed = ''), (resetError = '')" />
      <Dialog :visible="asking" modal append-to="self" header="Konfiguration zurücksetzen" :style="{ width: 'min(480px, 95vw)' }" @update:visible="(v) => !v && (asking = false)">
        <Message v-if="resetError" severity="error" :closable="false">{{ resetError }}</Message>
        <p>Tippe zur Bestätigung <strong>{{ WORD }}</strong> ein.</p>
        <InputText v-model="typed" data-test="reset-word" autocomplete="off" class="word" :placeholder="WORD" />
        <template #footer>
          <Button label="Abbrechen" text @click="asking = false" />
          <Button data-test="reset-confirm" label="Zurücksetzen" severity="danger" :disabled="!canConfirm" :loading="busy" @click="reset" />
        </template>
      </Dialog>
    </div>
  </div>
</template>

<style scoped>
.card {
  max-width: 760px;
}
.facts {
  display: grid;
  grid-template-columns: 140px 1fr;
  gap: 8px 14px;
  margin: 0;
  font-size: 0.9rem;
}
.facts dt {
  color: var(--bn-text-soft);
}
.facts dd {
  margin: 0;
}
.soft {
  margin: 0 0 14px;
  color: var(--bn-text-soft);
  font-size: 0.875rem;
}
.word {
  width: 100%;
}
</style>
