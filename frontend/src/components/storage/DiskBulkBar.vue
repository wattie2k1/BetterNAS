<script setup lang="ts">
import { computed, ref } from 'vue'
import Button from 'primevue/button'
import { useToast } from 'primevue/usetoast'
import { SPINDOWN_CHOICES, spindownLabel, storageApi, type DiskView } from '@/api/storage'
import { useStorageStore } from '@/stores/storage'
import { runBulk } from '@/utils/bulk'

/** Sammelaktionen für die in der Tabelle ausgewählten Platten. Zerstörende Aktionen (Formatieren, Freigeben) gibt es hier bewusst nicht. */
const props = defineProps<{ disks: DiskView[] }>()
const emit = defineEmits<{ clear: [] }>()

const store = useStorageStore()
const toast = useToast()
const busy = ref(false)
const spin = ref(0)

const online = computed(() => props.disks.filter((d) => d.state === 'online'))
const testable = computed(() => online.value.filter((d) => d.smart && !d.smart.selfTest?.running))
const running = computed(() => online.value.filter((d) => d.smart?.selfTest?.running))
const spinnable = computed(() => online.value.filter((d) => d.role === 'data' || d.role === 'parity'))
const findable = computed(() => online.value.filter((d) => d.device && !d.identifying))

const name = (id: string) => {
  const d = props.disks.find((x) => x.id === id)
  return d?.note?.name || d?.model || id
}

async function run(targets: DiskView[], fn: (id: string) => Promise<unknown>, done: (n: number) => string) {
  busy.value = true
  try {
    const res = await runBulk(targets.map((d) => d.id), fn)
    if (res.done.length) toast.add({ severity: 'success', summary: done(res.done.length), life: 5000 })
    if (res.failed.length) {
      toast.add({
        severity: 'error',
        summary: `${res.failed.length} von ${targets.length} fehlgeschlagen`,
        detail: res.failed.map((f) => `${name(f.id)}: ${f.message}`).join('\n'),
        life: 12000,
      })
    }
  } finally {
    await store.refresh()
    busy.value = false
  }
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`
const start = (kind: 'short' | 'long') =>
  run(testable.value, (id) => storageApi.startSelfTest(id, kind), (n) => `${kind === 'short' ? 'Kurztest' : 'Langtest'} auf ${plural(n, 'Platte', 'Platten')} gestartet`)
const abort = () => run(running.value, (id) => storageApi.abortSelfTest(id), (n) => `Selbsttest auf ${plural(n, 'Platte', 'Platten')} abgebrochen`)
const applySpin = () =>
  run(spinnable.value, (id) => storageApi.setSpindown(id, spin.value), (n) => `Ruhezustand „${spindownLabel(spin.value)}“ für ${plural(n, 'Platte', 'Platten')} gesetzt`)
const identify = () => run(findable.value, (id) => storageApi.startIdentify(id), (n) => `${plural(n, 'Platte blinkt', 'Platten blinken')} jetzt 30 Sekunden`)
</script>

<template>
  <div class="bulk" role="region" aria-label="Sammelaktionen" data-test="bulk-bar">
    <strong data-test="bulk-count">{{ disks.length }} ausgewählt</strong>
    <Button :label="`Kurztest (${testable.length})`" size="small" outlined :disabled="busy || !testable.length" data-test="bulk-short" @click="start('short')" />
    <Button :label="`Langtest (${testable.length})`" size="small" outlined :disabled="busy || !testable.length" data-test="bulk-long" @click="start('long')" />
    <Button v-if="running.length" :label="`Test abbrechen (${running.length})`" size="small" severity="secondary" outlined :disabled="busy" data-test="bulk-abort" @click="abort" />
    <span class="spin">
      <select v-model.number="spin" aria-label="Ruhezustand für die Auswahl" :disabled="busy || !spinnable.length" data-test="bulk-spin">
        <option v-for="m in SPINDOWN_CHOICES" :key="m" :value="m">{{ spindownLabel(m) }}</option>
      </select>
      <Button :label="`Ruhezustand (${spinnable.length})`" size="small" outlined :disabled="busy || !spinnable.length" data-test="bulk-spin-apply" @click="applySpin" />
    </span>
    <Button :label="`Finden (${findable.length})`" size="small" outlined :disabled="busy || !findable.length" data-test="bulk-identify" @click="identify" />
    <Button label="Auswahl aufheben" size="small" text :disabled="busy" data-test="bulk-clear" @click="emit('clear')" />
  </div>
</template>

<style scoped>
.bulk {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  padding: 0.6rem 0.8rem;
  margin-bottom: 0.6rem;
  border-radius: var(--bn-radius-sm);
  background: var(--bn-border-soft);
}
.spin {
  display: inline-flex;
  gap: 0.3rem;
  align-items: center;
}
select {
  padding: 0.3rem 0.5rem;
}
</style>
