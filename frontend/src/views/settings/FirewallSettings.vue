<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import Select from 'primevue/select'
import ToggleSwitch from 'primevue/toggleswitch'
import { useToast } from 'primevue/usetoast'
import { firewallApi, type FirewallConfig, type FirewallMode, type FirewallStatus } from '@/api/firewall'

const toast = useToast()
const status = ref<FirewallStatus | null>(null)
const enabled = ref(false)
const modes = ref<Record<string, FirewallMode>>({})
const nets = ref<Record<string, string>>({})
const error = ref('')
const busy = ref(false)
const now = ref(Math.floor(Date.now() / 1000))
let timer: ReturnType<typeof setInterval> | undefined

const modeOptions = [
  { label: 'Aus jedem Netz', value: 'all' },
  { label: 'Nur aus dem lokalen Netz', value: 'lan' },
  { label: 'Nur aus diesen Netzen', value: 'custom' },
]
const split = (s: string) => s.split(/[\s,;]+/).filter(Boolean)
const remaining = computed(() => Math.max(0, (status.value?.deadline ?? 0) - now.value))

function fill(s: FirewallStatus) {
  enabled.value = s.config.enabled
  for (const svc of s.services) {
    const r = s.config.rules[svc.id] ?? { mode: 'all', networks: [] }
    modes.value[svc.id] = r.mode
    nets.value[svc.id] = r.networks.join(', ')
  }
}

async function load(opts: { quiet?: boolean; fillForm?: boolean } = {}) {
  try {
    status.value = await firewallApi.status()
    if (opts.fillForm) fill(status.value)
    if (!opts.quiet) error.value = ''
  } catch (e) {
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

const dirty = computed(() => {
  const s = status.value
  if (!s) return false
  if (enabled.value !== s.config.enabled) return true
  return s.services.some((svc) => {
    const r = s.config.rules[svc.id] ?? { mode: 'all', networks: [] }
    return modes.value[svc.id] !== r.mode || (modes.value[svc.id] === 'custom' && split(nets.value[svc.id] ?? '').join(',') !== r.networks.join(','))
  })
})

function build(): FirewallConfig {
  const rules: FirewallConfig['rules'] = {}
  for (const svc of status.value?.services ?? []) {
    const mode = modes.value[svc.id] ?? 'all'
    rules[svc.id] = { mode, networks: mode === 'custom' ? split(nets.value[svc.id] ?? '') : [] }
  }
  return { enabled: enabled.value, rules }
}

async function apply() {
  busy.value = true
  error.value = ''
  try {
    await firewallApi.apply(build())
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
  await load({ quiet: true })
  busy.value = false
}

async function confirm() {
  busy.value = true
  try {
    await firewallApi.confirm()
    toast.add({ severity: 'success', summary: 'Firewall-Änderung bestätigt', life: 4000 })
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
    await firewallApi.revert()
    toast.add({ severity: 'success', summary: 'Firewall-Änderung zurückgenommen', life: 4000 })
    await load({ fillForm: true })
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="card" data-test="settings-firewall">
    <h2>Firewall</h2>
    <Message v-if="error" severity="error" :closable="false" data-test="fw-error">{{ error }}</Message>
    <template v-if="status">
      <div v-if="status.pending" data-test="fw-pending">
        <Message severity="warn" :closable="false">
          Die Änderung ist aktiv. Erreichst du die Oberfläche weiterhin, bestätige sie binnen <strong>{{ remaining }} s</strong>. Sonst werden die vorherigen Regeln automatisch wiederhergestellt.
        </Message>
        <div class="actions">
          <Button data-test="fw-revert" label="Zurücknehmen" severity="secondary" icon="pi pi-undo" :loading="busy" @click="revert" />
          <Button data-test="fw-confirm" label="Bestätigen" icon="pi pi-check" :loading="busy" @click="confirm" />
        </div>
      </div>

      <form v-else @submit.prevent="apply">
        <div class="switch">
          <ToggleSwitch v-model="enabled" input-id="fw-enabled" data-test="fw-enabled" />
          <label for="fw-enabled">Firewall aktiv</label>
          <span class="soft">{{ status.active ? 'läuft' : 'aus' }}</span>
        </div>
        <Message severity="info" :closable="false">
          Ist die Firewall aktiv, werden alle eingehenden Verbindungen blockiert, die unten nicht erlaubt sind. Ausgehender Verkehr, Docker-Container mit veröffentlichten Ports und virtuelle Maschinen sind nicht betroffen.
        </Message>
        <table class="rules" data-test="fw-rules">
          <thead>
            <tr><th>Dienst</th><th>Erreichbar</th><th>Netze</th></tr>
          </thead>
          <tbody>
            <tr v-for="svc in status.services" :key="svc.id" :data-test="`fw-${svc.id}`">
              <td>
                {{ svc.label }}
                <div class="soft ports">{{ svc.ports.join(', ') }}</div>
              </td>
              <td>
                <Select v-model="modes[svc.id]" :options="modeOptions" option-label="label" option-value="value" :aria-label="`Zugriff auf ${svc.label}`" :data-test="`fw-mode-${svc.id}`" />
              </td>
              <td>
                <InputText v-if="modes[svc.id] === 'custom'" v-model="nets[svc.id]" placeholder="192.168.1.0/24, 10.0.0.5" autocomplete="off" :aria-label="`Netze für ${svc.label}`" :data-test="`fw-nets-${svc.id}`" />
                <span v-else class="soft">{{ modes[svc.id] === 'lan' ? 'private Adressbereiche' : '—' }}</span>
              </td>
            </tr>
          </tbody>
        </table>
        <Message severity="warn" :closable="false" data-test="fw-hint">
          Nach dem Anwenden hast du 2 Minuten Zeit, die Änderung zu bestätigen. Bleibt die Bestätigung aus, stellt das NAS die vorherigen Regeln selbst wieder her.
        </Message>
        <div class="actions">
          <Button type="submit" data-test="fw-apply" label="Anwenden" icon="pi pi-check" :disabled="!dirty" :loading="busy" />
        </div>
      </form>
    </template>
  </div>
</template>

<style scoped>
.card {
  max-width: 860px;
}
.switch {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
}
.soft {
  color: var(--bn-text-soft);
}
.ports {
  font-size: 0.8rem;
}
.rules {
  width: 100%;
  border-collapse: collapse;
  margin: 14px 0;
}
.rules th {
  text-align: left;
  font-size: 0.8rem;
  color: var(--bn-text-soft);
  padding: 4px 8px 8px 0;
}
.rules td {
  padding: 8px 8px 8px 0;
  vertical-align: middle;
}
.rules td :deep(.p-select),
.rules td :deep(.p-inputtext) {
  width: 100%;
}
.actions {
  display: flex;
  gap: 10px;
  margin-top: 12px;
}
</style>
