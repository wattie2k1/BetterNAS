<script setup lang="ts">
import { onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import GroupDialog from '@/components/users/GroupDialog.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { useUsersStore } from '@/stores/users'
import type { GroupInput, GroupView } from '@/api/users'

const store = useUsersStore()
const toast = useToast()
const showDialog = ref(false)
const editing = ref<GroupView | null>(null)
const deleting = ref<GroupView | null>(null)
const busy = ref(false)

onMounted(() => store.refresh())

async function run(fn: () => Promise<unknown>, done: string) {
  busy.value = true
  try {
    await fn()
    toast.add({ severity: 'success', summary: done, life: 4000 })
    showDialog.value = false
    deleting.value = null
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Fehlgeschlagen', detail: e instanceof Error ? e.message : String(e), life: 10000 })
  } finally {
    busy.value = false
  }
}

function openNew() {
  editing.value = null
  showDialog.value = true
}
function openEdit(g: GroupView) {
  editing.value = g
  showDialog.value = true
}
function save(input: GroupInput) {
  if (editing.value) {
    const name = editing.value.name
    run(() => store.setMembers(name, input.members), 'Gruppe gespeichert')
  } else run(() => store.createGroup(input), 'Gruppe angelegt')
}
</script>

<template>
  <div class="toolbar">
    <Button label="Neue Gruppe" icon="pi pi-plus" @click="openNew" />
  </div>
  <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>
  <p v-if="!store.groups.length && !store.loading" class="empty">
    Noch keine Gruppen. Mit Gruppen lassen sich Freigaben für mehrere Benutzer auf einmal berechtigen.
  </p>
  <div class="table-wrap" v-else>
    <table class="groups">
      <thead>
        <tr>
          <th>Gruppe</th>
          <th>Mitglieder</th>
          <th />
        </tr>
      </thead>
      <tbody>
        <tr v-for="g in store.groups" :key="g.name" :data-test="`group-${g.name}`">
          <td><strong>{{ g.name }}</strong></td>
          <td>{{ g.members.length ? g.members.join(', ') : '—' }}</td>
          <td class="actions">
            <Button label="Bearbeiten" icon="pi pi-pencil" size="small" text @click="openEdit(g)" />
            <Button label="Löschen" icon="pi pi-trash" size="small" text severity="danger" @click="deleting = g" />
          </td>
        </tr>
      </tbody>
    </table>
  </div>
  <GroupDialog v-model:visible="showDialog" :users="store.users" :groups="store.groups" :group="editing" :busy="busy" @submit="save" />
  <ConfirmDialog
    :visible="!!deleting"
    header="Gruppe löschen"
    :message="`Die Gruppe „${deleting?.name}“ wird entfernt. Die Benutzer bleiben bestehen.`"
    :busy="busy"
    @update:visible="(v) => !v && (deleting = null)"
    @confirm="run(() => store.deleteGroup(deleting!.name), 'Gruppe gelöscht')"
  />
</template>

<style scoped>
.toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 1rem;
}
.empty {
  color: var(--p-text-muted-color);
}
.table-wrap {
  overflow-x: auto;
}
.groups {
  width: 100%;
  border-collapse: collapse;
  background: var(--bn-glass);
  border: 1px solid var(--bn-border);
  border-radius: var(--bn-radius);
  box-shadow: var(--bn-shadow);
  backdrop-filter: var(--bn-blur);
}
th,
td {
  text-align: left;
  padding: 0.7rem 0.9rem;
  border-bottom: 1px solid var(--bn-border-soft);
}
th {
  font-size: 0.8rem;
  color: var(--p-text-muted-color);
  font-weight: 600;
}
.actions {
  display: flex;
  gap: 0.2rem;
  justify-content: flex-end;
}
</style>
