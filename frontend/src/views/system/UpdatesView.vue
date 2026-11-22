<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import Button from 'primevue/button'
import Message from 'primevue/message'
import SelectButton from 'primevue/selectbutton'
import Tag from 'primevue/tag'
import ToggleSwitch from 'primevue/toggleswitch'
import { useToast } from 'primevue/usetoast'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { useUpdatesStore } from '@/stores/updates'
import { useRebootStore } from '@/stores/reboot'
import type { UpdateChannel } from '@/api/updates'

const store = useUpdatesStore()
const reboot = useRebootStore()
const toast = useToast()
const confirm = ref(false)

onMounted(() => {
  void store.load()
  void reboot.load()
})

// Nach einem Update von BetterNAS selbst läuft die Oberfläche in der alten Version: neu laden.
watch(
  () => store.reloadNeeded,
  (v) => v && setTimeout(() => window.location.reload(), 1500),
)

async function check() {
  try {
    await store.check()
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Suche fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  }
}

async function install() {
  confirm.value = false
  try {
    await store.install()
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Installation nicht gestartet', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  }
}

const channelOptions = [
  { label: 'Stabil', value: 'stable' },
  { label: 'Testing', value: 'testing' },
]

async function changeChannel(channel: UpdateChannel) {
  if (channel === store.overview.channel) return
  try {
    await store.setChannel(channel)
    toast.add({ severity: 'success', summary: 'Update-Kanal geändert', detail: 'Bitte jetzt erneut nach Updates suchen.', life: 6000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Kanalwechsel fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  }
}

const auto = computed(() => store.overview.auto ?? { enabled: false, lastRun: null })
const lastRun = computed(() => auto.value.lastRun)
const lastRunOpen = ref(false)

async function changeAuto(enabled: boolean) {
  try {
    await store.setAuto(enabled)
    toast.add({
      severity: 'success',
      summary: enabled ? 'Automatische Updates eingeschaltet' : 'Automatische Updates ausgeschaltet',
      life: 4000,
    })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Umschalten fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  }
}

const logEl = ref<HTMLElement | null>(null)
// Während der Installation immer das Ende des Protokolls zeigen.
watch(
  () => store.overview.run.log,
  async () => {
    await nextTick()
    if (logEl.value) logEl.value.scrollTop = logEl.value.scrollHeight
  },
)
async function copyLog() {
  try {
    await navigator.clipboard.writeText(store.overview.run.log)
    toast.add({ severity: 'success', summary: 'Protokoll kopiert', life: 2500 })
  } catch {
    toast.add({ severity: 'warn', summary: 'Kopieren nicht möglich', life: 4000 })
  }
}

const when = (iso: string) => (iso ? new Date(iso).toLocaleString('de-DE') : '')
</script>

<template>
  <div class="toolbar">
    <Button
      label="Nach Updates suchen"
      icon="pi pi-refresh"
      outlined
      data-test="updates-check"
      :loading="store.checking"
      :disabled="store.installing"
      @click="check"
    />
    <Button
      label="Updates installieren"
      icon="pi pi-download"
      data-test="updates-install"
      :disabled="!store.overview.packages.length || store.installing"
      @click="confirm = true"
    />
  </div>
  <div v-if="store.overview.channel" class="channel">
    <span class="channel-label">Update-Kanal</span>
    <SelectButton
      :model-value="store.overview.channel"
      :options="channelOptions"
      option-label="label"
      option-value="value"
      :allow-empty="false"
      :disabled="store.installing || store.changingChannel"
      data-test="updates-channel"
      @update:model-value="changeChannel"
    />
    <p class="sub channel-hint">
      Testing enthält Vorabversionen. Ein Wechsel zurück zu Stabil installiert keine ältere Version; das NAS bleibt auf
      seiner Version, bis Stabil sie einholt.
    </p>
  </div>
  <section class="auto" data-test="updates-auto">
    <div class="auto-head">
      <ToggleSwitch
        input-id="auto-updates"
        :model-value="auto.enabled"
        :disabled="store.installing || store.changingAuto"
        data-test="updates-auto-switch"
        @update:model-value="changeAuto"
      />
      <label for="auto-updates" class="channel-label">Updates automatisch installieren</label>
    </div>
    <p class="sub channel-hint">
      Debian-Updates, auch Sicherheitsupdates, werden täglich im Hintergrund installiert. Neue Kernel und Updates von
      BetterNAS selbst bleiben bewusst ausgenommen und erscheinen weiter in der Liste unten.
    </p>
    <div v-if="lastRun" class="auto-last" data-test="updates-auto-last">
      <i v-if="lastRun.result === 'failed'" class="pi pi-times-circle bad" />
      <i v-else class="pi pi-check-circle okc" />
      <span>
        Zuletzt automatisch aktualisiert: {{ when(lastRun.at) }} –
        {{ lastRun.packages.length }} {{ lastRun.packages.length === 1 ? 'Paket' : 'Pakete' }}
        <template v-if="lastRun.result === 'failed'"> (fehlgeschlagen)</template>
      </span>
      <Button
        :label="lastRunOpen ? 'Pakete ausblenden' : 'Pakete anzeigen'"
        text
        size="small"
        data-test="updates-auto-toggle"
        @click="lastRunOpen = !lastRunOpen"
      />
    </div>
    <p v-else-if="auto.enabled" class="sub channel-hint" data-test="updates-auto-none">Bisher wurde noch nichts automatisch installiert.</p>
    <p v-if="lastRun && lastRunOpen" class="auto-pkgs" data-test="updates-auto-pkgs">{{ lastRun.packages.join(', ') }}</p>
    <Message v-if="reboot.status.required" severity="warn" :closable="false" data-test="updates-auto-reboot">
      Ein Neustart wird empfohlen, damit alle Updates wirksam werden.
    </Message>
  </section>
  <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>

  <Message v-if="store.restarting" severity="info" :closable="false" data-test="updates-restarting">
    BetterNAS startet neu … Die Seite verbindet sich automatisch wieder.
  </Message>
  <Message v-else-if="store.overview.run.state === 'running'" severity="info" :closable="false" data-test="updates-running">
    Updates werden installiert. Bitte das NAS nicht ausschalten.
  </Message>
  <Message v-else-if="store.overview.run.state === 'succeeded'" severity="success" :closable="false" data-test="updates-done">
    Die Updates wurden erfolgreich installiert.
  </Message>
  <Message v-else-if="store.overview.run.state === 'failed'" severity="error" :closable="false" data-test="updates-failed">
    Die Installation ist fehlgeschlagen. Details stehen im Protokoll.
  </Message>

  <p v-if="store.overview.checkedAt" class="sub">Zuletzt geprüft: {{ when(store.overview.checkedAt) }}</p>
  <p v-else-if="!store.loading" class="sub">Noch nicht nach Updates gesucht.</p>

  <p v-if="store.overview.checkedAt && !store.overview.packages.length && store.overview.run.state === 'none'" class="ok">
    <i class="pi pi-check-circle" /> Alles auf dem neuesten Stand.
  </p>
  <div v-if="store.overview.packages.length" class="table-wrap">
    <table class="pkgs">
      <thead>
        <tr><th>Paket</th><th>Installiert</th><th>Neu</th></tr>
      </thead>
      <tbody>
        <tr v-for="p in store.overview.packages" :key="p.name">
          <td>{{ p.name }}</td>
          <td>{{ p.current || '—' }}</td>
          <td>{{ p.candidate }}</td>
        </tr>
      </tbody>
    </table>
  </div>

  <section v-if="store.overview.run.log" class="log-card" data-test="updates-log">
    <header class="log-head">
      <i class="pi pi-code" />
      <span class="log-title">Protokoll</span>
      <Tag v-if="store.overview.run.state === 'running'" value="läuft" severity="info" />
      <Tag v-else-if="store.overview.run.state === 'succeeded'" value="erfolgreich" severity="success" />
      <Tag v-else-if="store.overview.run.state === 'failed'" value="fehlgeschlagen" severity="danger" />
      <span class="spacer" />
      <Button icon="pi pi-copy" text rounded size="small" aria-label="Protokoll kopieren" title="Kopieren" @click="copyLog" />
    </header>
    <pre ref="logEl" class="log">{{ store.overview.run.log }}</pre>
  </section>

  <ConfirmDialog
    v-model:visible="confirm"
    header="Updates installieren"
    message="Die Updates werden jetzt installiert. Dabei kann BetterNAS kurz nicht erreichbar sein. Laufende Übertragungen können abbrechen."
    confirm-label="Installieren"
    @confirm="install"
  />
</template>

<style scoped>
.sub {
  color: var(--bn-text-soft);
}
.channel {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.6rem 0.9rem;
  margin-bottom: 1rem;
}
.channel-label {
  font-weight: 650;
}
.channel-hint {
  flex-basis: 100%;
  margin: 0;
  font-size: 0.85rem;
}
.auto {
  margin-bottom: 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.auto-head,
.auto-last {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.6rem;
}
.auto-pkgs {
  margin: 0;
  font-size: 0.85rem;
  overflow-wrap: anywhere;
  color: var(--bn-text-soft);
}
.okc {
  color: var(--bn-ok);
}
.bad {
  color: var(--p-red-500);
}
.ok {
  display: flex;
  align-items: center;
  gap: 0.7rem;
  padding: 1.4rem 1.6rem;
  border-radius: var(--bn-radius);
  background: linear-gradient(135deg, rgb(16 185 129 / 18%), rgb(34 211 238 / 12%));
  border: 1px solid rgb(16 185 129 / 35%);
  color: var(--bn-ok);
  font-weight: 650;
}
.ok i {
  font-size: 1.5rem;
}
.pkgs {
  width: 100%;
  border-collapse: collapse;
}
.log-card {
  margin-top: 1rem;
  border-radius: var(--bn-radius);
  border: 1px solid var(--bn-border);
  box-shadow: var(--bn-shadow);
  overflow: hidden;
  background: var(--bn-glass);
  backdrop-filter: var(--bn-blur);
}
.log-head {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  padding: 0.55rem 0.9rem;
  border-bottom: 1px solid var(--bn-border-soft);
  color: var(--bn-text-soft);
}
.log-title {
  font-weight: 650;
  color: var(--p-text-color);
}
.spacer {
  flex: 1;
}
.log {
  margin: 0;
  padding: 0.9rem 1.1rem;
  max-height: 24rem;
  overflow: auto;
  font-size: 0.8rem;
  line-height: 1.55;
  font-family: ui-monospace, 'SF Mono', Menlo, monospace;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  color: var(--bn-terminal-fg);
  background: var(--bn-terminal-bg);
}
</style>
