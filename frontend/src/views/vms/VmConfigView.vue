<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Button from 'primevue/button'
import Checkbox from 'primevue/checkbox'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Message from 'primevue/message'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import ToggleSwitch from 'primevue/toggleswitch'
import { useToast } from 'primevue/usetoast'
import { vmsApi, type VmConfig, type VmConfigUpdate, type VmDisk, type VmLiveUpdate, type VmUsbDevice } from '@/api/vms'
import DiskAddDialog from '@/components/vms/DiskAddDialog.vue'
import DiskRemoveDialog from '@/components/vms/DiskRemoveDialog.vue'
import DiskResizeDialog from '@/components/vms/DiskResizeDialog.vue'
import VmIconDialog from '@/components/vms/VmIconDialog.vue'
import RenameDialog from '@/components/vms/RenameDialog.vue'
import VmAdvanced, { type AdvancedState } from '@/components/vms/VmAdvanced.vue'
import VmPciDevices, { type PciState } from '@/components/vms/VmPciDevices.vue'
import VmSharedDirs, { type SharedState } from '@/components/vms/VmSharedDirs.vue'
import { useVmsStore } from '@/stores/vms'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const store = useVmsStore()
const name = String(route.params.name ?? '')

const cfg = ref<VmConfig | null>(null)
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const vcpus = ref<number | null>(2)
const ramMiB = ref<number | null>(2048)
// Hot-Plug-Spielraum: Obergrenzen, bis zu denen sich RAM und vCPUs im Betrieb ändern lassen.
const headroom = ref(false)
const maxVcpus = ref<number | null>(4)
const maxRam = ref<number | null>(4096)
const secureBoot = ref(false)
const guestAgent = ref(false)
const network = ref('default')
const media = ref('')
const isos = ref<string[]>([])
const networks = ref<string[]>([])
const ifaces = ref<string[]>([])
const bootFirst = ref<'hd' | 'cdrom'>('hd')
const mac = ref('')
const usbSel = ref<string[]>([])
const usbHost = ref<VmUsbDevice[]>([])
const usbKey = (d: VmUsbDevice) => `${d.vendor}:${d.product}`
const MAC_RE = /^([0-9a-f]{2}:){5}[0-9a-f]{2}$/i
const macValid = computed(() => {
  if (mac.value.toLowerCase() === (cfg.value?.mac ?? '').toLowerCase()) return true // unverändert
  if (!MAC_RE.test(mac.value)) return false
  return (parseInt(mac.value.slice(0, 2), 16) & 1) === 0
})
/** Angeschlossene Geräte plus die der VM zugewiesenen, aktuell nicht angeschlossenen. */
const usbOptions = computed(() => {
  const list = [...usbHost.value]
  for (const d of cfg.value?.usb ?? []) if (!list.some((h) => usbKey(h) === usbKey(d))) list.push({ ...d, name: `${d.vendor}:${d.product} (nicht angeschlossen)` })
  return list
})
const usbChanged = computed(() => {
  const cur = (cfg.value?.usb ?? []).map(usbKey).sort().join(',')
  return [...usbSel.value].sort().join(',') !== cur
})
const usbStorageSel = computed(() => usbOptions.value.some((d) => d.storage && usbSel.value.includes(usbKey(d))))
const adv = ref<AdvancedState>({ dirty: false, valid: true, update: {} })
const pciState = ref<PciState>({ dirty: false, valid: true, update: {} })
const shared = ref<SharedState>({ dirty: false, valid: true, update: {} })
const resetKey = ref(0)
const disks = ref<VmDisk[]>([])
const resizeDisk = ref<VmDisk | null>(null)
const resizeOpen = ref(false)
const addOpen = ref(false)
const renameOpen = ref(false)
const iconOpen = ref(false)
async function renamed(newName: string) {
  void store.refresh()
  // Die Ansicht gehört zum alten Namen; über die Liste neu einsteigen.
  await router.replace('/vms')
  await router.push(`/vms/${encodeURIComponent(newName)}`)
}
const removeDisk = ref<VmDisk | null>(null)
const removeOpen = ref(false)
function openRemove(d: VmDisk) {
  removeDisk.value = d
  removeOpen.value = true
}

