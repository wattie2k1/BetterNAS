<script setup lang="ts">
import { ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import { formatBytes } from '@/utils/format'
import type { BackupPlan } from '@/api/docker'

const props = defineProps<{ visible: boolean; stack: string; plan: BackupPlan | null; busy?: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean]; confirm: [backup: boolean] }>()

const backup = ref(true)
// Standard: sichern, sobald es möglich ist.
watch(
  () => [props.visible, props.plan] as const,
  () => (backup.value = !!props.plan?.possible),
  { immediate: true },
)
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    header="Stack aktualisieren"
    :style="{ width: 'min(560px, 96vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <p>Für den Stack „{{ stack }}“ werden neue Images geladen und die Dienste neu erstellt.</p>
    <template v-if="plan?.sources.length">
      <label class="check">
        <input v-model="backup" type="checkbox" :disabled="!plan.possible" data-test="update-backup" />
        <span><strong>Vorher die Daten sichern</strong> ({{ formatBytes(plan.totalBytes) }})</span>
      </label>
      <ul class="sources" data-test="update-sources">
        <li v-for="s in plan.sources" :key="s.path">
          <i :class="s.kind === 'volume' ? 'pi pi-database' : 'pi pi-folder'" />
          <span>{{ s.name }}</span>
          <small>{{ formatBytes(s.bytes) }}</small>
        </li>
      </ul>
      <p v-if="!plan.possible" class="err" data-test="update-reason">{{ plan.reason }}</p>
      <small class="hint">
        Dafür wird der Stack kurz angehalten (je nach Datenmenge Sekunden bis Minuten), damit die Daten in sich stimmig sind. Die Sicherung liegt unter /srv/appdata/backups,
        bleibt bis zum nächsten Update oder Rollback erhalten und lässt sich beim Zurückrollen einspielen. Gesichert werden nur App-Daten, keine Ordner auf den Pools.
      </small>
    </template>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button data-test="update-submit" label="Aktualisieren" icon="pi pi-download" :loading="busy" @click="emit('confirm', backup && !!plan?.possible)" />
    </template>
  </Dialog>
</template>

<style scoped>
.check {
  display: flex;
  align-items: flex-start;
  gap: 0.6rem;
  margin: 0.75rem 0 0.5rem;
  cursor: pointer;
}
.sources {
  list-style: none;
  margin: 0 0 0.75rem;
  padding: 0.5rem 0.75rem;
  border: 1px solid var(--p-content-border-color);
  border-radius: var(--bn-radius-xs);
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
}
.sources li {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.sources span {
  flex: 1;
  overflow-wrap: anywhere;
}
.sources small,
.hint {
  color: var(--bn-text-soft);
}
.err {
  color: var(--bn-danger);
}
</style>
