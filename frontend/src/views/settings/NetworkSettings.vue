<script setup lang="ts">
import BridgePanel from '@/components/vms/BridgePanel.vue'
import IpPanel from '@/components/settings/IpPanel.vue'
</script>

<template>
  <div class="stack" data-test="settings-network">
    <section class="card">
      <h3>IP-Konfiguration</h3>
      <IpPanel />
    </section>
    <section class="card">
      <h3>Bridge für virtuelle Maschinen</h3>
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
.card {
  padding: 20px 22px;
}
h3 {
  margin: 0 0 12px;
  font-size: 1rem;
}
</style>