function openResize(d: VmDisk) {
  resizeDisk.value = d
  resizeOpen.value = true
}
const fmtGiB = (bytes: number) => (bytes / 1024 ** 3).toFixed(1).replace(/\.0$/, '')
async function loadDisks() {
  disks.value = await vmsApi.disks(name).catch(() => [])
}

const editable = computed(() => cfg.value?.state === 'shutoff')
/** Läuft die VM, wirken RAM, vCPUs, USB, CD-Medium und Festplattengröße sofort (bis zu den Maxima). */
const live = computed(() => cfg.value?.state === 'running' || cfg.value?.state === 'paused')
const liveEditable = computed(() => editable.value || live.value)
const inRange = (n: number | null, lo: number, hi: number) => n !== null && Number.isInteger(n) && n >= lo && n <= hi
const effMaxCpus = computed(() => (headroom.value ? (maxVcpus.value ?? 0) : (vcpus.value ?? 0)))
const effMaxRam = computed(() => (headroom.value ? (maxRam.value ?? 0) : (ramMiB.value ?? 0)))
const vcpusMax = computed(() => (live.value ? (cfg.value?.maxVcpus ?? 64) : 64))
const ramMax = computed(() => (live.value ? (cfg.value?.maxMemoryMiB ?? 262144) : 262144))
const headroomValid = computed(() => !headroom.value || (inRange(maxVcpus.value, vcpus.value ?? 1, 64) && inRange(maxRam.value, ramMiB.value ?? 256, 262144)))
const valid = computed(() => inRange(vcpus.value, 1, vcpusMax.value) && inRange(ramMiB.value, 256, ramMax.value) && headroomValid.value && macValid.value && adv.value.valid && shared.value.valid && pciState.value.valid)
const dirty = computed(
  () =>
    !!cfg.value &&
    (vcpus.value !== cfg.value.vcpus ||
      ramMiB.value !== cfg.value.memoryMiB ||
      secureBoot.value !== cfg.value.secureBoot ||
      guestAgent.value !== cfg.value.guestAgent ||
      network.value !== cfg.value.network ||
      media.value !== cfg.value.media ||
      (cfg.value.boot !== 'custom' && bootFirst.value !== (cfg.value.boot ?? 'hd')) ||
      mac.value.toLowerCase() !== (cfg.value.mac ?? '').toLowerCase() ||
      usbChanged.value ||
      (!live.value && (effMaxCpus.value !== (cfg.value.maxVcpus ?? cfg.value.vcpus) || effMaxRam.value !== (cfg.value.maxMemoryMiB ?? cfg.value.memoryMiB))) ||
      adv.value.dirty ||
      shared.value.dirty ||
      pciState.value.dirty),
)
const secureBootChanged = computed(() => !!cfg.value && secureBoot.value !== cfg.value.secureBoot)

const netOptions = computed(() => {
  const opts = [
    ...networks.value.map((n) => ({ label: n === 'default' ? 'NAT (default) – Zugriff ins Internet, Host erreichbar' : n === 'bridge:br0' ? 'Bridge br0 – eigene IP im Heimnetz, Host erreichbar' : n, value: n })),
    ...ifaces.value.map((i) => ({ label: `LAN über ${i} – eigene IP im Heimnetz`, value: `direct:${i}` })),
  ]
  // Ein aktuell gesetztes, nicht (mehr) verfügbares Netz bleibt sichtbar.
  if (cfg.value && !opts.some((o) => o.value === cfg.value!.network)) opts.push({ label: cfg.value.network, value: cfg.value.network })
  return opts
})
const mediaOptions = computed(() => [{ label: 'Leer (kein Medium eingelegt)', value: '' }, ...isos.value.filter((f) => f.endsWith('.iso')).map((f) => ({ label: f, value: f }))])

const stateLabel: Record<string, string> = { running: 'Läuft', shutoff: 'Aus', paused: 'Pausiert', shuttingdown: 'Fährt herunter', crashed: 'Abgestürzt', other: 'Unbekannt' }

