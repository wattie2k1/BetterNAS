<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Button from 'primevue/button'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import ToggleSwitch from 'primevue/toggleswitch'
import type { VmConfig, VmConfigUpdate, VmDiskOpt, VmNic } from '@/api/vms'

export interface AdvancedState {
  dirty: boolean
  valid: boolean
  update: Partial<VmConfigUpdate>
}

const props = defineProps<{
  cfg: VmConfig
  vcpus: number | null
  editable: boolean
  netOptions: { label: string; value: string }[]
  /** Wird erhöht, wenn die Eingaben auf den gespeicherten Stand zurückgesetzt werden sollen. */
  reset: number
}>()
const emit = defineEmits<{ change: [state: AdvancedState] }>()

type NicRow = VmNic & { existing: boolean }

const cpuMode = ref<'host-passthrough' | 'host-model'>('host-passthrough')
const sockets = ref<number | null>(1)
const threads = ref<number | null>(1)
const hyperv = ref(false)
const clock = ref<'utc' | 'localtime'>('utc')
const diskOpts = ref<VmDiskOpt[]>([])
const nics = ref<NicRow[]>([])

function load() {
  const c = props.cfg
  cpuMode.value = c.cpuMode === 'host-model' ? 'host-model' : 'host-passthrough'
  sockets.value = c.sockets || 1
  threads.value = c.threads || 1
  hyperv.value = !!c.hyperv
  clock.value = c.clock === 'localtime' ? 'localtime' : 'utc'
  diskOpts.value = (c.diskOpts ?? []).map((d) => ({ ...d }))
  nics.value = (c.extraNics ?? []).map((n) => ({ ...n, existing: true }))
}
watch(() => [props.cfg, props.reset], load, { immediate: true })

const cacheOptions = [
  { label: 'Standard (empfohlen)', value: 'default' },
  { label: 'Aus (none) – sicher, direkt auf die Platte', value: 'none' },
  { label: 'Write-Back – schneller, bei Stromausfall riskanter', value: 'writeback' },
  { label: 'Write-Through – sicher, langsamer schreiben', value: 'writethrough' },
]
const modelOptions = [
  { label: 'virtio (schnell, braucht Treiber unter Windows)', value: 'virtio' },
  { label: 'e1000e (Intel, ohne Zusatztreiber)', value: 'e1000e' },
  { label: 'rtl8139 (alt, sehr kompatibel)', value: 'rtl8139' },
]
const cpuOptions = [
  { label: 'Host-Prozessor (host-passthrough) – volle Leistung', value: 'host-passthrough' },
  { label: 'Host-Modell (host-model) – besser übertragbar', value: 'host-model' },
]
const clockOptions = [
  { label: 'UTC (Linux)', value: 'utc' },
  { label: 'Lokale Zeit (Windows)', value: 'localtime' },
]
const diskName = (p: string) => p.split('/').pop() ?? p

const topoTouched = computed(() => sockets.value !== (props.cfg.sockets || 1) || threads.value !== (props.cfg.threads || 1))
const topoActive = computed(() => topoTouched.value || (props.cfg.sockets ?? 0) > 0)
const cores = computed(() => {
  const per = (sockets.value ?? 0) * (threads.value ?? 0)
  return per > 0 && props.vcpus && props.vcpus % per === 0 ? props.vcpus / per : null
})
const topoValid = computed(() => !topoActive.value || (Number.isInteger(sockets.value) && Number.isInteger(threads.value) && cores.value !== null))

