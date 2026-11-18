<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import Select from 'primevue/select'
import ToggleSwitch from 'primevue/toggleswitch'
import { useToast } from 'primevue/usetoast'
import { systemApi, type SystemSettings } from '@/api/system'

const toast = useToast()
const loaded = ref<SystemSettings | null>(null)
const zones = ref<string[]>([])
const hostname = ref('')
const timezone = ref('')
const ntp = ref(true)
const servers = ref('')
const error = ref('')
const busy = ref(false)
const renamed = ref('')

const split = (s: string) => s.split(/[\s,;]+/).filter(Boolean)
const dirty = computed(
  () =>
    !!loaded.value &&
    (hostname.value !== loaded.value.hostname || timezone.value !== loaded.value.timezone || ntp.value !== loaded.value.ntp || split(servers.value).join(' ') !== loaded.value.ntpServers.join(' ')),
)

function fill(s: SystemSettings) {
  loaded.value = s
  hostname.value = s.hostname
  timezone.value = s.timezone
  ntp.value = s.ntp
  servers.value = s.ntpServers.join(', ')
}

async function load() {
  try {
    const [s, z] = await Promise.all([systemApi.settings(), systemApi.timezones()])
    zones.value = z
    fill(s)
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}
onMounted(load)

async function save() {
  const old = loaded.value
  if (!old) return
  busy.value = true
  error.value = ''
  const change: Record<string, unknown> = {}
  if (hostname.value !== old.hostname) change.hostname = hostname.value.trim()
  if (timezone.value !== old.timezone) change.timezone = timezone.value
  if (ntp.value !== old.ntp) change.ntp = ntp.value
  if (split(servers.value).join(' ') !== old.ntpServers.join(' ')) change.ntpServers = split(servers.value)
  try {
    await systemApi.update(change)
    renamed.value = change.hostname ? String(change.hostname) : ''
    toast.add({ severity: 'success', summary: 'Einstellungen gespeichert', life: 3000 })
    fill(await systemApi.settings())
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="card" data-test="settings-general">
    <h3>Allgemein</h3>
    <Message v-if="error" severity="error" :closable="false" data-test="general-error">{{ error }}</Message>
    <Message v-if="renamed" severity="warn" :closable="false" data-test="general-renamed">
      Der NAS heißt jetzt <strong>{{ renamed }}</strong> und ist unter <strong>https://{{ renamed }}.local</strong> erreichbar. Erzeuge unter „Webzugriff“ ein neues Zertifikat, damit der Name darin steht.
    </Message>
    <form v-if="loaded" class="grid" @submit.prevent="save">
      <label for="g-host">Hostname</label>
      <InputText id="g-host" v-model="hostname" autocomplete="off" />
      <label for="g-zone">Zeitzone</label>
      <Select id="g-zone" v-model="timezone" :options="zones" filter data-test="general-zone" />
      <label for="g-ntp">Zeit automatisch synchronisieren</label>
      <span class="inline">
        <ToggleSwitch v-model="ntp" input-id="g-ntp" data-test="general-ntp" />
        <span class="soft">{{ loaded.ntpSynced ? 'synchronisiert' : 'nicht synchronisiert' }}</span>
      </span>
      <label for="g-servers">NTP-Server</label>
      <InputText id="g-servers" v-model="servers" placeholder="Leer = Standard der Distribution" autocomplete="off" :disabled="!ntp" />
      <span />
      <Button type="submit" data-test="general-save" label="Speichern" icon="pi pi-check" :disabled="!dirty" :loading="busy" class="save" />
    </form>
  </div>
</template>

<style scoped>
.card {
  padding: 20px 22px;
  max-width: 760px;
}
h3 {
  margin: 0 0 12px;
  font-size: 1rem;
}
.grid {
  display: grid;
  grid-template-columns: minmax(180px, 260px) 1fr;
  gap: 12px 14px;
  align-items: center;
}
.inline {
  display: flex;
  align-items: center;
  gap: 10px;
}
.soft {
  color: var(--bn-text-soft);
  font-size: 0.85rem;
}
.save {
  justify-self: start;
}
@media (max-width: 600px) {
  .grid {
    grid-template-columns: 1fr;
  }
}
</style>
