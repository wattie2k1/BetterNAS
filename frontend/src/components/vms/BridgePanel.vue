<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Message from 'primevue/message'
import Select from 'primevue/select'
import { useToast } from 'primevue/usetoast'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { networkApi, type BridgeStatus } from '@/api/network'

defineProps<{ closable?: boolean }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const toast = useToast()

const status = ref<BridgeStatus | null>(null)
const iface = ref('')
const error = ref('')
const busy = ref(false)
const confirmOff = ref(false)
const now = ref(Math.floor(Date.now() / 1000))
let timer: ReturnType<typeof setInterval> | undefined

async function load(quiet = false) {
  try {
    status.value = await networkApi.bridge()
    if (!iface.value || !status.value.candidates.includes(iface.value)) iface.value = status.value.candidates[0] ?? ''
    if (!quiet) error.value = ''
  } catch (e) {
    // Während die Bridge umschaltet, ist die Verbindung kurz weg.
    if (!quiet) error.value = e instanceof Error ? e.message : String(e)
  }
}

function startPolling() {
  stopPolling()
  timer = setInterval(() => {
    now.value = Math.floor(Date.now() / 1000)
    if (status.value?.pending) void load(true)
  }, 2000)
}
function stopPolling() {
  if (timer) clearInterval(timer)
  timer = undefined
}

onMounted(() => {
  void load()
  startPolling()
})
onBeforeUnmount(stopPolling)

// Nach der Umstellung kann das NAS eine neue Adresse bekommen; dort muss die Bestätigung erfolgen.
const links = computed(() => {
  const s = status.value
  const list = (s?.addresses ?? []).map((a) => `https://${a}`)
  if (s?.hostname) list.push(`https://${s.hostname}.local`)
  return list
})
const remaining = computed(() => Math.max(0, (status.value?.deadline ?? 0) - now.value))

async function enable() {
  busy.value = true
  error.value = ''
  try {
    await networkApi.enableBridge(iface.value)
  } catch (e) {
    // Bricht die Verbindung beim Umschalten ab, ist die Bridge meist trotzdem eingerichtet: Status entscheidet.
    error.value = e instanceof Error ? e.message : String(e)
  }
  for (let i = 0; i < 10; i++) {
    await load(true)
    if (status.value?.pending) {
      error.value = ''
      break
    }
    await new Promise((r) => setTimeout(r, 1500))
  }
  busy.value = false
  emit('changed')
}

async function confirm() {
  busy.value = true
  try {
    await networkApi.confirmBridge()
    toast.add({ severity: 'success', summary: 'Bridge br0 bestätigt', life: 4000 })
    await load()
    emit('changed')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function disable() {
  busy.value = true
  try {
    await networkApi.disableBridge()
    confirmOff.value = false
    toast.add({ severity: 'success', summary: 'Bridge br0 zurückgenommen', life: 4000 })
    await load()
    emit('changed')
  } catch (e) {
    confirmOff.value = false
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div>
    <Message v-if="error" severity="error" :closable="false" data-test="bridge-error">{{ error }}</Message>
    <template v-if="status">
      <div v-if="status.active && status.pending" data-test="bridge-pending">
        <Message severity="warn" :closable="false">
          Die Bridge ist eingerichtet. Funktioniert die Verbindung zum NAS weiterhin, bestätige die Änderung binnen
          <strong>{{ remaining }} s</strong>. Sonst wird die alte Netzwerkkonfiguration automatisch wiederhergestellt.
        </Message>
        <p v-if="links.length" data-test="bridge-links">
          Erreichbar unter:
          <template v-for="(l, i) in links" :key="l"><template v-if="i">, </template><a :href="l">{{ l }}</a></template>
        </p>
      </div>
      <div v-else-if="status.active" data-test="bridge-active">
        <Message severity="success" :closable="false">Die Bridge <strong>br0</strong> ist aktiv und hängt an <strong>{{ status.interface }}</strong>. In der VM-Erstellung steht jetzt „Bridge br0“ als Netz zur Verfügung.</Message>
      </div>
      <div v-else data-test="bridge-off">
        <p>
          Mit einer Bridge bekommen VMs eine eigene IP-Adresse im Heimnetz, und das NAS selbst erreicht sie auch (anders als beim LAN-Netz über macvtap).
        </p>
        <Message severity="warn" :closable="false">
          Beim Einrichten wird die Netzwerkverbindung kurz unterbrochen. Bestätige die Änderung danach binnen 2 Minuten; ohne Bestätigung stellt das NAS die alte Konfiguration selbst wieder her.
        </Message>
        <Message severity="info" :closable="false" data-test="bridge-address-hint">
          <strong>Die Adresse des NAS kann sich ändern.</strong> Die Bridge bekommt per DHCP unter Umständen eine andere IP-Adresse als bisher. Öffnet sich die Seite danach nicht mehr, rufe
          <template v-if="status.hostname"><strong>https://{{ status.hostname }}.local</strong> oder </template>die neue Adresse aus der Geräteliste deines Routers auf und bestätige die Änderung dort. Hast du in der Zeit nicht bestätigt, wird alles nach 2 Minuten automatisch zurückgenommen und das NAS ist wieder unter der alten Adresse erreichbar.
        </Message>
        <div class="field">
          <label for="bridge-if">Netzwerkschnittstelle</label>
          <Select id="bridge-if" v-model="iface" :options="status.candidates" placeholder="Keine Schnittstelle gefunden" data-test="bridge-iface" />
        </div>
      </div>
    </template>
    <div class="actions">
      <Button v-if="closable" label="Schließen" text @click="emit('close')" />
      <Button v-if="status?.active && status.pending" data-test="bridge-confirm" label="Bestätigen" icon="pi pi-check" :loading="busy" @click="confirm" />
      <Button v-else-if="status?.active" data-test="bridge-disable" label="Zurücknehmen" severity="danger" icon="pi pi-undo" :disabled="busy" @click="confirmOff = true" />
      <Button v-else data-test="bridge-enable" label="Bridge einrichten" icon="pi pi-sitemap" :disabled="!iface" :loading="busy" @click="enable" />
    </div>
  </div>
  <ConfirmDialog
    :visible="confirmOff"
    header="Bridge zurücknehmen"
    message="Die Bridge br0 wird entfernt und die Schnittstelle wieder wie vorher konfiguriert (kurze Unterbrechung). Nur möglich, wenn keine VM die Bridge nutzt."
    :busy="busy"
    @update:visible="(v) => !v && (confirmOff = false)"
    @confirm="disable"
  />
</template>

<style scoped>
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.5rem;
  margin-top: 1rem;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
}
</style>
