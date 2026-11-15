<script setup lang="ts">
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'

defineProps<{ visible: boolean; header: string; message: string; confirmLabel?: string; busy?: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; confirm: [] }>()
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="header"
    :style="{ width: 'min(480px, 95vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <p>{{ message }}</p>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button
        data-test="confirm-submit"
        :label="confirmLabel ?? 'Löschen'"
        severity="danger"
        :disabled="busy"
        :loading="busy"
        @click="emit('confirm')"
      />
    </template>
  </Dialog>
</template>
