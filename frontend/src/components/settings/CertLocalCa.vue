<script setup lang="ts">
import { ref, watch } from 'vue'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { CA_DOWNLOAD_URL, splitList, tlsApi, type TlsStatus } from '@/api/tls'

const props = defineProps<{ status: TlsStatus | null }>()
const emit = defineEmits<{ changed: [] }>()
const toast = useToast()

const names = ref('')
const busy = ref(false)
const error = ref('')
const confirmReset = ref(false)

watch(
  () => props.status?.localCA.names,
  (n) => {
    if (n) names.value = n.join(', ')
  },
  { immediate: true },
)

const fmt = (iso?: string) => (iso ? new Date(iso).toLocaleDateString('de-DE', { day: '2-digit', month: 'long', year: 'numeric' }) : '')

async function run(reset: boolean) {
  busy.value = true
  error.value = ''
  try {
    await tlsApi.enableLocalCa(splitList(names.value), reset)
    toast.add({
      severity: 'success',
      summary: reset ? 'Neue Zertifizierungsstelle angelegt' : 'Zertifikat der lokalen Zertifizierungsstelle aktiv',
      detail: 'Installiere das Root-Zertifikat auf deinen Geräten, damit sie der Seite vertrauen.',
      life: 8000,
    })
    confirmReset.value = false
    emit('changed')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
    confirmReset.value = false
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="card" data-test="cert-localca">
    <h2>Lokale Zertifizierungsstelle <span class="tag">empfohlen fürs Heimnetz</span></h2>
    <p class="soft">
      BetterNAS legt eine eigene Zertifizierungsstelle (CA) an und stellt damit das Zertifikat für die Weboberfläche aus – auch für die IP-Adresse des NAS und für <code>.local</code>-Namen. Es erneuert sich selbst und passt sich an,
      wenn sich die Adresse ändert. Du installierst das Root-Zertifikat einmal pro Gerät, danach gibt es keine Browserwarnung mehr. Es ist kein Konto und keine Domain nötig.
    </p>
    <Message v-if="error" severity="error" :closable="false" data-test="localca-error">{{ error }}</Message>
    <Message v-if="status?.localCA.exists" severity="secondary" :closable="false" data-test="localca-info">
      CA bis {{ fmt(status.localCA.notAfter) }} gültig · Fingerabdruck <span class="mono">{{ status.localCA.fingerprint }}</span>
    </Message>

    <div class="form">
      <label for="ca-names">Weitere Namen oder Adressen im Zertifikat (optional)</label>
      <InputText id="ca-names" v-model="names" placeholder="z. B. nas.fritz.box, 10.0.0.5" spellcheck="false" />
      <small class="soft">Hostname, <code>.local</code>-Name und die aktuellen LAN-Adressen sind immer enthalten.</small>
    </div>
    <div class="actions">
      <Button
        v-if="status?.localCA.exists"
        data-test="localca-reset"
        label="CA neu erzeugen"
        icon="pi pi-refresh"
        severity="secondary"
        text
        :disabled="busy"
        @click="confirmReset = true"
      />
      <Button
        data-test="localca-enable"
        :label="status?.mode === 'localca' ? 'Zertifikat neu ausstellen' : 'Lokale CA einrichten und verwenden'"
        icon="pi pi-shield"
        :loading="busy"
        @click="run(false)"
      />
    </div>

    <template v-if="status?.localCA.exists">
      <h3>Root-Zertifikat auf Geräten installieren</h3>
      <p>
        <a class="dl" data-test="localca-download" :href="CA_DOWNLOAD_URL" download="betternas-ca.crt">
          <i class="pi pi-download" /> Root-Zertifikat herunterladen (betternas-ca.crt)
        </a>
      </p>
      <details>
        <summary>Windows</summary>
        <p>Datei doppelklicken → „Zertifikat installieren“ → „Lokaler Computer“ → „Alle Zertifikate in folgendem Speicher speichern“ → <em>Vertrauenswürdige Stammzertifizierungsstellen</em>. Browser neu starten.</p>
      </details>
      <details>
        <summary>macOS</summary>
        <p>Datei doppelklicken (Schlüsselbundverwaltung öffnet sich) → Zertifikat im Schlüsselbund „System“ ablegen → doppelklicken → „Vertrauen“ → „Bei Verwendung dieses Zertifikats: Immer vertrauen“.</p>
      </details>
      <details>
        <summary>iPhone / iPad</summary>
        <p>
          Datei in Safari laden und das Profil zulassen → Einstellungen → „Profil geladen“ → installieren. Danach: Einstellungen → Allgemein → Info → Zertifikatsvertrauenseinstellungen → BetterNAS-CA einschalten.
        </p>
      </details>
      <details>
        <summary>Android</summary>
        <p>Einstellungen → Sicherheit → Verschlüsselung und Anmeldedaten → „Zertifikat installieren“ → „CA-Zertifikat“ → Datei wählen. (Die Menüs heißen je nach Hersteller etwas anders.)</p>
      </details>
      <details>
        <summary>Linux / Firefox</summary>
        <p>
          Firefox hat einen eigenen Speicher: Einstellungen → Datenschutz &amp; Sicherheit → Zertifikate anzeigen → Zertifizierungsstellen → Importieren. Für das System unter Debian/Ubuntu: Datei nach
          <code>/usr/local/share/ca-certificates/betternas-ca.crt</code> kopieren und <code>sudo update-ca-certificates</code> ausführen.
        </p>
      </details>
      <Message severity="warn" :closable="false">
        Auf Geräten, die diese CA installiert haben, könnte sie Zertifikate für beliebige Webseiten ausstellen. Der CA-Schlüssel liegt ausschließlich auf diesem NAS – halte es sicher und installiere die CA nur auf eigenen Geräten.
      </Message>
    </template>

    <ConfirmDialog
      :visible="confirmReset"
      header="CA neu erzeugen"
      message="Es wird eine völlig neue Zertifizierungsstelle angelegt. Die bisherige wird ungültig: Auf allen Geräten musst du das neue Root-Zertifikat erneut installieren und das alte entfernen."
      confirm-label="Neu erzeugen"
      :busy="busy"
      @update:visible="(v) => !v && (confirmReset = false)"
      @confirm="run(true)"
    />
  </section>
</template>

<style scoped>
.soft {
  margin: 0 0 12px;
  color: var(--bn-text-soft);
  font-size: 0.875rem;
}
.tag {
  margin-left: 8px;
  padding: 2px 8px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--bn-ok) 18%, transparent);
  color: var(--bn-ok);
  font-size: 0.72rem;
  font-weight: 600;
  vertical-align: middle;
}
.mono {
  font-family: ui-monospace, monospace;
  font-size: 0.75rem;
  overflow-wrap: anywhere;
}
.form {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 12px;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 10px;
}
h3 {
  margin: 18px 0 6px;
  font-size: 1rem;
}
details {
  margin: 4px 0;
}
summary {
  cursor: pointer;
  font-weight: 600;
}
details p {
  margin: 6px 0 6px 16px;
  font-size: 0.875rem;
  color: var(--bn-text-soft);
}
.dl {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
}
</style>
