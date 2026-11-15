<script setup lang="ts">
import ProgressBar from 'primevue/progressbar'
import Tag from 'primevue/tag'
import { useJobsStore } from '@/stores/jobs'

const jobs = useJobsStore()

const statusLabel: Record<string, { label: string; severity: string }> = {
  queued: { label: 'Wartet', severity: 'secondary' },
  running: { label: 'Läuft', severity: 'info' },
  succeeded: { label: 'Erledigt', severity: 'success' },
  failed: { label: 'Fehlgeschlagen', severity: 'danger' },
}

const time = (iso: string) => new Date(iso).toLocaleString('de-DE', { dateStyle: 'short', timeStyle: 'short' })
</script>

<template>
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
