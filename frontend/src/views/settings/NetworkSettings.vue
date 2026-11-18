<script setup lang="ts">
import { ref } from 'vue'
import Button from 'primevue/button'
import BridgeDialog from '@/components/vms/BridgeDialog.vue'

const bridge = ref(false)
</script>

<template>
  <div class="card glass" data-test="settings-network">
    <h3>Bridge für virtuelle Maschinen</h3>
    <p class="soft">Mit der Bridge <code>br0</code> bekommen VMs eine eigene Adresse im Heimnetz. Die Umstellung wird nach 120 Sekunden ohne Bestätigung automatisch zurückgenommen.</p>
    <Button data-test="settings-bridge" label="Bridge verwalten" icon="pi pi-sitemap" @click="bridge = true" />
    <BridgeDialog v-model:visible="bridge" />
  </div>
</template>

<style scoped>
.card {
  padding: 20px 22px;
}
h3 {
  margin: 0 0 6px;
  font-size: 1rem;
}
.soft {
  margin: 0 0 14px;
  color: var(--bn-text-soft);
  font-size: 0.875rem;
}
</style>
