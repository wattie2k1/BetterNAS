<script setup lang="ts">
import { onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Message from 'primevue/message'
import Textarea from 'primevue/textarea'
import { useToast } from 'primevue/usetoast'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { systemApi, type CertInfo } from '@/api/system'

const toast = useToast()
const info = ref<CertInfo | null>(null)
const cert = ref('')
const key = ref('')
const error = ref('')
const busy = ref(false)
const confirmRegen = ref(false)

const fmt = (iso: string) => new Date(iso).toLocaleDateString('de-DE', { day: '2-digit', month: 'long', year: 'numeric' })

async function load() {
  try {
    info.value = await systemApi.certificate()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}
onMounted(load)

async function run(fn: () => Promise<CertInfo>, ok: string) {
  busy.value = true
  error.value = ''
  try {
    info.value = await fn()
    toast.add({ severity: 'success', summary: ok, detail: 'Beim nächsten Seitenaufruf gilt das neue Zertifikat.', life: 6000 })
    confirmRegen.value = false
    cert.value = key.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
    confirmRegen.value = false
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="stack" data-test="settings-webaccess">
    <section class="card">
      <h2>HTTPS-Zertifikat</h2>
      <Message v-if="error" severity="error" :closable="false" data-test="web-error">{{ error }}</Message>
      <template v-if="info">
        <Message v-if="info.expired" severity="error" :closable="false">Das Zertifikat ist abgelaufen.</Message>
        <dl class="now" data-test="cert-info">
          <dt>Art</dt>
          <dd>{{ info.selfSigned ? 'Selbstsigniert (Browser warnen davor)' : 'Von einer Zertifizierungsstelle ausgestellt' }}</dd>
          <dt>Ausgestellt für</dt>
          <dd>{{ [...info.dnsNames, ...info.ipAddresses].join(', ') || info.subject }}</dd>
          <dt>Gültig bis</dt>
          <dd>{{ fmt(info.notAfter) }}</dd>
          <dt>Fingerabdruck</dt>
          <dd class="mono">{{ info.fingerprint }}</dd>
        </dl>
        <div class="actions">
          <Button data-test="cert-regenerate" label="Selbstsigniertes Zertifikat neu erzeugen" icon="pi pi-refresh" severity="secondary" :disabled="busy" @click="confirmRegen = true" />
        </div>
      </template>
    </section>
    <section class="card">
      <h2>Eigenes Zertifikat verwenden</h2>
      <p class="soft">Füge Zertifikat (inklusive Zwischenzertifikaten) und privaten Schlüssel im PEM-Format ein. Der Schlüssel darf nicht passwortgeschützt sein. Bei Fehlern bleibt das bisherige Zertifikat aktiv.</p>
      <form class="form" @submit.prevent="run(() => systemApi.installCertificate(cert, key), 'Zertifikat installiert')">
        <label for="w-cert">Zertifikat</label>
        <Textarea id="w-cert" v-model="cert" rows="5" placeholder="-----BEGIN CERTIFICATE-----" spellcheck="false" />
        <label for="w-key">Privater Schlüssel</label>
        <Textarea id="w-key" v-model="key" rows="5" placeholder="-----BEGIN PRIVATE KEY-----" spellcheck="false" autocomplete="off" />
        <div class="actions">
          <Button type="submit" data-test="cert-install" label="Installieren" icon="pi pi-upload" :disabled="!cert.trim() || !key.trim()" :loading="busy" />
        </div>
      </form>
    </section>
    <ConfirmDialog
      :visible="confirmRegen"
      header="Zertifikat neu erzeugen"
      message="Es wird ein neues selbstsigniertes Zertifikat für den aktuellen Hostnamen erzeugt und sofort verwendet. Browser fragen danach erneut nach einer Ausnahme."
      confirm-label="Neu erzeugen"
      :busy="busy"
      @update:visible="(v) => !v && (confirmRegen = false)"
      @confirm="run(() => systemApi.regenerateCertificate(), 'Zertifikat erneuert')"
    />
  </div>
</template>

<style scoped>
.stack {
  display: flex;
  flex-direction: column;
  gap: 18px;
  max-width: 760px;
}
.soft {
  margin: 0 0 12px;
  color: var(--bn-text-soft);
  font-size: 0.875rem;
}
.now {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 4px 16px;
  margin: 0 0 14px;
}
.now dt {
  color: var(--bn-text-soft);
}
.now dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.mono {
  font-family: ui-monospace, monospace;
  font-size: 0.78rem;
}
.form {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.actions {
  display: flex;
  justify-content: flex-end;
  margin-top: 8px;
}
</style>