const MAC_RE = /^([0-9a-f]{2}:){5}[0-9a-f]{2}$/i
const macOk = (m: string) => m === '' || (MAC_RE.test(m) && (parseInt(m.slice(0, 2), 16) & 1) === 0)
const nicsValid = computed(() => {
  const macs = nics.value.map((n) => n.mac.toLowerCase()).filter(Boolean)
  return nics.value.every((n) => macOk(n.mac) && n.network) && new Set(macs).size === macs.length
})
const nicsChanged = computed(() => {
  const cur = (props.cfg.extraNics ?? []).map((n) => `${n.network}|${n.mac.toLowerCase()}`)
  const now = nics.value.map((n) => `${n.network}|${n.mac.toLowerCase()}`)
  return cur.length !== now.length || cur.some((v, i) => v !== now[i]) || nics.value.some((n) => !n.existing)
})
const diskChanged = computed(() => {
  const cur = new Map((props.cfg.diskOpts ?? []).map((d) => [d.path, `${d.cache}|${d.discard}`]))
  return diskOpts.value.some((d) => cur.get(d.path) !== `${d.cache}|${d.discard}`)
})

const state = computed<AdvancedState>(() => {
  const c = props.cfg
  const update: Partial<VmConfigUpdate> = {}
  const curCpu = c.cpuMode ?? 'other'
  const curClock = c.clock ?? 'other'
  if (curCpu !== 'other' && cpuMode.value !== curCpu) update.cpuMode = cpuMode.value
  if (topoTouched.value && sockets.value && threads.value) update.topology = { sockets: sockets.value, threads: threads.value }
  if (hyperv.value !== !!c.hyperv) update.hyperv = hyperv.value
  if (curClock !== 'other' && clock.value !== curClock) update.clock = clock.value
  if (diskChanged.value) update.diskOpts = diskOpts.value.map((d) => ({ ...d }))
  if (nicsChanged.value) update.extraNics = nics.value.map(({ network, model, mac }) => ({ network, model, mac: mac.toLowerCase() }))
  return { dirty: Object.keys(update).length > 0, valid: topoValid.value && nicsValid.value, update }
})
watch(state, (s) => emit('change', s), { immediate: true })

function addNic() {
  nics.value.push({ network: props.netOptions[0]?.value ?? 'default', model: 'virtio', mac: '', existing: false })
}
</script>

