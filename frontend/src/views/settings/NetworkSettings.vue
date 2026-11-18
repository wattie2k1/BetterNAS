<script setup lang="ts">
import BridgePanel from '@/components/vms/BridgePanel.vue'
import IpPanel from '@/components/settings/IpPanel.vue'
</script>

<template>
  <div class="stack" data-test="settings-network">
    <section class="card">
      <h2>IP-Konfiguration</h2>
      <IpPanel />
    </section>
    <section class="card">
      <h2>Bridge für virtuelle Maschinen</h2>
      <BridgePanel />
    </section>
  </div>
</template>

<style scoped>
.stack {
  display: flex;
  flex-direction: column;
  gap: 18px;
  max-width: 760px;
}
</style>
