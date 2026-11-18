<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import Checkbox from 'primevue/checkbox'
import Message from 'primevue/message'
import Tag from 'primevue/tag'
import { vmsApi, type VmConfig, type VmConfigUpdate, type VmPciDevice, type VmPciInfo } from '@/api/vms'

export interface PciState {
  dirty: boolean
  valid: boolean
  update: Partial<VmConfigUpdate>
}

const props = defineProps<{ cfg: VmConfig; editable: boolean; reset: number }>()
const emit = defineEmits<{ change: [state: PciState] }>()

const info = ref<VmPciInfo>({ iommu: true, devices: [] })
const loaded = ref(false)
const sel = ref<string[]>([])

function load() {
  sel.value = [...(props.cfg.pci ?? [])]
}
watch(() => [props.cfg, props.reset], load, { immediate: true })
onMounted(async () => {
  try {
    info.value = await vmsApi.pci()
  } catch {
    info.value = { iommu: true, devices: [] }
  }
  loaded.value = true
})

const kindLabel: Record<string, string> = { gpu: 'Grafik', audio: 'Audio', usb: 'USB-Controller', net: 'Netzwerk', storage: 'Speicher', other: 'Sonstiges' }

interface Group {
  id: number
  devices: VmPciDevice[]
  blocked: string
}
const groups = computed<Group[]>(() => {
  const m = new Map<number, VmPciDevice[]>()
  for (const d of info.value.devices) {
    const k = d.group >= 0 ? d.group : -1000 - m.size // Geräte ohne Gruppe stehen einzeln
    m.set(k, [...(m.get(k) ?? []), d])
  }
  return [...m.entries()].map(([id, devices]) => ({ id: id < 0 ? -1 : id, devices, blocked: devices.find((d) => d.blocked)?.blocked ?? '' }))
})
const usable = computed(() => groups.value.filter((g) => !g.blocked))
const blockedList = computed(() => info.value.devices.filter((d) => d.blocked))
/** Zugewiesene Adressen, die der Host gerade nicht (mehr) hat. */
const missing = computed(() => sel.value.filter((a) => !info.value.devices.some((d) => d.address === a)))

const groupChecked = (g: Group) => g.devices.every((d) => sel.value.includes(d.address))
function toggle(g: Group, on: boolean) {
  const addrs = g.devices.map((d) => d.address)
  sel.value = on ? [...new Set([...sel.value, ...addrs])] : sel.value.filter((a) => !addrs.includes(a))
}
const removeMissing = (a: string) => (sel.value = sel.value.filter((x) => x !== a))

const changed = computed(() => [...sel.value].sort().join(',') !== [...(props.cfg.pci ?? [])].sort().join(','))
const state = computed<PciState>(() => ({ dirty: changed.value, valid: sel.value.length <= 8, update: changed.value ? { pci: [...sel.value] } : {} }))
watch(state, (s) => emit('change', s), { immediate: true })
const selectedDevices = computed(() => info.value.devices.filter((d) => sel.value.includes(d.address)))
</script>

<template>
  <section class="pci" data-test="pci">
    <h3>PCI-Geräte durchreichen</h3>
    <p class="hint">Ein PCI-Gerät des NAS (z. B. eine Grafikkarte, ein USB- oder Speicher-Controller) gehört der VM direkt. Nur bei ausgeschalteter VM änderbar.</p>
    <Message v-if="loaded && !info.iommu" severity="info" :closable="false" data-test="pci-noiommu">
      Die IOMMU ist auf diesem NAS nicht aktiv, deshalb lassen sich keine PCI-Geräte durchreichen. Im BIOS „VT-d“ (Intel) bzw. „AMD-Vi/IOMMU“ einschalten und den Kernel mit <code>intel_iommu=on</code> bzw. <code>amd_iommu=on</code> starten.
    </Message>
    <template v-else>
      <p v-if="loaded && !usable.length && !missing.length" class="hint" data-test="pci-none">Es gibt kein PCI-Gerät, das sich durchreichen ließe.</p>
      <label v-for="g in usable" :key="`${g.id}-${g.devices[0].address}`" class="group" data-test="pci-group">
        <Checkbox :model-value="groupChecked(g)" binary :disabled="!editable" :data-test="`pci-${g.devices[0].address}`" @update:model-value="(v: boolean) => toggle(g, v)" />
        <span class="devs">
          <span v-if="g.devices.length > 1" class="hint">IOMMU-Gruppe {{ g.id }} – nur zusammen durchreichbar</span>
          <span v-for="d in g.devices" :key="d.address" class="dev">
            <strong>{{ d.name }}</strong>
            <Tag :value="kindLabel[d.kind] ?? d.kind" severity="secondary" />
            <small class="hint">{{ d.address }}{{ d.driver ? ` · Treiber ${d.driver}` : '' }}</small>
          </span>
        </span>
      </label>
      <div v-for="a in missing" :key="a" class="group missing" data-test="pci-missing">
        <span class="dev"><strong>{{ a }}</strong> <small class="hint">am NAS nicht (mehr) vorhanden</small></span>
        <button type="button" class="link" :disabled="!editable" @click="removeMissing(a)">entfernen</button>
      </div>
      <details v-if="blockedList.length" class="blocked" data-test="pci-blocked">
        <summary>{{ blockedList.length }} Gerät(e) sind gesperrt</summary>
        <ul>
          <li v-for="d in blockedList" :key="d.address">
            <strong>{{ d.name }}</strong> <small class="hint">{{ d.address }}</small><br />
            <small class="bad">{{ d.blocked }}</small>
          </li>
        </ul>
      </details>
    </template>
    <template v-if="selectedDevices.length">
      <Message v-for="d in selectedDevices.filter((x) => x.note)" :key="d.address" severity="warn" :closable="false" data-test="pci-note">{{ d.name }}: {{ d.note }}</Message>
      <p class="hint warn" data-test="pci-warn">Das Gerät gehört der VM ab dem Start und steht dem NAS nicht mehr zur Verfügung; nach dem Beenden der VM bekommt es der Host zurück. Der Arbeitsspeicher der VM wird dabei fest reserviert (RAM im Betrieb nicht verkleinerbar), und Snapshots sind nur bei ausgeschalteter VM möglich. Zwei VMs können dasselbe Gerät nicht gleichzeitig nutzen.</p>
    </template>
  </section>
</template>

<style scoped>
.pci {
  margin-top: 1.25rem;
  border-top: 1px solid var(--bn-border, rgba(128, 128, 128, 0.25));
  padding-top: 0.75rem;
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
  align-items: flex-start;
}
.pci h3 {
  margin: 0;
  font-size: 0.95rem;
}
.hint {
  margin: 0;
  color: var(--bn-text-soft);
  font-size: 0.8rem;
}
.hint.warn {
  color: var(--bn-warn);
}
.bad {
  color: var(--bn-danger);
}
.group {
  display: flex;
  gap: 0.6rem;
  align-items: flex-start;
  width: 100%;
}
.devs {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}
.dev {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
  align-items: center;
}
.missing {
  justify-content: space-between;
}
.link {
  background: none;
  border: 0;
  color: var(--p-primary-color, inherit);
  cursor: pointer;
  text-decoration: underline;
}
.blocked {
  font-size: 0.85rem;
}
.blocked summary {
  cursor: pointer;
}
.blocked ul {
  margin: 0.4rem 0 0;
  padding-left: 1.1rem;
}
</style>
