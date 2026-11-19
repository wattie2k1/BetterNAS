<script setup lang="ts">
import { ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import { storageApi, type SelfTestSchedule } from '@/api/storage'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean] }>()
const toast = useToast()

const form = ref<SelfTestSchedule>({ short: 'off', long: 'off', hour: 2 })
const error = ref('')
const busy = ref(false)

const SHORT = [
  { value: 'off', label: 'Aus' },
  { value: 'weekly', label: 'Wöchentlich' },
  { value: 'monthly', label: 'Monatlich' },
]
const LONG = [
  { value: 'off', label: 'Aus' },
  { value: 'monthly', label: 'Monatlich' },
  { value: 'quarterly', label: 'Alle 3 Monate' },
]

watch(
  () => props.visible,
  async (v) => {
    if (!v) return
    error.value = ''
    try {
      form.value = await storageApi.selfTestSchedule()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  },
  { immediate: true },
)

async function save() {
  busy.value = true
  error.value = ''
  try {
    await storageApi.saveSelfTestSchedule({ ...form.value, hour: Number(form.value.hour) })
    toast.add({ severity: 'success', summary: 'Zeitplan gespeichert', life: 4000 })
    emit('update:visible', false)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Dialog :visible="visible" modal append-to="self" header="Selbsttests planen" :style="{ width: 'min(480px, 95vw)' }" @update:visible="emit('update:visible', $event)">
    <p class="hint">
      Die Platten prüfen sich selbst und melden Fehler, bevor Daten verloren gehen. Die Tests laufen nacheinander auf allen Platten mit SMART-Werten;
      ein fälliger Langtest ersetzt den Kurztest. Eine schlafende Platte wird dafür geweckt.
    </p>
    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
    <label class="row">
      <span>Kurztest</span>
      <select v-model="form.short" data-test="sched-short">
        <option v-for="o in SHORT" :key="o.value" :value="o.value">{{ o.label }}</option>
      </select>
    </label>
    <label class="row">
      <span>Langtest</span>
      <select v-model="form.long" data-test="sched-long">
        <option v-for="o in LONG" :key="o.value" :value="o.value">{{ o.label }}</option>
      </select>
    </label>
    <label class="row">
      <span>Beginn</span>
      <select v-model="form.hour" data-test="sched-hour">
        <option v-for="h in 24" :key="h" :value="h - 1">{{ String(h - 1).padStart(2, '0') }}:00 Uhr</option>
      </select>
    </label>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button label="Speichern" :loading="busy" data-test="sched-save" @click="save" />
    </template>
  </Dialog>
</template>

<style scoped>
.hint {
  font-size: 0.85rem;
  color: var(--bn-text-soft);
}
.row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
  margin: 0.6rem 0;
}
select {
  padding: 0.35rem 0.6rem;
  min-width: 10rem;
}
</style>
