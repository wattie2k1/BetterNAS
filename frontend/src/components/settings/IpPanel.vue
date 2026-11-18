<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import SelectButton from 'primevue/selectbutton'
import { useToast } from 'primevue/usetoast'
import { networkApi, type IpConfig, type IpStatus } from '@/api/network'
import { maskToPrefix, splitCidr } from '@/utils/netmask'

const toast = useToast()
const status = ref<IpStatus | null>(null)
const mode = ref<'dhcp' | 'static'>('dhcp')
const address = ref('')
const netmask = ref('255.255.255.0')
const gateway = ref('')
const dns = ref('')
const domains = ref('')
const error = ref('')
const busy = ref(false)
const now = ref(Math.floor(Date.now() / 1000))
let timer: ReturnType<typeof setInterval> | undefined

const modes = [
  { label: 'Automatisch (DHCP)', value: 'dhcp' },
  { label: 'Statisch', value: 'static' },
]
const split = (s: string) => s.split(/[\s,;]+/).filter(Boolean)
const remaining = computed(() => Math.max(0, (status.value?.deadline ?? 0) - now.value))

/** Formular aus dem Status füllen: gespeicherte statische Werte, sonst die aktuell wirksamen als Vorschlag. */
function fill(s: IpStatus) {
  mode.value = s.config.mode
  if (s.config.mode === 'static') {
    ;({ ip: address.value, mask: netmask.value } = splitCidr(s.config.address))
    gateway.value = s.config.gateway
    dns.value = s.config.dns.join(', ')
    domains.value = s.config.domains.join(', ')
  } else {
    ;({ ip: address.value, mask: netmask.value } = splitCidr(s.current.addresses[0] ?? ''))
    gateway.value = s.current.gateway
    dns.value = s.current.dns.filter((d) => !d.includes(':')).join(', ')
    domains.value = s.current.domains.join(', ')
  }
}

async function load(opts: { quiet?: boolean; fillForm?: boolean } = {}) {
  try {
    status.value = await networkApi.status()
    if (opts.fillForm) fill(status.value)
    if (!opts.quiet) error.value = ''
  } catch (e) {
    // Während der Umstellung ist die Verbindung kurz weg.
    if (!opts.quiet) error.value = e instanceof Error ? e.message : String(e)
  }
}

onMounted(() => {
  void load({ fillForm: true })
  timer = setInterval(() => {
    now.value = Math.floor(Date.now() / 1000)
    if (status.value?.pending) void load({ quiet: true })
  }, 2000)
})
onBeforeUnmount(() => timer && clearInterval(timer))

const links = computed(() => {
  const s = status.value
  const list = (s?.current.addresses ?? []).map((a) => `https://${a.split('/')[0]}`)
  if (s?.hostname) list.push(`https://${s.hostname}.local`)
  return list
})

async function apply() {
  error.value = ''
  let cidr = ''
  if (mode.value === 'static') {
    const prefix = maskToPrefix(netmask.value)
    if (prefix === null) {
      error.value = 'Ungültige Netzmaske (z. B. 255.255.255.0).'
      return
    }
    cidr = `${address.value.trim()}/${prefix}`
  }
  busy.value = true
  const cfg: IpConfig = { mode: mode.value, address: cidr, gateway: gateway.value.trim(), dns: split(dns.value), domains: split(domains.value) }
  try {
    await networkApi.configure(cfg)
  } catch (e) {
    // Bricht die Verbindung beim Umschalten ab, ist die Änderung meist trotzdem aktiv: Status entscheidet.
    error.value = e instanceof Error ? e.message : String(e)
  }
  for (let i = 0; i < 10; i++) {
    await load({ quiet: true })
    if (status.value?.pending) {
      error.value = ''
      break
    }
    if (error.value && /Ungültig|muss|Gib|darf|Höchstens|Eine /.test(error.value)) break
    await new Promise((r) => setTimeout(r, 1500))
  }
  busy.value = false
}

