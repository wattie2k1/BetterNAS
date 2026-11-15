<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import Button from 'primevue/button'
import Stepper from 'primevue/stepper'
import StepList from 'primevue/steplist'
import Step from 'primevue/step'
import StepPanels from 'primevue/steppanels'
import StepPanel from 'primevue/steppanel'
import { useToast } from 'primevue/usetoast'
import DisksView from '@/views/storage/DisksView.vue'
import PoolsView from '@/views/storage/PoolsView.vue'
import ShareDialog from '@/components/shares/ShareDialog.vue'
import { useSetupStore } from '@/stores/setup'
import { useStorageStore } from '@/stores/storage'
import { useSharesStore } from '@/stores/shares'
import { useUsersStore } from '@/stores/users'
import { useAuthStore } from '@/stores/auth'
import type { ShareInput } from '@/api/shares'

const router = useRouter()
const toast = useToast()
const setup = useSetupStore()
const storage = useStorageStore()
const sharesStore = useSharesStore()
const usersStore = useUsersStore()
const auth = useAuthStore()
const step = ref('1')
const showShare = ref(false)
const busy = ref(false)

onMounted(() => Promise.all([storage.refresh(), sharesStore.refresh(), usersStore.refresh()]))

const hasData = computed(() => storage.disks.some((d) => d.role === 'data'))
const hasPool = computed(() => storage.pools.length > 0)
const first = computed(() => sharesStore.shares[0])
const smbUrl = computed(() => (first.value ? `smb://${setup.hostname}.local/${first.value.name}` : ''))

async function createShare(input: ShareInput) {
  busy.value = true
  try {
    await sharesStore.create(input)
    showShare.value = false
    toast.add({ severity: 'success', summary: 'Freigabe angelegt', life: 4000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = false
  }
}

async function finish() {
  try {
    await setup.complete()
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Konnte nicht gespeichert werden', detail: e instanceof Error ? e.message : String(e), life: 8000 })
    return
  }
  await router.replace('/')
}
</script>

<template>
  <div class="head">
    <p class="intro">In wenigen Schritten richtest du deinen Speicher ein. Du kannst das auch später unter „Datenträger“, „Speicherpools“ und „Freigaben“ erledigen.</p>
    <Button label="Später einrichten" text data-test="setup-skip" @click="finish" />
  </div>
  <Stepper v-model:value="step" linear>
    <StepList>
      <Step value="1">Datenplatten</Step>
      <Step value="2">Speicherpool</Step>
      <Step value="3">Freigabe</Step>
      <Step value="4">Fertig</Step>
    </StepList>
    <StepPanels>
      <StepPanel v-slot="{ activateCallback }" value="1">
        <p class="hint">Formatiere die Platten, die BetterNAS nutzen soll, oder übernimm Platten mit vorhandenen Daten.</p>
        <DisksView />
        <div class="nav">
          <Button label="Weiter" data-test="step-1-next" :disabled="!hasData" @click="activateCallback('2')" />
        </div>
      </StepPanel>
      <StepPanel v-slot="{ activateCallback }" value="2">
        <p class="hint">Ein Pool fasst mehrere Platten zu einem gemeinsamen Speicherort zusammen.</p>
        <PoolsView />
        <div class="nav">
          <Button label="Zurück" text @click="activateCallback('1')" />
          <Button label="Weiter" data-test="step-2-next" :disabled="!hasPool" @click="activateCallback('3')" />
        </div>
      </StepPanel>
      <StepPanel v-slot="{ activateCallback }" value="3">
        <p class="hint">Eine Freigabe macht einen Ordner im Netzwerk verfügbar. Du bekommst darauf Schreibrecht.</p>
        <Button label="Freigabe anlegen" icon="pi pi-plus" data-test="setup-new-share" @click="showShare = true" />
        <ul>
          <li v-for="s in sharesStore.shares" :key="s.name">{{ s.name }} <span class="hint">({{ s.path }})</span></li>
        </ul>
        <div class="nav">
          <Button label="Zurück" text @click="activateCallback('2')" />
          <Button label="Weiter" data-test="step-3-next" :disabled="!first" @click="activateCallback('4')" />
        </div>
      </StepPanel>
      <StepPanel v-slot="{ activateCallback }" value="4">
        <h2>Fertig!</h2>
        <template v-if="first">
          <p>Deine Freigabe „{{ first.name }}“ erreichst du so:</p>
          <p>Windows: <code>{{ first.uncPath }}</code></p>
          <p>Mac: <code>{{ smbUrl }}</code></p>
          <p class="hint">Melde dich mit deinem Benutzernamen und Passwort an.</p>
        </template>
        <div class="nav">
          <Button label="Zurück" text @click="activateCallback('3')" />
          <Button label="Zur Übersicht" data-test="setup-finish" @click="finish" />
        </div>
      </StepPanel>
    </StepPanels>
  </Stepper>
  <ShareDialog
    v-model:visible="showShare"
    :pools="storage.pools"
    :users="usersStore.users"
    :groups="usersStore.groups"
    :shares="sharesStore.shares"
    :default-writer="auth.user ?? undefined"
    :busy="busy"
    @submit="createShare"
  />
</template>

<style scoped>
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
  flex-wrap: wrap;
  margin-bottom: 1rem;
}
.intro,
.hint {
  color: var(--p-text-muted-color);
}
.nav {
  display: flex;
  justify-content: space-between;
  margin-top: 1.5rem;
}
code {
  overflow-wrap: anywhere;
}
</style>
