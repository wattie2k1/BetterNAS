<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import Tag from 'primevue/tag'
import ToggleSwitch from 'primevue/toggleswitch'
import { useToast } from 'primevue/usetoast'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import ProxySiteDialog from '@/components/docker/ProxySiteDialog.vue'
import { dockerApi, type ProxyConfig, type ProxySite } from '@/api/docker'
import DockerNotConfigured from '@/components/docker/DockerNotConfigured.vue'
import { useDockerStore } from '@/stores/docker'
import { useJobsStore } from '@/stores/jobs'
import { CADDY_CONTAINER } from '@/utils/proxy'

const store = useDockerStore()
const jobs = useJobsStore()
const toast = useToast()

const empty = (): ProxyConfig => ({ enabled: false, email: '', httpPort: 8080, httpsPort: 8443, sites: [] })
const form = ref<ProxyConfig>(empty())
const saved = ref('')
const loaded = ref(false)
const busy = ref(false)
const loadError = ref('')

// Die Port-Felder sind Textfelder; erst beim Speichern werden Zahlen daraus.
const httpText = ref('8080')
const httpsText = ref('8443')
const num = (v: unknown) => Number(v)
const current = (): ProxyConfig => ({ ...form.value, httpPort: num(httpText.value), httpsPort: num(httpsText.value) })
const snapshot = (c: ProxyConfig) => JSON.stringify(c)
const dirty = computed(() => snapshot(current()) !== saved.value)

function adopt(c: ProxyConfig) {
  form.value = { ...c, sites: c.sites.map((s) => ({ ...s })) }
  httpText.value = String(c.httpPort)
  httpsText.value = String(c.httpsPort)
  saved.value = snapshot(current())
}

