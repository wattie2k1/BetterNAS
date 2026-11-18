<script setup lang="ts">
import Checkbox from 'primevue/checkbox'
import Message from 'primevue/message'
import type { VmSnapshot } from '@/api/vms'

defineProps<{ snapshots: VmSnapshot[]; prefix: string; action: string }>()
const confirm = defineModel<boolean>({ required: true })
</script>

<template>
  <Message v-if="snapshots.length" severity="warn" :closable="false" :data-test="`${prefix}-snapshots`">
    Diese VM hat {{ snapshots.length }} Snapshot(s): {{ snapshots.map((s) => s.name).join(', ') }}. Mit Snapshots lässt sich die Festplatte nicht ändern – sie werden dafür endgültig gelöscht.
    <label class="row">
      <Checkbox v-model="confirm" binary :input-id="`${prefix}-confirm`" :data-test="`${prefix}-confirm`" />
      <span>Snapshots löschen und {{ action }}</span>
    </label>
  </Message>
</template>

<style scoped>
.row {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  margin-top: 0.6rem;
}
</style>
