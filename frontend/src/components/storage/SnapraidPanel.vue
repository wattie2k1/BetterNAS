<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { storageApi, unprotected, type PoolView, type SnapraidRun, type SnapraidStatus } from '@/api/storage'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ pool: PoolView; refreshKey?: number }>()
const emit = defineEmits<{ configure: []; run: [action: 'sync' | 'scrub' | 'diff', force: boolean]; disable: [] }>()

const status = ref<SnapraidStatus | null>(null)
const error = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
let alive = true

async function load() {
  clearTimeout(timer)
  if (!props.pool.snapraid) {
    status.value = null
    return
  }
  try {
    status.value = await storageApi.snapraidStatus(props.pool.name)
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
  // Solange ein Lauf aktiv ist, alle 5 Sekunden nachfragen.
  if (alive && status.value?.running) timer = setTimeout(load, 5000)
}

onMounted(load)
onBeforeUnmount(() => {
  alive = false
  clearTimeout(timer)
})
watch(() => [props.refreshKey, !!props.pool.snapraid], load)

const running = computed(() => !!status.value?.running)
const blocked = computed(() => status.value?.lastSync?.result === 'blocked')
const massDelete = computed(() => blocked.value && (status.value?.lastSync?.removed ?? 0) > 0)

const tag = (r?: SnapraidRun) =>
  r ? { ok: { v: 'OK', s: 'success' }, errors: { v: 'Fehler gefunden', s: 'danger' }, failed: { v: 'Fehlgeschlagen', s: 'danger' }, blocked: { v: 'Gestoppt', s: 'warn' } }[r.result] : null
const syncTag = computed(() => tag(status.value?.lastSync))
const scrubTag = computed(() => tag(status.value?.lastScrub))
</script>

<template>
  <section class="snap" data-test="snapraid-panel">
    <template v-if="!pool.snapraid">
      <p class="sub">Keine Ausfallsicherheit. Mit SnapRAID lässt sich der Ausfall einer oder zweier Platten ausgleichen.</p>
      <Button data-test="snapraid-setup" label="SnapRAID einrichten" icon="pi pi-shield" size="small" outlined @click="emit('configure')" />
    </template>
    <template v-else>
      <div class="head">
        <strong><i class="pi pi-shield" /> SnapRAID</strong>
        <Tag v-if="running" value="Läuft" severity="info" />
      </div>
      <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
      <Message v-if="massDelete" severity="warn" :closable="false" data-test="snapraid-blocked">
        Sync gestoppt: {{ status!.lastSync!.removed }} Dateien gelöscht. Prüfe, ob das gewollt ist (z. B. eine leere oder neu
        formatierte Platte), bevor du fortfährst.
        <Button data-test="snapraid-force" label="Trotzdem synchronisieren" size="small" severity="warn" :disabled="running" @click="emit('run', 'sync', true)" />
      </Message>
      <Message v-else-if="blocked" severity="warn" :closable="false">{{ status!.lastSync!.message }}</Message>
      <dl v-if="status">
        <dt>Letzter Sync</dt>
        <dd>
          <template v-if="status.lastSync">
            {{ formatDateTime(status.lastSync.at) }} <Tag v-if="syncTag" :value="syncTag.v" :severity="syncTag.s" />
          </template>
          <template v-else>noch nie</template>
        </dd>
        <dt>Ungeschützte Änderungen</dt>
        <dd data-test="snapraid-unprotected">
          <template v-if="status.diff">{{ unprotected(status.diff) }}<span v-if="status.diffAt" class="sub"> (Stand {{ formatDateTime(status.diffAt) }})</span></template>
          <template v-else>unbekannt</template>
        </dd>
        <dt>Letzte Prüfung</dt>
        <dd>
          <template v-if="status.lastScrub">
            {{ formatDateTime(status.lastScrub.at) }} <Tag v-if="scrubTag" :value="scrubTag.v" :severity="scrubTag.s" />
            <div v-if="status.lastScrub.result !== 'ok' && status.lastScrub.message" class="sub">{{ status.lastScrub.message }}</div>
          </template>
          <template v-else>noch nie</template>
        </dd>
      </dl>
      <p class="sub">
        Paritätsplatten: {{ pool.snapraid.parity.join(', ') }} · Sync {{ pool.snapraid.syncSchedule }} · Prüfung
        {{ pool.snapraid.scrubSchedule }}
      </p>
      <div class="actions">
        <Button data-test="snapraid-sync" label="Jetzt synchronisieren" size="small" outlined :disabled="running" @click="emit('run', 'sync', false)" />
        <Button data-test="snapraid-scrub" label="Jetzt prüfen" size="small" outlined :disabled="running" @click="emit('run', 'scrub', false)" />
        <Button data-test="snapraid-diff" label="Änderungen prüfen" size="small" text :disabled="running" @click="emit('run', 'diff', false)" />
        <Button data-test="snapraid-edit" label="Einstellungen" size="small" text @click="emit('configure')" />
        <Button data-test="snapraid-disable" label="Abschalten" size="small" text severity="danger" :disabled="running" @click="emit('disable')" />
      </div>
    </template>
  </section>
</template>

<style scoped>
.snap {
  border-top: 1px solid var(--p-content-border-color);
  margin-top: 0.8rem;
  padding-top: 0.8rem;
}
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.4rem;
}
dl {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 0.25rem 1rem;
  margin: 0.5rem 0;
}
dt {
  color: var(--p-text-muted-color);
}
dd {
  margin: 0;
}
.sub {
  color: var(--p-text-muted-color);
  font-size: 0.875rem;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}
</style>
