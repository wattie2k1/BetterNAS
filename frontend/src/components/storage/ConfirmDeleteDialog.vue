<script setup lang="ts">
import { ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'

const props = defineProps<{ visible: boolean; name: string; busy?: boolean; header?: string; text?: string; confirmLabel?: string }>()
const emit = defineEmits<{ 'update:visible': [boolean]; confirm: [] }>()
const typed = ref('')
watch(() => props.visible, () => (typed.value = ''))
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="header ?? 'Pool löschen'"
    :style="{ width: 'min(480px, 95vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <p>{{ text ?? `Der Pool „${name}“ wird entfernt. Die Daten auf den Platten bleiben erhalten.` }}</p>
    <label for="confirm-name">Zur Bestätigung den Poolnamen eingeben:</label>
    <InputText id="confirm-name" v-model="typed" autocomplete="off" fluid />
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button
        data-test="delete-submit"
        :label="confirmLabel ?? 'Löschen'"
        severity="danger"
        :disabled="typed !== name || busy"
        :loading="busy"
        @click="emit('confirm')"
      />
    </template>
  </Dialog>
</template>
