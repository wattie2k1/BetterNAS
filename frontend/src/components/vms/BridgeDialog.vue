<script setup lang="ts">
import Dialog from 'primevue/dialog'
import BridgePanel from './BridgePanel.vue'

defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; changed: [] }>()
</script>

<template>
  <Dialog :visible="visible" modal append-to="self" header="Bridge br0 für VMs" :style="{ width: 'min(560px, 96vw)' }" @update:visible="emit('update:visible', $event)">
    <BridgePanel v-if="visible" closable @close="emit('update:visible', false)" @changed="emit('changed')" />
  </Dialog>
</template>
