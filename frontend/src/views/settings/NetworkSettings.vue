<script setup lang="ts">
import BridgePanel from '@/components/vms/BridgePanel.vue'
</script>

<template>
  <div class="card" data-test="settings-network">
    <h3>Bridge für virtuelle Maschinen</h3>
    <BridgePanel />
  </div>
</template>

<style scoped>
.card {
  padding: 20px 22px;
  max-width: 720px;
}
h3 {
  margin: 0 0 12px;
  font-size: 1rem;
}
</style>
