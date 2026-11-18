<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import Select from 'primevue/select'
import SelectButton from 'primevue/selectbutton'
import ToggleSwitch from 'primevue/toggleswitch'
import { useToast } from 'primevue/usetoast'
import { VM_NAME_RE, vmsApi, type VmSpec } from '@/api/vms'
import { useVmsStore } from '@/stores/vms'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [boolean] }>()

const store = useVmsStore()
const toast = useToast()

const step = ref(1)
const name = ref('')
const os = ref<'linux' | 'windows'>('linux')
const firmware = ref<'uefi' | 'bios'>('uefi')
const tpm = ref(false)
const vcpus = ref<number | null>(2)
const ramGiB = ref<number | null>(4)
const diskGiB = ref<number | null>(40)
const iso = ref('')
const driverIso = ref('')
const network = ref('default')
const start = ref(true)
const isos = ref<string[]>([])
const networks = ref<string[]>(['default'])
const ifaces = ref<string[]>([])
const busy = ref(false)
const error = ref('')

watch(
  () => props.visible,
  async (v) => {
    if (!v) return
    step.value = 1
    name.value = ''
    os.value = 'linux'
    firmware.value = 'uefi'
    tpm.value = false
    vcpus.value = 2
    ramGiB.value = 4
    diskGiB.value = 40
    iso.value = ''
    driverIso.value = ''
    network.value = 'default'
    start.value = true
    error.value = ''
    try {
      ;[isos.value, networks.value, ifaces.value] = await Promise.all([vmsApi.isos(), vmsApi.networks(), vmsApi.interfaces().catch(() => [])])
      if (networks.value.length && !networks.value.includes(network.value)) network.value = networks.value[0]
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  },
  { immediate: true },
)

// Standardwerte je Betriebssystem; Windows 11 verlangt UEFI und TPM.
watch(os, (o) => {
  diskGiB.value = o === 'windows' ? 80 : 40
  tpm.value = o === 'windows'
  if (o === 'windows') firmware.value = 'uefi'
  if (o !== 'windows') driverIso.value = ''
})
watch(firmware, (f) => {
  if (f === 'bios') tpm.value = false
})

const nameError = computed(() => (name.value && !VM_NAME_RE.test(name.value) ? 'Nur Kleinbuchstaben, Ziffern, - und _ (höchstens 40 Zeichen).' : ''))
const inRange = (n: number | null, lo: number, hi: number) => n !== null && Number.isInteger(n) && n >= lo && n <= hi
const step1Ok = computed(() => VM_NAME_RE.test(name.value))
const step2Ok = computed(() => inRange(vcpus.value, 1, 64) && inRange(ramGiB.value, 1, 256) && inRange(diskGiB.value, 1, 65536))

const spec = (): VmSpec => ({
  name: name.value,
  os: os.value,
  firmware: firmware.value,
  network: network.value,
  iso: iso.value,
  driverIso: driverIso.value,
  vcpus: vcpus.value ?? 0,
  memoryMiB: (ramGiB.value ?? 0) * 1024,
  diskGiB: diskGiB.value ?? 0,
  tpm: tpm.value,
})

async function submit() {
  busy.value = true
  error.value = ''
  try {
    await store.create(spec(), start.value)
    toast.add({ severity: 'success', summary: 'VM angelegt', detail: name.value, life: 4000 })
    emit('update:visible', false)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

const osOptions = [
  { label: 'Linux', value: 'linux' },
  { label: 'Windows', value: 'windows' },
]
const fwOptions = [
  { label: 'UEFI', value: 'uefi' },
  { label: 'BIOS', value: 'bios' },
]
const isoOptions = computed(() => [{ label: '— keine —', value: '' }, ...isos.value.map((i) => ({ label: i.endsWith('.qcow2') ? `${i} (Festplatten-Image)` : i, value: i }))])
const driverOptions = computed(() => [{ label: '— keine —', value: '' }, ...isos.value.filter((i) => !i.endsWith('.qcow2')).map((i) => ({ label: i, value: i }))])
const netOptions = computed(() => [
  ...networks.value.map((n) => ({ label: n === 'default' ? 'NAT (default) – Zugriff ins Internet, Host erreichbar' : n === 'bridge:br0' ? 'Bridge br0 – eigene IP im Heimnetz, Host erreichbar' : n, value: n })),
  ...ifaces.value.map((i) => ({ label: `LAN über ${i} – eigene IP im Heimnetz`, value: `direct:${i}` })),
])
const isImage = computed(() => iso.value.endsWith('.qcow2'))
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    header="Neue virtuelle Maschine"
    :style="{ width: 'min(560px, 96vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <p class="steps">Schritt {{ step }} von 3</p>

    <div v-if="step === 1" class="body">
      <div class="field">
        <label for="vm-name">Name</label>
        <InputText id="vm-name" v-model="name" :invalid="!!nameError" autocomplete="off" />
        <small v-if="nameError" class="err">{{ nameError }}</small>
      </div>
      <div class="field">
        <label>Betriebssystem</label>
        <SelectButton v-model="os" :options="osOptions" option-label="label" option-value="value" :allow-empty="false" data-test="vm-os" />
      </div>
      <div class="field">
        <label>Firmware</label>
        <SelectButton v-model="firmware" :options="fwOptions" option-label="label" option-value="value" :allow-empty="false" :disabled="os === 'windows'" data-test="vm-fw" />
      </div>
      <label v-if="os === 'windows'" class="row" data-test="vm-tpm-row">
        <ToggleSwitch v-model="tpm" data-test="vm-tpm" aria-label="TPM 2.0" />
        <span>TPM 2.0 (für Windows 11)</span>
      </label>
    </div>

    <div v-else-if="step === 2" class="body">
      <div class="field">
        <label for="vm-cpu">vCPUs</label>
        <InputNumber v-model="vcpus" input-id="vm-cpu" :min="1" :max="64" show-buttons />
      </div>
      <div class="field">
        <label for="vm-ram">Arbeitsspeicher (GiB)</label>
        <InputNumber v-model="ramGiB" input-id="vm-ram" :min="1" :max="256" show-buttons />
      </div>
      <div class="field">
        <label for="vm-disk">Festplatte (GiB)</label>
        <InputNumber v-model="diskGiB" input-id="vm-disk" :min="1" :max="65536" show-buttons />
        <small class="hint">Die Festplatte wächst bei Bedarf (qcow2).</small>
      </div>
    </div>

    <div v-else class="body">
      <div class="field">
        <label for="vm-iso">Installations-ISO oder Festplatten-Image (.qcow2)</label>
        <Select v-model="iso" input-id="vm-iso" :options="isoOptions" option-label="label" option-value="value" data-test="vm-iso" />
        <small v-if="!isos.length" class="hint">Noch keine Dateien – ISO- oder qcow2-Dateien lassen sich auf der VM-Seite über „ISO-Dateien“ hochladen.</small>
        <small v-else-if="isImage" class="hint" data-test="vm-image-hint">Das Image wird als primäre Festplatte kopiert; das Original bleibt unverändert, die Größe aus Schritt 2 gilt dann nicht.</small>
      </div>
      <div v-if="os === 'windows'" class="field">
        <label for="vm-drv">VirtIO-Treiber-ISO (optional)</label>
        <Select v-model="driverIso" input-id="vm-drv" :options="driverOptions" option-label="label" option-value="value" data-test="vm-driver" />
      </div>
      <div class="field">
        <label for="vm-net">Netzwerk</label>
        <Select v-model="network" input-id="vm-net" :options="netOptions" option-label="label" option-value="value" data-test="vm-network" />
        <small v-if="network.startsWith('direct:')" class="hint" data-test="vm-lan-hint">Die VM erscheint mit eigener IP im LAN. Dieses NAS selbst erreicht sie dabei nicht direkt (macvtap-Eigenschaft).</small>
      </div>
      <label class="row">
        <ToggleSwitch v-model="start" data-test="vm-start-after" aria-label="Nach dem Anlegen starten" />
        <span>Nach dem Anlegen starten</span>
      </label>
    </div>

    <Message v-if="error" severity="error" :closable="false" data-test="vm-error">{{ error }}</Message>

    <template #footer>
      <Button v-if="step > 1" label="Zurück" text data-test="vm-back" @click="step--" />
      <Button v-if="step === 1" label="Weiter" data-test="vm-next" :disabled="!step1Ok" @click="step = 2" />
      <Button v-else-if="step === 2" label="Weiter" data-test="vm-next" :disabled="!step2Ok" @click="step = 3" />
      <Button v-else label="Anlegen" data-test="vm-create" :disabled="busy" :loading="busy" @click="submit" />
    </template>
  </Dialog>
</template>

<style scoped>
.steps {
  margin: 0 0 1rem;
  color: var(--bn-text-soft);
  font-size: 0.85rem;
}
.body {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}
.row {
  display: flex;
  align-items: center;
  gap: 0.6rem;
}
.err {
  color: var(--bn-danger);
}
.hint {
  color: var(--bn-text-soft);
}
</style>
