<script setup lang="ts">
import Button from 'primevue/button'
import { useRouter } from 'vue-router'

const router = useRouter()
const go = (name: string) => void router?.push({ name })
</script>

<template>
  <div class="card" data-test="docker-not-configured">
    <h3><i class="pi pi-lock" /> Docker ist noch nicht eingerichtet</h3>
    <p>
      Lege zuerst fest, wo Docker seine Daten ablegt: die Einstellungen der Apps (klein) und die Daten, Datenbanken und Images (groß). Beides sollte nicht auf der Systemplatte liegen.
    </p>
    <p>Bereite zuerst unter Speicher einen Pool oder eine Platte vor, falls es noch keinen gibt.</p>
    <div class="actions">
      <Button data-test="docker-setup" label="Speicherorte festlegen" icon="pi pi-cog" @click="go('settings-docker')" />
      <Button data-test="docker-setup-pools" label="Zu den Speicherpools" icon="pi pi-database" severity="secondary" text @click="go('pools')" />
    </div>
  </div>
</template>

<style scoped>
.card {
  max-width: 640px;
}
h3 {
  margin-top: 0;
}
.actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
