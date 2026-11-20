<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import { systemApi, type PowerAction } from '@/api/system'
import { usePowerStore } from '@/stores/power'

/** Bestätigungsdialog für Neustart und Herunterfahren (das Wort muss abgetippt werden). */
const action = defineModel<PowerAction | null>({ required: true })
const emit = defineEmits<{ done: [PowerAction] }>()

const toast = useToast()
const power = usePowerStore()
const WORDS: Record<PowerAction, string> = { reboot: 'NEUSTARTEN', poweroff: 'HERUNTERFAHREN' }
const typed = ref('')
const busy = ref(false)
const error = ref('')
const word = computed(() => (action.value ? WORDS[action.value] : ''))

watch(action, () => {
  typed.value = ''
  error.value = ''
})

async function run() {
  if (!action.value) return
  busy.value = true
  try {
    const chosen = action.value
    await systemApi.power(chosen, typed.value)
    power.begin(chosen)
    toast.add({ severity: 'info', summary: chosen === 'reboot' ? 'Neustart angefordert' : 'Herunterfahren angefordert', life: 6000 })
    action.value = null
    emit('done', chosen)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Dialog :visible="!!action" modal append-to="self" :header="action === 'reboot' ? 'NAS neu starten' : 'NAS herunterfahren'" :style="{ width: 'min(480px, 95vw)' }" @update:visible="(v) => !v && (action = null)">
    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
    <p>Tippe zur Bestätigung <strong>{{ word }}</strong> ein.</p>
    <InputText v-model="typed" data-test="power-word" autocomplete="off" class="word" :placeholder="word" @keyup.enter="typed === word && !busy && run()" />
    <template #footer>
      <Button label="Abbrechen" text @click="action = null" />
      <Button data-test="power-confirm" :label="action === 'reboot' ? 'Neu starten' : 'Herunterfahren'" severity="danger" :disabled="typed !== word || busy" :loading="busy" @click="run" />
    </template>
  </Dialog>
</template>

<style scoped>
.word {
  width: 100%;
}
</style>
