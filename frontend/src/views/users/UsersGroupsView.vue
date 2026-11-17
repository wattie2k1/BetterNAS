<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Button from 'primevue/button'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import UsersView from './UsersView.vue'
import GroupsView from './GroupsView.vue'
import { useUsersStore } from '@/stores/users'

type Tab = 'benutzer' | 'gruppen'

const route = useRoute()
const router = useRouter()
const store = useUsersStore()
const tab = computed<Tab>({
  get: () => (route.query.tab === 'gruppen' ? 'gruppen' : 'benutzer'),
  set: (v) => {
    if (v) router.replace({ query: v === 'benutzer' ? {} : { tab: v } })
  },
})
const usersRef = ref<{ openNew: () => void } | null>(null)
const groupsRef = ref<{ openNew: () => void } | null>(null)
</script>

<template>
  <div class="toolbar">
    <Button v-if="tab === 'benutzer'" label="Neuer Benutzer" icon="pi pi-user-plus" data-test="new-user" @click="usersRef?.openNew()" />
    <Button v-else label="Neue Gruppe" icon="pi pi-plus" data-test="new-group" @click="groupsRef?.openNew()" />
  </div>
  <Tabs v-model:value="tab" lazy data-test="users-tabs">
    <TabList>
      <Tab value="benutzer">Benutzer ({{ store.users.length }})</Tab>
      <Tab value="gruppen">Gruppen ({{ store.groups.length }})</Tab>
    </TabList>
    <TabPanels>
      <TabPanel value="benutzer"><UsersView ref="usersRef" /></TabPanel>
      <TabPanel value="gruppen"><GroupsView ref="groupsRef" /></TabPanel>
    </TabPanels>
  </Tabs>
</template>
