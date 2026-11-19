<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Button from 'primevue/button'
import Checkbox from 'primevue/checkbox'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import Password from 'primevue/password'
import Select from 'primevue/select'
import { useToast } from 'primevue/usetoast'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { useJobsStore } from '@/stores/jobs'
import { splitList, tlsApi, type AcmeInput, type TlsStatus } from '@/api/tls'

const props = defineProps<{ status: TlsStatus | null }>()
const emit = defineEmits<{ changed: [] }>()
const toast = useToast()
const jobs = useJobsStore()

const provider = ref('')
const domains = ref('')
const email = ref('')
const staging = ref(false)
const agreed = ref(false)
const creds = ref<Record<string, string>>({})
const busy = ref(false)
const error = ref('')
const confirmRemove = ref(false)

const current = computed(() => props.status?.providers.find((p) => p.id === provider.value))
const acme = computed(() => props.status?.acme)

watch(
  () => props.status?.acme,
  (a) => {
    if (!a || !a.configured) return
    provider.value = a.provider
    domains.value = a.domains.join(', ')
    email.value = a.email
    staging.value = a.staging
    agreed.value = a.agreedTerms
  },
  { immediate: true },
)
// Beim Anbieterwechsel gehören die eingegebenen Geheimnisse nicht mehr dazu.
watch(provider, () => (creds.value = {}))

const fmt = (iso?: string) => (iso ? new Date(iso).toLocaleString('de-DE', { dateStyle: 'medium', timeStyle: 'short' }) : '')

function input(): AcmeInput {
  return { provider: provider.value, domains: splitList(domains.value), email: email.value.trim(), staging: staging.value, agreedTerms: agreed.value, credentials: { ...creds.value } }
}

const missing = computed(() => {
  if (!current.value) return 'Bitte einen DNS-Anbieter wählen.'
  if (splitList(domains.value).length === 0) return 'Bitte mindestens eine Domain eintragen.'
  if (!agreed.value) return 'Bitte den Nutzungsbedingungen von Let’s Encrypt zustimmen.'
  for (const f of current.value.fields) {
    if (!f.optional && !creds.value[f.key]?.trim() && !(acme.value?.provider === provider.value && acme.value.credentialsSet[f.key])) return `Bitte „${f.label}“ ausfüllen.`
  }
  return ''
})

