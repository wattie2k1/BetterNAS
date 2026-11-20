<script setup lang="ts">
import { computed, ref } from 'vue'
import Button from 'primevue/button'
import Menu from 'primevue/menu'
import Message from 'primevue/message'
import ProgressBar from 'primevue/progressbar'
import Tag from 'primevue/tag'
import { useStorageStore } from '@/stores/storage'
import { profileLabel, type PoolView } from '@/api/storage'
import { formatBytes } from '@/utils/format'

const props = defineProps<{ pool: PoolView }>()
export type BtrfsAction = 'replace' | 'add' | 'remove' | 'balance' | 'convert'
const emit = defineEmits<{
  scrub: [action: 'start' | 'cancel']
  action: [kind: BtrfsAction]
  mountDegraded: []
  resetErrors: []
}>()
const storage = useStorageStore()
const menu = ref<InstanceType<typeof Menu> | null>(null)

const st = computed(() => props.pool.btrfs)
const op = computed(() => st.value?.op)
const replace = computed(() => (st.value?.replace?.state === 'running' ? st.value.replace : null))
const scrub = computed(() => st.value?.scrub)
const opLabel = { balance: 'Balance', remove: 'Platte entfernen', convert: 'Profilwechsel' } as const
const busy = computed(() => !!op.value?.running || !!replace.value || !!scrub.value?.running)
const mounted = computed(() => !!st.value?.mounted)
const degraded = computed(() => !!st.value?.degraded)
const profile = computed(() => profileLabel(st.value?.dataProfile || props.pool.profile))

const state = computed(() => {
  if (degraded.value) return { v: 'Unvollständig', s: 'danger' }
  if (busy.value) return { v: 'Wartung läuft', s: 'info' }
  if ((st.value?.errors ?? 0) > 0) return { v: 'Fehler gezählt', s: 'warn' }
  return { v: 'In Ordnung', s: 'success' }
})

const diskName = (id: string) => {
  const d = storage.disks.find((x) => x.id === id)
  return d ? d.model || id : id
}
/** Platten mit gezählten Fehlern (Pfad der Zählung: btrfs device stats). */
const faulty = computed(() =>
  (st.value?.devices ?? [])
    .map((d) => ({ d, n: d.errors.write + d.errors.read + d.errors.flush + d.errors.corruption + d.errors.generation }))
    .filter((x) => x.n > 0),
)

const scrubTag = computed(() => {
  const s = scrub.value
  if (!s || s.running) return null
  if (s.uncorrectable > 0) return { v: `${s.uncorrectable} nicht reparierbar`, s: 'danger' }
  if (s.result === 'finished' && s.errors > 0) return { v: `${s.corrected} repariert`, s: 'warn' }
  if (s.result === 'finished') return { v: 'OK', s: 'success' }
  return { v: s.result === 'aborted' ? 'Abgebrochen' : 'Unterbrochen', s: 'secondary' }
})

const items = computed(() => [
  { label: 'Platte ersetzen …', icon: 'pi pi-sync', disabled: busy.value || !mounted.value, command: () => emit('action', 'replace') },
  { label: 'Platte hinzufügen …', icon: 'pi pi-plus', disabled: busy.value || !mounted.value || degraded.value, command: () => emit('action', 'add') },
  { label: 'Platte entfernen …', icon: 'pi pi-minus', disabled: busy.value || !mounted.value, command: () => emit('action', 'remove') },
  { separator: true },
  { label: 'Daten neu verteilen (Balance) …', icon: 'pi pi-arrows-h', disabled: busy.value || !mounted.value, command: () => emit('action', 'balance') },
  { label: 'Schutz ändern …', icon: 'pi pi-shield', disabled: busy.value || !mounted.value || degraded.value, command: () => emit('action', 'convert') },
  { separator: true },
  { label: 'Fehlerzähler zurücksetzen', icon: 'pi pi-refresh', disabled: !mounted.value || (st.value?.errors ?? 0) === 0, command: () => emit('resetErrors') },
])
</script>