async function confirm() {
  busy.value = true
  try {
    await networkApi.confirm()
    toast.add({ severity: 'success', summary: 'Netzwerk-Änderung bestätigt', life: 4000 })
    await load({ fillForm: true })
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function revert() {
  busy.value = true
  try {
    await networkApi.revert()
    toast.add({ severity: 'success', summary: 'Netzwerk-Änderung zurückgenommen', life: 4000 })
    await load({ fillForm: true })
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div data-test="ip-panel">
    <Message v-if="error" severity="error" :closable="false" data-test="ip-error">{{ error }}</Message>
    <template v-if="status">
      <dl class="now" data-test="ip-current">
        <dt>Schnittstelle</dt>
        <dd>{{ status.interface }}<span v-if="status.bridgeActive" class="soft"> (Bridge)</span></dd>
        <dt>Adresse</dt>
        <dd>{{ status.current.addresses.join(', ') || '—' }}</dd>
        <dt>Gateway</dt>
        <dd>{{ status.current.gateway || '—' }}</dd>
        <dt>DNS</dt>
        <dd>{{ status.current.dns.join(', ') || '—' }}</dd>
      </dl>

      <div v-if="status.pending" data-test="ip-pending">
        <Message severity="warn" :closable="false">
          Die Änderung ist aktiv. Funktioniert die Verbindung zum NAS weiterhin, bestätige sie binnen <strong>{{ remaining }} s</strong>. Sonst wird die vorherige Konfiguration automatisch wiederhergestellt.
        </Message>
        <p v-if="links.length" data-test="ip-links">
          Erreichbar unter:
          <template v-for="(l, i) in links" :key="l"><template v-if="i">, </template><a :href="l">{{ l }}</a></template>
        </p>
        <div class="actions">
          <Button data-test="ip-revert" label="Zurücknehmen" severity="secondary" icon="pi pi-undo" :loading="busy" @click="revert" />
          <Button data-test="ip-confirm" label="Bestätigen" icon="pi pi-check" :loading="busy" @click="confirm" />
        </div>
      </div>

      <form v-else @submit.prevent="apply">
        <SelectButton v-model="mode" :options="modes" option-label="label" option-value="value" :allow-empty="false" data-test="ip-mode" aria-label="Konfiguration" />
        <div v-if="mode === 'static'" class="grid" data-test="ip-static">
          <label for="ip-address">IPv4-Adresse</label>
          <InputText id="ip-address" v-model="address" placeholder="192.168.1.10" autocomplete="off" />
          <label for="ip-netmask">Netzmaske</label>
          <InputText id="ip-netmask" v-model="netmask" placeholder="255.255.255.0" autocomplete="off" />
          <label for="ip-gateway">Gateway</label>
          <InputText id="ip-gateway" v-model="gateway" placeholder="192.168.1.1" autocomplete="off" />
          <label for="ip-dns">DNS-Server (bis zu 3)</label>
          <InputText id="ip-dns" v-model="dns" placeholder="192.168.1.1, 1.1.1.1" autocomplete="off" />
          <label for="ip-domains">Suchdomänen (optional)</label>
          <InputText id="ip-domains" v-model="domains" placeholder="fritz.box" autocomplete="off" />
        </div>
        <Message severity="info" :closable="false" data-test="ip-hint">
          <strong>Die Verbindung zum NAS wird kurz unterbrochen.</strong>
          Ändert sich die Adresse, öffnest du die Oberfläche unter der neuen Adresse
          <template v-if="status.hostname">(oder <strong>https://{{ status.hostname }}.local</strong>)</template>
          und bestätigst dort binnen 2 Minuten. Sonst stellt das NAS die alte Konfiguration selbst wieder her.
          Eine feste Adresse sollte außerhalb des DHCP-Bereichs deines Routers liegen.
        </Message>
        <div class="actions">
          <Button type="submit" data-test="ip-apply" label="Anwenden" icon="pi pi-check" :loading="busy" />
        </div>
      </form>
    </template>
  </div>
</template>

<style scoped>
.now {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 4px 16px;
  margin: 0 0 16px;
}
.now dt {
  color: var(--bn-text-soft);
}
.now dd {
  margin: 0;
}
.soft {
  color: var(--bn-text-soft);
}
.grid {
  display: grid;
  grid-template-columns: minmax(150px, 220px) 1fr;
  gap: 10px 14px;
  align-items: center;
  margin: 14px 0;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.5rem;
  margin-top: 1rem;
}
@media (max-width: 600px) {
  .grid {
    grid-template-columns: 1fr;
  }
}
</style>
