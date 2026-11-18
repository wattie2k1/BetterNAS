<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import Select from 'primevue/select'
import { useToast } from 'primevue/usetoast'
import { servicesApi, type SmbGlobal } from '@/api/system'

const toast = useToast()
const loaded = ref<SmbGlobal | null>(null)
const form = ref<SmbGlobal>({ workgroup: '', serverName: '', minProtocol: 'SMB2_10' })
const error = ref('')
const busy = ref(false)

const protocols = [
  { label: 'SMB 2.1 (auch ältere Geräte)', value: 'SMB2_10' },
  { label: 'SMB 3.0', value: 'SMB3_00' },
  { label: 'SMB 3.1.1 (nur aktuelle Clients)', value: 'SMB3_11' },
]
const dirty = computed(() => !!loaded.value && JSON.stringify(loaded.value) !== JSON.stringify({ ...form.value, workgroup: form.value.workgroup.trim().toUpperCase() }))

async function load() {
  try {
    loaded.value = await servicesApi.smb()
    form.value = { ...loaded.value }
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}
onMounted(load)

async function save() {
  busy.value = true
  error.value = ''
  try {
    await servicesApi.setSmb({ ...form.value, workgroup: form.value.workgroup.trim(), serverName: form.value.serverName.trim() })
    toast.add({ severity: 'success', summary: 'SMB-Einstellungen gespeichert', life: 4000 })
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="card" data-test="settings-fileservices">
    <h2>Windows- und Mac-Freigaben (SMB)</h2>
    <p class="soft">Globale Werte für alle Freigaben. Einzelne Freigaben legst du unter „Freigaben“ an, NFS wird dort je Freigabe aktiviert.</p>
    <Message v-if="error" severity="error" :closable="false" data-test="smb-error">{{ error }}</Message>
    <form v-if="loaded" class="grid" @submit.prevent="save">
      <label for="smb-wg">Arbeitsgruppe</label>
      <InputText id="smb-wg" v-model="form.workgroup" autocomplete="off" />
      <label for="smb-name">Beschreibung</label>
      <InputText id="smb-name" v-model="form.serverName" autocomplete="off" />
      <label for="smb-proto">Mindestprotokoll</label>
      <Select id="smb-proto" v-model="form.minProtocol" :options="protocols" option-label="label" option-value="value" />
      <span />
      <Button type="submit" data-test="smb-save" label="Speichern" icon="pi pi-check" :disabled="!dirty" :loading="busy" class="save" />
    </form>
    <Message severity="info" :closable="false">Die Änderung wird sofort geladen. Verbundene Clients müssen die Freigabe unter Umständen neu öffnen, damit die Arbeitsgruppe aktualisiert wird.</Message>
  </div>
</template>

<style scoped>
.card {
  max-width: 760px;
}
.soft {
  margin: 0 0 14px;
  color: var(--bn-text-soft);
  font-size: 0.875rem;
}
.grid {
  display: grid;
  grid-template-columns: minmax(150px, 220px) 1fr;
  gap: 12px 14px;
  align-items: center;
  margin-bottom: 14px;
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