<template>
  <details class="adv" data-test="adv">
    <summary>Erweiterte Einstellungen</summary>

    <h3>Prozessor</h3>
    <div class="field">
      <label for="adv-cpu">CPU-Typ</label>
      <Select v-model="cpuMode" input-id="adv-cpu" :options="cpuOptions" option-label="label" option-value="value" :disabled="!editable || cfg.cpuMode === 'other'" data-test="adv-cpu" />
      <small v-if="cfg.cpuMode === 'other'" class="hint">Diese VM nutzt ein festes CPU-Modell, das sich hier nicht ändern lässt.</small>
    </div>
    <div class="field">
      <label>Topologie (Sockets × Kerne × Threads)</label>
      <div class="topo">
        <InputNumber v-model="sockets" :min="1" :max="8" show-buttons :disabled="!editable" input-class="narrow" data-test="adv-sockets" />
        <span>×</span>
        <span class="cores" data-test="adv-cores">{{ cores ?? '?' }}</span>
        <span>×</span>
        <InputNumber v-model="threads" :min="1" :max="8" show-buttons :disabled="!editable" input-class="narrow" data-test="adv-threads" />
      </div>
      <small class="hint" :class="{ bad: !topoValid }">{{ topoValid ? `Kerne ergeben sich aus den vCPUs (${vcpus ?? '–'}) geteilt durch Sockets × Threads. Nur ändern, wenn das Gastsystem oder eine Lizenz es verlangt.` : 'Die vCPUs müssen durch Sockets × Threads teilbar sein.' }}</small>
    </div>

    <h3>Festplatten</h3>
    <div v-for="d in diskOpts" :key="d.path" class="disk" data-test="adv-disk">
      <strong>{{ diskName(d.path) }}</strong>
      <div class="field">
        <label :for="`adv-cache-${d.path}`">Cache</label>
        <Select v-model="d.cache" :input-id="`adv-cache-${d.path}`" :options="cacheOptions" option-label="label" option-value="value" :disabled="!editable" data-test="adv-cache" />
      </div>
      <label class="row">
        <ToggleSwitch :model-value="d.discard === 'unmap'" :disabled="!editable" aria-label="TRIM" data-test="adv-discard" @update:model-value="(v: boolean) => (d.discard = v ? 'unmap' : 'ignore')" />
        <span>TRIM/Discard weitergeben</span>
      </label>
    </div>
    <small class="hint">TRIM lässt die Image-Datei schrumpfen, wenn der Gast Dateien löscht (unter Linux z. B. per <code>fstrim</code>).</small>

    <h3>Zusätzliche Netzwerkkarten</h3>
    <div v-for="(n, i) in nics" :key="i" class="nic" data-test="adv-nic">
      <Select v-model="n.network" :options="netOptions" option-label="label" option-value="value" :disabled="!editable" data-test="adv-nic-network" />
      <div class="nic-row">
        <Select v-model="n.model" :options="modelOptions" option-label="label" option-value="value" :disabled="!editable || n.existing" data-test="adv-nic-model" />
        <input v-model="n.mac" class="mac" :class="{ invalid: !macOk(n.mac) }" placeholder="MAC (leer = automatisch)" :disabled="!editable" data-test="adv-nic-mac" />
        <Button icon="pi pi-trash" severity="danger" text :disabled="!editable" aria-label="Netzwerkkarte entfernen" data-test="adv-nic-remove" @click="nics.splice(i, 1)" />
      </div>
    </div>
    <Button label="Netzwerkkarte hinzufügen" icon="pi pi-plus" size="small" severity="secondary" :disabled="!editable || nics.length >= 4" data-test="adv-nic-add" @click="addNic" />
    <small class="hint">Neue Karten bekommen eine automatische MAC. Das Modell einer bestehenden Karte lässt sich nicht ändern; entfernen und neu anlegen geht.</small>

    <h3>Uhr und Windows</h3>
    <div class="field">
      <label for="adv-clock">Hardware-Uhr</label>
      <Select v-model="clock" input-id="adv-clock" :options="clockOptions" option-label="label" option-value="value" :disabled="!editable || cfg.clock === 'other'" data-test="adv-clock" />
      <small class="hint">Windows erwartet lokale Zeit, Linux UTC. Stimmt das nicht, geht die Uhr in der VM um Stunden falsch.</small>
    </div>
    <div class="field">
      <label class="row">
        <ToggleSwitch v-model="hyperv" :disabled="!editable" aria-label="Hyper-V-Optimierungen" data-test="adv-hyperv" />
        <span>Hyper-V-Optimierungen für Windows</span>
      </label>
      <small class="hint">Verbessert Leistung und Zeitgeber von Windows-Gästen. Für Linux nicht nötig.</small>
    </div>
  </details>
</template>

<style scoped>
.adv {
  margin-top: 1.25rem;
  border-top: 1px solid var(--bn-border, rgba(128, 128, 128, 0.25));
  padding-top: 0.75rem;
}
.adv summary {
  cursor: pointer;
  font-weight: 600;
}
.adv h3 {
  margin: 1.25rem 0 0.25rem;
  font-size: 0.95rem;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  margin-top: 0.75rem;
}
.row {
  display: flex;
  align-items: center;
  gap: 0.6rem;
}
.topo {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.cores {
  min-width: 2rem;
  text-align: center;
  font-weight: 600;
}
.hint {
  color: var(--bn-text-soft);
  font-size: 0.8rem;
}
.hint.bad {
  color: var(--bn-danger);
}
.disk,
.nic {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  margin-top: 0.75rem;
}
.nic-row {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
.mac {
  flex: 1;
  min-width: 0;
}
.invalid {
  border-color: var(--bn-danger);
}
:deep(.narrow) {
  width: 4.5rem;
}
</style>
