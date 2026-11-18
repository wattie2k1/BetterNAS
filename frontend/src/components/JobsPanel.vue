<script setup lang="ts">
import ProgressBar from 'primevue/progressbar'
import Tag from 'primevue/tag'
import { RouterLink } from 'vue-router'
import { useJobsStore } from '@/stores/jobs'
import { useRebootStore } from '@/stores/reboot'

const jobs = useJobsStore()
const reboot = useRebootStore()

const statusLabel: Record<string, { label: string; severity: string }> = {
  queued: { label: 'Wartet', severity: 'secondary' },
  running: { label: 'Läuft', severity: 'info' },
  succeeded: { label: 'Erledigt', severity: 'success' },
  failed: { label: 'Fehlgeschlagen', severity: 'danger' },
}

const time = (iso: string) => new Date(iso).toLocaleString('de-DE', { dateStyle: 'short', timeStyle: 'short' })
</script>

<template>
  <div v-if="reboot.status.required" class="reboot" data-test="reboot-required" role="alert">
    <div class="row">
      <strong><i class="pi pi-exclamation-triangle" /> Neustart erforderlich</strong>
      <Tag value="Offen" severity="warn" />
    </div>
    <p class="why">{{ reboot.status.reason || 'Ein Update verlangt einen Neustart des Servers.' }}</p>
    <p v-if="reboot.status.packages.length" class="meta">Betroffen: {{ reboot.status.packages.join(', ') }}</p>
    <RouterLink to="/einstellungen/energie">Zu Energie &amp; Neustart</RouterLink>
  </div>
  <p v-if="!jobs.jobs.length" class="empty">Noch keine Aufträge.</p>
  <ul class="jobs">
    <li v-for="j in jobs.jobs" :key="j.id">
      <div class="row">
        <strong>{{ j.kind }}</strong>
        <Tag :value="statusLabel[j.status].label" :severity="statusLabel[j.status].severity" />
      </div>
      <div class="meta">{{ j.target }} · {{ j.startedBy }} · {{ time(j.startedAt) }}</div>
      <ProgressBar v-if="j.status === 'running'" :value="j.progress" style="height: 6px" :show-value="false" />
      <details v-if="j.log">
        <summary>Protokoll</summary>
        <pre>{{ j.log }}</pre>
      </details>
    </li>
  </ul>
</template>

<style scoped>
.reboot {
  border: 1px solid var(--p-orange-400, #fb923c);
  background: color-mix(in srgb, var(--p-orange-400, #fb923c) 14%, transparent);
  border-radius: 12px;
  padding: 0.75rem 0.9rem;
  margin-bottom: 1rem;
}
.reboot .why {
  margin: 0.4rem 0 0.2rem;
}
.reboot .meta {
  margin: 0 0 0.4rem;
}
.empty {
  color: var(--p-text-muted-color);
}
.jobs {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.row {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.meta {
  font-size: 0.8rem;
  color: var(--p-text-muted-color);
  margin: 0.2rem 0 0.4rem;
}
pre {
  white-space: pre-wrap;
  font-size: 0.75rem;
  max-height: 200px;
  overflow: auto;
}
</style>