function apply(c: VmConfig) {
  resetKey.value++
  cfg.value = c
  vcpus.value = c.vcpus
  ramMiB.value = c.memoryMiB
  const mv = c.maxVcpus ?? c.vcpus
  const mr = c.maxMemoryMiB ?? c.memoryMiB
  headroom.value = mv > c.vcpus || mr > c.memoryMiB
  maxVcpus.value = headroom.value ? mv : Math.min(64, c.vcpus * 2)
  maxRam.value = headroom.value ? mr : Math.min(262144, c.memoryMiB * 2)
  secureBoot.value = c.secureBoot
  guestAgent.value = c.guestAgent
  network.value = c.network
  media.value = c.media
  bootFirst.value = c.boot === 'cdrom' ? 'cdrom' : 'hd'
  mac.value = c.mac ?? ''
  usbSel.value = (c.usb ?? []).map(usbKey)
}

async function load() {
  loading.value = true
  try {
    const [c, i, n, f] = await Promise.all([vmsApi.config(name), vmsApi.isos(), vmsApi.networks(), vmsApi.interfaces().catch(() => []), loadDisks()])
    usbHost.value = await vmsApi.usb().catch(() => [])
    isos.value = i
    networks.value = n
    ifaces.value = f
    apply(c)
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}
onMounted(load)

async function save() {
  if (!cfg.value || !valid.value) return
  saving.value = true
  try {
    const c = cfg.value
    if (live.value) {
      const u: VmLiveUpdate = {}
      if (vcpus.value !== c.vcpus) u.vcpus = vcpus.value!
      if (ramMiB.value !== c.memoryMiB) u.memoryMiB = ramMiB.value!
      if (media.value !== c.media) u.media = media.value
      if (usbChanged.value) u.usb = usbOptions.value.filter((d) => usbSel.value.includes(usbKey(d))).map(({ vendor, product }) => ({ vendor, product }))
      await vmsApi.live(name, u)
      toast.add({ severity: 'success', summary: 'Änderungen übernommen', detail: `${name} – wirken sofort`, life: 4000 })
      await load()
      void store.refresh()
      return
    }
    const upd: VmConfigUpdate = { vcpus: vcpus.value!, memoryMiB: ramMiB.value!, secureBoot: secureBoot.value, network: network.value, media: media.value, guestAgent: guestAgent.value }
    if (effMaxCpus.value > vcpus.value!) upd.maxVcpus = effMaxCpus.value
    if (effMaxRam.value > ramMiB.value!) upd.maxMemoryMiB = effMaxRam.value
    if (c.boot !== 'custom' && bootFirst.value !== (c.boot ?? 'hd')) upd.bootFirst = bootFirst.value
    if (mac.value.toLowerCase() !== (c.mac ?? '').toLowerCase()) upd.mac = mac.value.toLowerCase()
    if (usbChanged.value) upd.usb = usbOptions.value.filter((d) => usbSel.value.includes(usbKey(d))).map(({ vendor, product }) => ({ vendor, product }))
    await vmsApi.configure(name, { ...upd, ...adv.value.update, ...shared.value.update, ...pciState.value.update })
    toast.add({ severity: 'success', summary: 'Einstellungen gespeichert', detail: name, life: 4000 })
    await load()
    void store.refresh()
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    saving.value = false
  }
}

async function toggleAutostart(on: boolean) {
  try {
    await vmsApi.action(name, on ? 'autostart' : 'noautostart')
    if (cfg.value) cfg.value.autostart = on
    void store.refresh()
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  }
}

const back = () => router?.push('/vms')
</script>

<template>
  <div class="bar">
    <Button icon="pi pi-arrow-left" label="Zurück" text data-test="cfg-back" @click="back" />
    <span class="spacer" />
  </div>
  <Message v-if="error" severity="error" :closable="false" data-test="cfg-error">{{ error }}</Message>
  <section v-if="cfg" class="card">
    <header>
      <h2>
        {{ cfg.name }}
        <Button icon="pi pi-pencil" size="small" text rounded aria-label="VM umbenennen" :disabled="!editable" data-test="cfg-rename" @click="renameOpen = true" />
        <Button icon="pi pi-image" size="small" text rounded aria-label="Icon ändern" data-test="cfg-icon" @click="iconOpen = true" />
      </h2>
      <Tag :value="stateLabel[cfg.state] ?? cfg.state" :severity="cfg.state === 'running' ? 'success' : cfg.state === 'shutoff' ? 'secondary' : 'warn'" />
    </header>
    <Message v-if="live" severity="info" :closable="false" data-test="cfg-locked">Die VM läuft. Arbeitsspeicher und vCPUs (bis zu den Maxima), USB-Geräte, das CD-Medium und die Festplattengröße wirken sofort („Live übernehmen“). Alles andere lässt sich nur bei ausgeschalteter VM ändern; der Autostart immer.</Message>
    <Message v-else-if="!editable" severity="info" :closable="false" data-test="cfg-locked">Zum Ändern der Einstellungen muss die VM ausgeschaltet sein. Nur der Autostart lässt sich jederzeit ändern.</Message>

    <div class="field">
      <label for="cfg-vcpus">vCPUs</label>
      <InputNumber v-model="vcpus" input-id="cfg-vcpus" :min="1" :max="vcpusMax" show-buttons :disabled="!liveEditable" data-test="cfg-vcpus" />
      <small v-if="live" class="hint" data-test="cfg-live-cpu">Wirkt sofort, bis höchstens {{ cfg.maxVcpus }}. Mehr geht nur bei ausgeschalteter VM (Maximum erhöhen).</small>
    </div>
    <div class="field">
      <label for="cfg-ram">Arbeitsspeicher (MiB)</label>
      <InputNumber v-model="ramMiB" input-id="cfg-ram" :min="256" :max="ramMax" :step="256" show-buttons :disabled="!liveEditable" data-test="cfg-ram" />
      <small v-if="live" class="hint" data-test="cfg-live-ram">Wirkt sofort, bis höchstens {{ cfg.maxMemoryMiB }} MiB. Der Gast braucht dafür den Balloon-Treiber (Linux: eingebaut, Windows: virtio-win).</small>
      <small class="hint">{{ ramMiB ? (ramMiB / 1024).toFixed(2).replace(/\.?0+$/, '') : '–' }} GiB</small>
    </div>
    <div v-if="!live" class="field">
      <label class="row">
        <ToggleSwitch v-model="headroom" :disabled="!editable" aria-label="Hot-Plug" data-test="cfg-headroom" />
        <span>RAM und vCPUs im Betrieb änderbar machen (Hot-Plug)</span>
      </label>
      <small class="hint">Legt Obergrenzen fest, bis zu denen sich Arbeitsspeicher und vCPUs bei laufender VM ändern lassen. Die VM startet mit den Werten oben. Ohne Spielraum geht das nur bei ausgeschalteter VM.</small>
      <div v-if="headroom" class="two">
        <div class="field">
          <label for="cfg-maxcpu">Maximale vCPUs</label>
          <InputNumber v-model="maxVcpus" input-id="cfg-maxcpu" :min="vcpus ?? 1" :max="64" show-buttons :disabled="!editable" data-test="cfg-maxcpu" />
        </div>
        <div class="field">
          <label for="cfg-maxram">Maximaler Arbeitsspeicher (MiB)</label>
          <InputNumber v-model="maxRam" input-id="cfg-maxram" :min="ramMiB ?? 256" :max="262144" :step="256" show-buttons :disabled="!editable" data-test="cfg-maxram" />
        </div>
      </div>
      <small v-if="!headroomValid" class="hint bad" data-test="cfg-headroom-bad">Die Maxima müssen mindestens so groß sein wie die Werte oben.</small>
    </div>
    <div class="field">
      <label for="cfg-net">Netzwerk</label>
      <Select v-model="network" input-id="cfg-net" :options="netOptions" option-label="label" option-value="value" :disabled="!editable" data-test="cfg-network" />
      <small v-if="network.startsWith('direct:')" class="hint">Die VM erscheint mit eigener IP im LAN. Dieses NAS selbst erreicht sie dabei nicht direkt (macvtap-Eigenschaft).</small>
      <small v-else-if="network === 'default'" class="hint">Hinter NAT ist die VM aus dem Heimnetz nicht erreichbar – für Dienste wie Home Assistant besser LAN oder Bridge wählen.</small>
    </div>
    <div class="field">
      <label for="cfg-mac">MAC-Adresse der Netzwerkkarte</label>
      <InputText v-model="mac" input-id="cfg-mac" :invalid="!macValid" :disabled="!editable" placeholder="52:54:00:12:34:56" data-test="cfg-mac" />
      <small class="hint">{{ macValid ? 'Für eine DHCP-Reservierung im Router oder um die Adresse einer anderen Maschine zu übernehmen.' : 'Ungültig – erwartet wird z. B. 52:54:00:12:34:56 (keine Multicast-Adresse).' }}</small>
    </div>
    <div class="field">
      <label for="cfg-media">CD-/DVD-Laufwerk</label>
      <Select v-model="media" input-id="cfg-media" :options="mediaOptions" option-label="label" option-value="value" :disabled="!liveEditable || (live && !cfg.hasCdrom)" data-test="cfg-media" />
      <small class="hint">{{ cfg.hasCdrom ? 'Das Medium wird beim nächsten Start vor der Festplatte gebootet.' : 'Die VM hat noch kein Laufwerk; es wird beim Speichern hinzugefügt.' }}</small>
    </div>
    <div class="field">
      <label for="cfg-boot">Starten von</label>
      <Select
        v-model="bootFirst"
        input-id="cfg-boot"
        :options="[{ label: 'Festplatte zuerst', value: 'hd' }, { label: 'CD/ISO zuerst', value: 'cdrom' }]"
        option-label="label"
        option-value="value"
        :disabled="!editable || cfg.boot === 'custom'"
        data-test="cfg-boot"
      />
      <small class="hint">{{ cfg.boot === 'custom' ? 'Die Boot-Reihenfolge dieser VM ist pro Gerät festgelegt und lässt sich hier nicht ändern.' : 'Mit „Festplatte zuerst“ bootet die VM nach der Installation direkt vom System, auch wenn noch eine ISO eingelegt ist.' }}</small>
    </div>
    <div class="field">
      <label>USB-Geräte</label>
      <ul v-if="usbOptions.length" class="usb" data-test="cfg-usb">
        <li v-for="d in usbOptions" :key="usbKey(d)">
          <label class="row">
            <Checkbox v-model="usbSel" :value="usbKey(d)" :disabled="!liveEditable" :input-id="`usb-${usbKey(d)}`" :data-test="`cfg-usb-${usbKey(d)}`" />
            <span>{{ d.name || usbKey(d) }} <small class="hint">{{ usbKey(d) }}</small></span>
          </label>
        </li>
      </ul>
      <small v-else class="hint">Am NAS ist kein USB-Gerät angeschlossen.</small>
      <small class="hint">Die Geräte gehören der VM ab dem nächsten Start und sind dann am NAS nicht mehr nutzbar. Ist ein Gerät nicht angeschlossen, startet die VM trotzdem.</small>
      <Message v-if="usbStorageSel" severity="warn" :closable="false" data-test="cfg-usb-storage">Ein ausgewähltes Gerät ist ein Massenspeicher. Ist es am NAS als Datenplatte eingebunden, fällt diese für das NAS weg.</Message>
    </div>
    <div v-if="cfg.firmware === 'uefi'" class="field">
      <label class="row">
        <ToggleSwitch v-model="secureBoot" :disabled="!editable" aria-label="Secure Boot" data-test="cfg-secureboot" />
        <span>Secure Boot</span>
      </label>
      <small class="hint">Nur für signierte Systeme (z. B. Windows 11). Home Assistant OS und viele Linux-Images starten damit nicht („Access Denied“).</small>
      <Message v-if="secureBootChanged" severity="warn" :closable="false" data-test="cfg-sb-warn">Beim Ändern werden die gespeicherten UEFI-Einstellungen der VM zurückgesetzt.</Message>
    </div>
    <div class="field">
      <label class="row">
        <ToggleSwitch v-model="guestAgent" :disabled="!editable" aria-label="Guest-Agent" data-test="cfg-guestagent" />
        <span>QEMU-Guest-Agent</span>
      </label>
      <small class="hint">Ermöglicht sauberes Herunterfahren aus der Oberfläche. Das System in der VM braucht den Agent (Home Assistant OS bringt ihn mit; unter Linux: Paket qemu-guest-agent). Ohne ihn reagieren manche Systeme nicht auf „Herunterfahren“.</small>
    </div>
    <div class="field">
      <label class="row">
        <ToggleSwitch :model-value="cfg.autostart" aria-label="Autostart" data-test="cfg-autostart" @update:model-value="toggleAutostart" />
        <span>Autostart beim Hochfahren des NAS</span>
      </label>
    </div>

    <div class="field">
      <label>Festplatten</label>
      <ul v-if="disks.length" class="disks" data-test="cfg-disks">
        <li v-for="(d, i) in disks" :key="d.path">
          <div class="disk-info">
            <span class="disk-path">{{ d.path }}</span>
            <small class="hint">{{ d.sizeGiB ? `${d.sizeGiB} GiB, belegt ${fmtGiB(d.usedBytes)} GiB` : 'Größe unbekannt' }}</small>
          </div>
          <div class="disk-actions">
            <Button label="Vergrößern" icon="pi pi-arrows-v" size="small" severity="secondary" :disabled="!liveEditable || !d.sizeGiB || !d.path.startsWith('/srv/vms/disks/')" data-test="cfg-disk-resize" @click="openResize(d)" />
            <Button v-if="i > 0" icon="pi pi-trash" size="small" severity="danger" text aria-label="Festplatte entfernen" :disabled="!editable" data-test="cfg-disk-remove" @click="openRemove(d)" />
          </div>
        </li>
      </ul>
      <span v-else class="hint">{{ cfg.disks.join(', ') || '–' }}</span>
      <div>
        <Button label="Festplatte hinzufügen" icon="pi pi-plus" size="small" severity="secondary" :disabled="!editable || !disks.length" data-test="cfg-disk-add" @click="addOpen = true" />
      </div>
    </div>
    <DiskResizeDialog v-model:visible="resizeOpen" :vm="name" :disk="resizeDisk" :live="live" @resized="load" />
    <VmIconDialog v-model:visible="iconOpen" :vm="name" />
  <RenameDialog v-model:visible="renameOpen" :vm="name" @renamed="renamed" />
    <DiskAddDialog v-model:visible="addOpen" :vm="name" @added="load" />
    <DiskRemoveDialog v-model:visible="removeOpen" :vm="name" :disk="removeDisk" @removed="load" />

    <VmSharedDirs :cfg="cfg" :editable="editable" :reset="resetKey" @change="shared = $event" />
    <VmPciDevices :cfg="cfg" :editable="editable" :reset="resetKey" @change="pciState = $event" />
    <VmAdvanced :cfg="cfg" :vcpus="effMaxCpus || vcpus" :editable="editable" :net-options="netOptions" :reset="resetKey" @change="adv = $event" />

    <dl class="info">
      <dt>Firmware</dt>
      <dd>{{ cfg.firmware === 'uefi' ? 'UEFI' : 'BIOS' }}{{ cfg.tpm ? ' · TPM 2.0' : '' }}</dd>
    </dl>

    <footer>
      <Button label="Zurücksetzen" text :disabled="!dirty || saving" data-test="cfg-reset" @click="cfg && apply(cfg)" />
      <Button :label="live ? 'Live übernehmen' : 'Speichern'" :icon="live ? 'pi pi-bolt' : 'pi pi-check'" :loading="saving" :disabled="!liveEditable || !dirty || !valid" data-test="cfg-save" @click="save" />
    </footer>
  </section>
  <p v-else-if="!loading && !error" class="empty">VM nicht gefunden.</p>
</template>

<style scoped>
.bar {
  display: flex;
  margin-bottom: 1rem;
}
.spacer {
  flex: 1;
}
.card {
  max-width: 640px;
}
.card header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}
.card h2 {
  margin: 0;
  font-size: 1.2rem;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  margin-top: 1rem;
}
.row {
  display: flex;
  align-items: center;
  gap: 0.6rem;
}
.hint,
.empty {
  color: var(--bn-text-soft);
  font-size: 0.8rem;
}
.two {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 1rem;
}
.hint.bad {
  color: var(--bn-danger);
}
.usb {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.disks {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.disks li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}
.disk-actions {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  flex-shrink: 0;
}
.disk-info {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.disk-path {
  font-size: 0.85rem;
  word-break: break-all;
}
.info {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 0.3rem 1rem;
  margin: 1.25rem 0 0;
  font-size: 0.85rem;
  color: var(--bn-text-soft);
}
.info dd {
  margin: 0;
  word-break: break-all;
}
footer {
  display: flex;
  justify-content: flex-end;
  gap: 0.5rem;
  margin-top: 1.25rem;
}
</style>
