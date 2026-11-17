<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Button from 'primevue/button'
import SelectButton from 'primevue/selectbutton'
import UsersView from './UsersView.vue'
import GroupsView from './GroupsView.vue'

type Tab = 'benutzer' | 'gruppen'

const route = useRoute()
const router = useRouter()
const options = [
  { label: 'Benutzer', value: 'benutzer' },
  { label: 'Gruppen', value: 'gruppen' },
]
const tab = computed<Tab>({
  get: () => (route.query.tab === 'gruppen' ? 'gruppen' : 'benutzer'),
  set: (v) => {
    if (v) router.replace({ query: v === 'benutzer' ? {} : { tab: v } })
  },
})
const current = ref<{ openNew: () => void } | null>(null)
</script>

<template>
  <div class="toolbar">
    <SelectButton v-model="tab" :options="options" option-label="label" option-value="value" :allow-empty="false" aria-label="Ansicht" data-test="users-tabs" />
    <span class="spacer" />
    <Button v-if="tab === 'benutzer'" label="Neuer Benutzer" icon="pi pi-user-plus" data-test="new-user" @click="current?.openNew()" />
    <Button v-else label="Neue Gruppe" icon="pi pi-plus" data-test="new-group" @click="current?.openNew()" />
  </div>
  <UsersView v-if="tab === 'benutzer'" ref="current" />
  <GroupsView v-else ref="current" />
</template>

<style scoped>
.spacer {
  flex: 1;
}
</style>
