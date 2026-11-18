<script setup lang="ts">
import { ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import type { StackInfo } from '@/api/docker'

const props = defineProps<{ visible: boolean; stack: StackInfo | null; busy?: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; confirm: [restoreData: boolean] }>()

const restoreData = ref(false)
// Daten zurückzuspielen ist nie voreingestellt: es verwirft alles, was seit dem Update geändert wurde.
watch(
  () => [props.visible, props.stack] as const,
  () => (restoreData.value = false),
  { immediate: true },
)
const when = (iso?: string) => (iso ? new Date(iso).toLocaleString('de-DE') : '')
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    header="Update zurückrollen"
    :style="{ width: 'min(520px, 96vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <p data-test="rollback-text">
      Der Stack „{{ stack?.name }}“ wird mit den Images von vor dem letzten Update (Stand {{ when(stack?.rollback) }}) neu erstellt.
      Daten in Volumes und Ordnern bleiben dabei erhalten; war das Update mit einer Datenbank-Migration verbunden, kann die ältere Version damit eventuell nicht mehr umgehen.
    </p>
    <template v-if="stack?.rollbackData">
      <label class="check">
        <input v-model="restoreData" type="checkbox" data-test="rollback-data" />
        <span><strong>Auch die Daten auf den Stand vor dem Update zurücksetzen</strong></span>
      </label>
      <p v-if="restoreData" class="err" data-test="rollback-warning">
        Der Stack wird dafür angehalten. Alle Änderungen an seinen Daten seit dem Update gehen verloren, auch neue Dateien und Einträge.
      </p>
      <small v-else class="hint">Empfohlen, wenn der Stack nach dem Update nicht mehr richtig startet.</small>
    </template>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button data-test="rollback-submit" label="Zurückrollen" icon="pi pi-undo" :severity="restoreData ? 'danger' : undefined" :loading="busy" @click="emit('confirm', restoreData)" />
    </template>
  </Dialog>
</template>

<style scoped>
.check {
  display: flex;
  align-items: flex-start;
  gap: 0.6rem;
  margin: 0.75rem 0 0.5rem;
  cursor: pointer;
}
.err {
  color: var(--bn-danger);
}
.hint {
  color: var(--bn-text-soft);
}
</style>