onMounted(async () => {
  await store.refresh()
  try {
    adopt(await dockerApi.proxy())
    loaded.value = true
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
})

const dockerDown = computed(() => store.status && (!store.status.installed || !store.status.active))
const caddy = computed(() => store.containers.find((c) => c.name === CADDY_CONTAINER))
const caddyTag = computed(() => {
  if (!caddy.value) return { text: 'Nicht eingerichtet', severity: 'secondary' as const }
  return caddy.value.state === 'running' ? { text: 'Läuft', severity: 'success' as const } : { text: 'Gestoppt', severity: 'warn' as const }
})

// --- Einträge ---
const editing = ref<number | null>(null) // Index; -1 = neu
const removing = ref<number | null>(null)
const editSite = computed(() => (editing.value !== null && editing.value >= 0 ? form.value.sites[editing.value] : null))
const others = computed(() => form.value.sites.filter((_, i) => i !== editing.value))

function saveSite(s: ProxySite) {
  if (editing.value === null) return
  if (editing.value < 0) form.value.sites.push(s)
  else form.value.sites[editing.value] = s
  form.value.sites.sort((a, b) => a.domain.localeCompare(b.domain))
  editing.value = null
}
function removeSite() {
  if (removing.value !== null) form.value.sites.splice(removing.value, 1)
  removing.value = null
}
const target = (s: ProxySite) => `${s.host || 'dieses NAS'}:${s.port}${s.https ? ' (HTTPS)' : ''}`

// --- Einstellungen ---
const portError = computed(() => {
  const h = httpText.value
  const s = httpsText.value
  const ok = (p: unknown) => Number.isInteger(num(p)) && num(p) >= 1 && num(p) <= 65535
  if (!ok(h) || !ok(s)) return 'Die Ports müssen zwischen 1 und 65535 liegen.'
  if (num(h) === num(s)) return 'HTTP- und HTTPS-Port müssen verschieden sein.'
  if (num(h) === 443 || num(s) === 443) return 'Port 443 belegt die BetterNAS-Oberfläche. Nimm z. B. 8443 und leite am Router 443 darauf weiter.'
  return ''
})

async function apply() {
  if (portError.value || busy.value) return
  busy.value = true
  try {
    adopt(await dockerApi.setProxy(current()))
    const { jobId } = await dockerApi.applyProxy()
    await jobs.track(jobId)
    await store.refresh()
    toast.add({ severity: 'success', summary: form.value.enabled ? 'Reverse-Proxy angewendet' : 'Reverse-Proxy ausgeschaltet', life: 5000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 12000 })
    await store.refresh()
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <DockerNotConfigured v-if="!store.configured" />
  <template v-else>
  <Message v-if="loadError" severity="error" :closable="false">{{ loadError }}</Message>
  <Message v-if="dockerDown" severity="warn" :closable="false" data-test="docker-down">
    {{ store.status?.installed ? 'Der Docker-Dienst läuft nicht.' : 'Docker ist nicht installiert.' }}
  </Message>

  <div class="card info" data-test="proxy-howto">
    <h3>So erreichst du deine Apps von außen</h3>
    <p>Der Reverse-Proxy (Caddy) nimmt Anfragen für deine Domains an, holt dafür automatisch kostenlose HTTPS-Zertifikate bei Let's Encrypt und leitet sie an die jeweilige App weiter.</p>
    <ol>
      <li>Besorge dir eine Domain und lass sie (per DNS-Eintrag oder DynDNS) auf die öffentliche IP-Adresse deines Anschlusses zeigen.</li>
      <li>
        Leite am Router <strong>Port 80</strong> auf Port <strong data-test="howto-http">{{ httpText }}</strong> und <strong>Port 443</strong> auf Port
        <strong data-test="howto-https">{{ httpsText }}</strong> dieses NAS weiter (Port 443 selbst belegt die BetterNAS-Oberfläche).
      </li>
      <li>Lege unten Einträge an und wende sie an. Zertifikate holt Caddy beim ersten Aufruf einer Domain.</li>
    </ol>
  </div>

  <div class="card" data-test="proxy-settings">
    <div class="head">
      <h3>Einstellungen</h3>
      <Tag :value="caddyTag.text" :severity="caddyTag.severity" data-test="proxy-status" />
      <router-link v-if="caddy" to="/docker/container" class="link">Protokoll ansehen</router-link>
    </div>
    <div class="grid">
      <label for="px-on">Reverse-Proxy einschalten</label>
      <ToggleSwitch v-model="form.enabled" input-id="px-on" data-test="proxy-enabled" />
      <label for="px-mail">E-Mail für Let's Encrypt</label>
      <InputText id="px-mail" v-model="form.email" placeholder="du@example.de (optional)" autocomplete="off" data-test="proxy-email" />
      <label for="px-http">HTTP-Port am NAS (Router: 80 →)</label>
      <InputText id="px-http" v-model="httpText" inputmode="numeric" data-test="proxy-http" />
      <label for="px-https">HTTPS-Port am NAS (Router: 443 →)</label>
      <InputText id="px-https" v-model="httpsText" inputmode="numeric" data-test="proxy-https" />
    </div>
    <small v-if="portError" class="err" data-test="proxy-port-error">{{ portError }}</small>
  </div>

  <div class="card" data-test="proxy-sites">
    <div class="head">
      <h3>Einträge</h3>
      <Button data-test="proxy-add" label="Eintrag hinzufügen" icon="pi pi-plus" size="small" :disabled="!loaded" @click="editing = -1" />
    </div>
    <p v-if="!form.sites.length" class="empty">Noch keine Einträge. Ein Eintrag verbindet eine Domain mit einer App, z. B. jellyfin.meinedomain.de mit deinem Jellyfin-Container.</p>
    <table v-else class="table">
      <thead>
        <tr>
          <th>Domain</th>
          <th>Ziel</th>
          <th>Zugriff</th>
          <th />
        </tr>
      </thead>
      <tbody>
        <tr v-for="(s, i) in form.sites" :key="s.domain" :data-test="`site-${s.domain}`">
          <td>
            <strong>{{ s.domain }}</strong>
          </td>
          <td>{{ target(s) }}</td>
          <td><Tag :value="s.public ? 'Auch Internet' : 'Nur Heimnetz'" :severity="s.public ? 'warn' : 'secondary'" /></td>
          <td class="actions">
            <Button icon="pi pi-pencil" text rounded aria-label="Bearbeiten" title="Bearbeiten" @click="editing = i" />
            <Button icon="pi pi-trash" text rounded severity="danger" aria-label="Entfernen" title="Entfernen" @click="removing = i" />
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <div class="bar">
    <small v-if="dirty" class="hint" data-test="proxy-dirty">Ungespeicherte Änderungen. Sie gelten erst nach „Speichern und anwenden“.</small>
    <Button
      data-test="proxy-apply"
      label="Speichern und anwenden"
      icon="pi pi-check"
      :loading="busy"
      :disabled="!loaded || !!dockerDown || !!portError || (!dirty && !!caddy && caddy.state === 'running')"
      @click="apply"
    />
  </div>

  <ProxySiteDialog
    :visible="editing !== null"
    :site="editSite"
    :others="others"
    :containers="store.containers"
    @update:visible="(v) => !v && (editing = null)"
    @save="saveSite"
  />
  <ConfirmDialog
    :visible="removing !== null"
    header="Eintrag entfernen"
    :message="`Der Eintrag „${removing !== null ? form.sites[removing]?.domain : ''}“ wird entfernt. Die App selbst bleibt unverändert; sie ist über diese Domain erst nach „Speichern und anwenden“ nicht mehr erreichbar.`"
    confirm-label="Entfernen"
    @update:visible="(v) => !v && (removing = null)"
    @confirm="removeSite"
  />
  </template>
</template>

<style scoped>
.info p,
.info ol {
  color: var(--bn-text-soft);
  margin: 0.3rem 0;
}
.info ol {
  padding-left: 1.2rem;
}
.head {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 0.5rem;
}
.head h3 {
  flex: 1;
  margin: 0;
}
.link {
  font-size: 0.85rem;
}
.grid {
  display: grid;
  grid-template-columns: minmax(180px, 280px) minmax(0, 1fr);
  gap: 0.6rem 1rem;
  align-items: center;
}
.table {
  width: 100%;
  border-collapse: collapse;
}
.table th {
  text-align: left;
  font-weight: 600;
  padding: 0.4rem 0.5rem;
  color: var(--bn-text-soft);
}
.table td {
  padding: 0.5rem;
  border-top: 1px solid var(--p-content-border-color);
}
.actions {
  text-align: right;
  white-space: nowrap;
}
.empty,
.hint {
  color: var(--bn-text-soft);
}
.bar {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 1rem;
  margin: 0.5rem 0 1.5rem;
}
.err {
  display: block;
  margin-top: 0.5rem;
  color: var(--bn-danger);
}
</style>
