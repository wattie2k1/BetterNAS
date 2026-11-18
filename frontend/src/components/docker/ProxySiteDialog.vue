<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import ToggleSwitch from 'primevue/toggleswitch'
import type { ContainerInfo, ProxySite } from '@/api/docker'
import { publishedPorts, siteError } from '@/utils/proxy'

const props = defineProps<{ visible: boolean; site: ProxySite | null; others: ProxySite[]; containers: ContainerInfo[] }>()
const emit = defineEmits<{ 'update:visible': [boolean]; save: [site: ProxySite] }>()

const domain = ref('')
const external = ref(false) // false: Container auf diesem NAS, true: anderes Gerät
const host = ref('')
const port = ref('')
const https = ref(false)
const isPublic = ref(false)

const ports = computed(() => publishedPorts(props.containers))

watch(
  () => [props.visible, props.site] as const,
  ([v]) => {
    if (!v) return
    const s = props.site
    domain.value = s?.domain ?? ''
    external.value = !!s?.host
    host.value = s?.host ?? ''
    port.value = s ? String(s.port) : ''
    https.value = s?.https ?? false
    isPublic.value = s?.public ?? false
  },
  { immediate: true },
)

const draft = computed<ProxySite>(() => ({
  domain: domain.value.trim().toLowerCase(),
  host: external.value ? host.value.trim() : '',
  port: Number(port.value),
  https: https.value,
  public: isPublic.value,
}))
const error = computed(() => (domain.value || port.value !== '' ? siteError(draft.value, props.others) : ''))
const valid = computed(() => !siteError(draft.value, props.others))

function pick(e: Event) {
  const v = (e.target as HTMLSelectElement).value
  if (v) port.value = v
}
const save = () => valid.value && emit('save', draft.value)
</script>

<template>
  <Dialog
    :visible="visible"
    modal
    append-to="self"
    :header="site ? 'Eintrag bearbeiten' : 'Neuer Eintrag'"
    :style="{ width: 'min(560px, 96vw)' }"
    @update:visible="emit('update:visible', $event)"
  >
    <div class="field">
      <label for="px-domain">Domain</label>
      <InputText id="px-domain" v-model="domain" placeholder="jellyfin.meinedomain.de" autocomplete="off" spellcheck="false" data-test="px-domain" />
    </div>
    <div class="field">
      <label>Ziel</label>
      <div class="kind">
        <label><input v-model="external" type="radio" :value="false" data-test="px-kind-local" /> Container auf diesem NAS</label>
        <label><input v-model="external" type="radio" :value="true" data-test="px-kind-external" /> Anderes Gerät im Heimnetz</label>
      </div>
    </div>
    <div v-if="external" class="field">
      <label for="px-host">IP-Adresse des Geräts</label>
      <InputText id="px-host" v-model="host" placeholder="192.168.1.50" autocomplete="off" data-test="px-host" />
    </div>
    <div v-else class="field">
      <label for="px-pick">Laufender Container</label>
      <select id="px-pick" data-test="px-pick" :disabled="!ports.length" @change="pick">
        <option value="">{{ ports.length ? 'Container wählen …' : 'Kein Container mit veröffentlichtem Port' }}</option>
        <option v-for="p in ports" :key="`${p.container}:${p.port}`" :value="p.port">{{ p.label }}</option>
      </select>
    </div>
    <div class="field">
      <label for="px-port">Port</label>
      <InputText id="px-port" v-model="port" inputmode="numeric" placeholder="8096" autocomplete="off" data-test="px-port" />
    </div>
    <label class="row"><input v-model="https" type="checkbox" data-test="px-https" /> Das Ziel spricht selbst HTTPS (selbstsigniertes Zertifikat wird akzeptiert)</label>
    <div class="row public">
      <ToggleSwitch v-model="isPublic" input-id="px-public" data-test="px-public" />
      <label for="px-public"><strong>Auch aus dem Internet erreichbar</strong></label>
    </div>
    <small v-if="isPublic" class="warn" data-test="px-public-warning">
      Dann kann jeder im Internet diese Seite aufrufen. Nutze starke Passwörter, halte die App aktuell und gib nur frei, was du wirklich von außen brauchst.
    </small>
    <small v-else class="hint">Ohne diese Option antwortet der Proxy nur Anfragen aus deinem Heimnetz.</small>
    <small v-if="error" class="err" data-test="px-error">{{ error }}</small>
    <template #footer>
      <Button label="Abbrechen" text @click="emit('update:visible', false)" />
      <Button data-test="px-save" label="Übernehmen" icon="pi pi-check" :disabled="!valid" @click="save" />
    </template>
  </Dialog>
</template>

<style scoped>
.field {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  margin-bottom: 0.75rem;
}
.kind {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem 1.5rem;
}
.row {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  margin-bottom: 0.5rem;
}
.public {
  margin-top: 0.5rem;
}
.hint {
  color: var(--bn-text-soft);
}
.warn {
  color: var(--bn-warn);
}
.err {
  display: block;
  margin-top: 0.5rem;
  color: var(--bn-danger);
}
</style>
