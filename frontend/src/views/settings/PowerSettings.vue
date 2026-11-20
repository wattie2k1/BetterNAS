<script setup lang="ts">
import { ref } from 'vue'
import Button from 'primevue/button'
import Message from 'primevue/message'
import type { PowerAction } from '@/api/system'
import PowerDialog from '@/components/PowerDialog.vue'

const action = ref<PowerAction | null>(null)
const done = ref('')

function onDone(a: PowerAction) {
  done.value = a === 'reboot' ? 'Das NAS startet in wenigen Sekunden neu und ist nach etwa einer Minute wieder erreichbar.' : 'Das NAS fährt in wenigen Sekunden herunter. Zum Einschalten brauchst du den Netzschalter.'
}
</script>

<template>
  <div class="card" data-test="settings-power">
    <h2>Energie</h2>
    <Message v-if="done" severity="success" :closable="false" data-test="power-done">{{ done }}</Message>
    <p class="soft">Laufende virtuelle Maschinen und Docker-Container werden beim Neustart oder Herunterfahren vom System beendet. Aktive Dateiübertragungen brechen ab.</p>
    <div class="actions">
      <Button data-test="power-reboot" label="Neu starten" icon="pi pi-refresh" severity="secondary" @click="action = 'reboot'" />
      <Button data-test="power-poweroff" label="Herunterfahren" icon="pi pi-power-off" severity="danger" @click="action = 'poweroff'" />
    </div>
    <PowerDialog v-model="action" @done="onDone" />
  </div>
</template>

<style scoped>
.card {
  max-width: 760px;
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
</style>