<template>
  <section class="btrfs" data-test="btrfs-panel">
    <div class="head">
      <strong><i class="pi pi-shield" /> btrfs · {{ profile }}</strong>
      <Tag v-if="st" :value="state.v" :severity="state.s" data-test="btrfs-state" />
    </div>
    <p v-if="!st" class="sub">Status nicht verfügbar.</p>
    <template v-else>
      <p v-if="st.usableBytes" class="sub">
        Nutzbar {{ formatBytes(st.usableBytes) }} von {{ formatBytes(st.rawBytes) }} Rohkapazität
      </p>

      <Message v-if="degraded" severity="error" :closable="false" data-test="btrfs-degraded">
        Es fehlt {{ st.missing === 1 ? 'eine Platte' : `${st.missing} Platten` }}.
        <template v-if="!mounted">
          Der Pool ist deshalb nicht eingehängt. Mit „Degradiert einhängen“ ist er bis zum nächsten Neustart wieder erreichbar, damit du die fehlende Platte ersetzen kannst.
          <div class="actions">
            <Button data-test="btrfs-mount-degraded" label="Degradiert einhängen" size="small" severity="warn" @click="emit('mountDegraded')" />
          </div>
        </template>
        <template v-else> Ersetze die Platte, damit der Pool wieder voll geschützt ist.</template>
      </Message>

      <Message v-if="faulty.length" severity="warn" :closable="false" data-test="btrfs-errors">
        Das Dateisystem hat Fehler gezählt:
        <span v-for="x in faulty" :key="x.d.devId" class="wipe">
          {{ x.d.diskId ? diskName(x.d.diskId) : x.d.path || `Platte ${x.d.devId}` }}: {{ x.n }}
          (Lesen {{ x.d.errors.read }}, Schreiben {{ x.d.errors.write }}, Prüfsumme {{ x.d.errors.corruption }})
        </span>
        Prüfe die SMART-Werte der Platte und setze die Zähler danach zurück.
      </Message>

      <div v-if="replace" data-test="btrfs-replace">
        <p class="sub">Plattenersatz läuft … {{ Math.round(replace.percent) }} %</p>
        <ProgressBar :value="Math.round(replace.percent)" :show-value="false" />
      </div>
      <div v-if="op?.running" data-test="btrfs-op">
        <p class="sub">{{ opLabel[op.op as keyof typeof opLabel] ?? 'Wartung' }} läuft<template v-if="op.progress >= 0"> … {{ op.progress }} %</template></p>
        <ProgressBar v-if="op.progress >= 0" :value="op.progress" :show-value="false" />
        <ProgressBar v-else mode="indeterminate" style="height: 6px" />
      </div>
      <Message v-else-if="op?.result === 'failed'" severity="error" :closable="false" data-test="btrfs-op-failed">
        {{ opLabel[op.op as keyof typeof opLabel] ?? 'Wartung' }} ist fehlgeschlagen. {{ op.message }}
      </Message>

      <dl>
        <dt>Letzte Prüfung</dt>
        <dd data-test="btrfs-scrub">
          <template v-if="scrub?.running">läuft …</template>
          <template v-else-if="scrub">
            {{ scrub.started }} <Tag v-if="scrubTag" :value="scrubTag.v" :severity="scrubTag.s" />
          </template>
          <template v-else>noch nie</template>
        </dd>
      </dl>
    </template>

    <div class="actions">
      <Button
        v-if="scrub?.running"
        data-test="btrfs-scrub-cancel"
        label="Prüfung abbrechen"
        size="small"
        text
        @click="emit('scrub', 'cancel')"
      />
      <Button
        v-else
        data-test="btrfs-scrub-start"
        label="Jetzt prüfen"
        size="small"
        outlined
        :disabled="!mounted || busy"
        @click="emit('scrub', 'start')"
      />
      <Button data-test="btrfs-menu" label="Wartung" icon="pi pi-wrench" size="small" text @click="(e) => menu?.toggle(e)" />
      <Menu ref="menu" :model="items" popup append-to="body" />
    </div>
  </section>
</template>

<style scoped>
.btrfs {
  margin-top: 1rem;
  padding-top: 0.75rem;
  border-top: 1px solid var(--bn-border, rgba(127, 127, 127, 0.25));
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem;
}
.sub {
  color: var(--bn-text-soft);
  font-size: 0.875rem;
  margin: 0;
}
dl {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 0.2rem 0.75rem;
  margin: 0;
  font-size: 0.875rem;
}
dt {
  color: var(--bn-text-soft);
}
dd {
  margin: 0;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
  margin-top: 0.4rem;
}
.wipe {
  display: block;
}
</style>