async function save(issue: boolean) {
  busy.value = true
  error.value = ''
  try {
    await tlsApi.saveAcme(input())
    creds.value = {}
    emit('changed')
    if (issue) {
      const { jobId } = await tlsApi.issueAcme()
      await jobs.track(jobId)
      toast.add({ severity: 'success', summary: 'Zertifikat von Let’s Encrypt aktiv', detail: 'Rufe die Seite jetzt über den Domainnamen auf.', life: 8000 })
    } else {
      toast.add({ severity: 'success', summary: 'Einstellungen gespeichert', life: 3000 })
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
    emit('changed')
  }
}

async function remove() {
  busy.value = true
  try {
    await tlsApi.removeAcme()
    provider.value = ''
    domains.value = email.value = ''
    agreed.value = staging.value = false
    creds.value = {}
    confirmRemove.value = false
    emit('changed')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
    confirmRemove.value = false
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="card" data-test="cert-acme">
    <h2>Let’s Encrypt</h2>
    <p class="soft">
      Ein von allen Browsern und Geräten anerkanntes Zertifikat, ohne etwas zu installieren. Dafür brauchst du einen Domainnamen, dessen DNS-Anbieter eine Schnittstelle (API) hat. Das NAS muss <strong>nicht</strong> aus dem
      Internet erreichbar sein: Let’s Encrypt prüft über einen Eintrag im DNS, nicht über eine Verbindung zu deinem Gerät. Hast du keine eigene Domain, gibt es kostenlose Subdomains bei DuckDNS (duckdns.org) oder deSEC (desec.io).
    </p>
    <Message severity="info" :closable="false">
      Die Domain muss im Heimnetz auf die LAN-Adresse dieses NAS zeigen (beim Anbieter als A-Eintrag oder im Router/Pi-hole). Außerdem erscheint der Domainname in öffentlichen Zertifikatsprotokollen, die LAN-Adresse nicht.
    </Message>
    <Message v-if="error" severity="error" :closable="false" data-test="acme-error">{{ error }}</Message>
    <Message v-if="acme?.lastError" severity="warn" :closable="false" data-test="acme-last-error">Letzter Versuch fehlgeschlagen: {{ acme.lastError }}</Message>
    <p v-if="acme?.lastIssued" class="soft" data-test="acme-last-issued">Zuletzt ausgestellt: {{ fmt(acme.lastIssued) }}. Das Zertifikat wird automatisch rund 30 Tage vor Ablauf erneuert.</p>

    <form class="form" @submit.prevent="save(true)">
      <label for="acme-provider">DNS-Anbieter</label>
      <Select id="acme-provider" v-model="provider" :options="status?.providers ?? []" option-label="label" option-value="id" placeholder="Anbieter wählen" data-test="acme-provider" />
      <small v-if="current" class="soft" data-test="acme-help">{{ current.help }}</small>

      <label for="acme-domains">Domain(s)</label>
      <InputText id="acme-domains" v-model="domains" placeholder="meinnas.duckdns.org" spellcheck="false" autocomplete="off" />

      <template v-if="current">
        <template v-for="f in current.fields" :key="current.id + f.key">
          <label :for="'acme-f-' + f.key">{{ f.label }}<span v-if="f.optional" class="soft"> (optional)</span></label>
          <Password
            v-if="f.secret"
            v-model="creds[f.key]"
            :input-id="'acme-f-' + f.key"
            :feedback="false"
            toggle-mask
            fluid
            autocomplete="off"
            :placeholder="acme?.provider === current.id && acme.credentialsSet[f.key] ? 'gespeichert – leer lassen zum Beibehalten' : ''"
          />
          <InputText v-else :id="'acme-f-' + f.key" v-model="creds[f.key]" autocomplete="off" spellcheck="false" :placeholder="acme?.provider === current.id && acme.credentialsSet[f.key] ? 'gespeichert' : ''" />
        </template>
      </template>

      <label for="acme-email">E-Mail (optional, für Hinweise von Let’s Encrypt)</label>
      <InputText id="acme-email" v-model="email" type="email" autocomplete="off" />

      <div class="check">
        <Checkbox v-model="agreed" input-id="acme-terms" binary data-test="acme-terms" />
        <label for="acme-terms">
          Ich stimme den <a href="https://letsencrypt.org/repository/" target="_blank" rel="noopener noreferrer">Nutzungsbedingungen von Let’s Encrypt</a> zu.
        </label>
      </div>
      <details>
        <summary>Erweitert</summary>
        <div class="check">
          <Checkbox v-model="staging" input-id="acme-staging" binary />
          <label for="acme-staging">Testumgebung von Let’s Encrypt verwenden (Browser vertrauen diesen Zertifikaten nicht – nur zum Ausprobieren)</label>
        </div>
      </details>

      <Message v-if="missing" severity="secondary" :closable="false" data-test="acme-missing">{{ missing }}</Message>
      <div class="actions">
        <Button v-if="acme?.configured" type="button" label="Einstellungen löschen" icon="pi pi-trash" severity="danger" text :disabled="busy" data-test="acme-remove" @click="confirmRemove = true" />
        <Button type="button" label="Nur speichern" severity="secondary" :disabled="!!missing || busy" data-test="acme-save" @click="save(false)" />
        <Button type="submit" label="Zertifikat anfordern" icon="pi pi-lock" :disabled="!!missing" :loading="busy || acme?.busy" data-test="acme-issue" />
      </div>
      <small v-if="busy" class="soft">Das Anfordern kann einige Minuten dauern (das DNS muss sich aktualisieren). Den Fortschritt siehst du unter „Aufträge“.</small>
    </form>

    <ConfirmDialog
      :visible="confirmRemove"
      header="Einstellungen löschen"
      message="Zugangsdaten und Domain werden gelöscht. Das aktuelle Zertifikat bleibt aktiv, wird aber nicht mehr automatisch erneuert."
      :busy="busy"
      @update:visible="(v) => !v && (confirmRemove = false)"
      @confirm="remove"
    />
  </section>
</template>

<style scoped>
.soft {
  margin: 0 0 12px;
  color: var(--bn-text-soft);
  font-size: 0.875rem;
}
.form {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 10px;
}
.form .soft {
  margin: 0;
}
.check {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-top: 8px;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 10px;
}
details {
  margin-top: 6px;
}
summary {
  cursor: pointer;
  font-weight: 600;
}
</style>
