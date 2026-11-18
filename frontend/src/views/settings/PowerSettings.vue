<script setup lang="ts">
import { computed, ref } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import { systemApi, type PowerAction } from '@/api/system'

const toast = useToast()
const WORDS: Record<PowerAction, string> = { reboot: 'NEUSTARTEN', poweroff: 'HERUNTERFAHREN' }
const action = ref<PowerAction | null>(null)
const typed = ref('')
const busy = ref(false)
const error = ref('')
const done = ref('')
const word = computed(() => (action.value ? WORDS[action.value] : ''))

function ask(a: PowerAction) {
  action.value = a
  typed.value = ''
  error.value = ''
}

async function run() {
  if (!action.value) return
  busy.value = true
  try {
    await systemApi.power(action.value, typed.value)
    done.value = action.value === 'reboot' ? 'Das NAS startet in wenigen Sekunden neu und ist nach etwa einer Minute wieder erreichbar.' : 'Das NAS fährt in wenigen Sekunden herunter. Zum Einschalten brauchst du den Netzschalter.'
    toast.add({ severity: 'info', summary: action.value === 'reboot' ? 'Neustart angefordert' : 'Herunterfahren angefordert', life: 6000 })
    action.value = null
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="card" data-test="settings-power">
    <h3>Energie</h3>
    <Message v-if="done" severity="success" :closable="false" data-test="power-done">{{ done }}</Message>
    <p class="soft">Laufende virtuelle Maschinen und Docker-Container werden beim Neustart oder Herunterfahren vom System beendet. Aktive Dateiübertragungen brechen ab.</p>
    <div class="actions">
      <Button data-test="power-reboot" label="Neu starten" icon="pi pi-refresh" severity="secondary" @click="ask('reboot')" />
      <Button data-test="power-poweroff" label="Herunterfahren" icon="pi pi-power-off" severity="danger" @click="ask('poweroff')" />
    </div>
    <Dialog :visible="!!action" modal append-to="self" :header="action === 'reboot' ? 'NAS neu starten' : 'NAS herunterfahren'" :style="{ width: 'min(480px, 95vw)' }" @update:visible="(v) => !v && (action = null)">
      <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
      <p>Tippe zur Bestätigung <strong>{{ word }}</strong> ein.</p>
      <InputText v-model="typed" data-test="power-word" autocomplete="off" class="word" :placeholder="word" />
      <template #footer>
        <Button label="Abbrechen" text @click="action = null" />
        <Button data-test="power-confirm" :label="action === 'reboot' ? 'Neu starten' : 'Herunterfahren'" severity="danger" :disabled="typed !== word || busy" :loading="busy" @click="run" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.card {
  padding: 20px 22px;
  max-width: 760px;
}
h3 {
  margin: 0 0 12px;
  font-size: 1rem;
}
.soft {
  margin: 0 0 14px;
  color: var(--bn-text-soft);
  font-size: 0.875rem;
}
.actions {
  display: flex;
  gap: 0.5rem;
}
.word {
  width: 100%;
}
</style>
