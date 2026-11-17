<script setup lang="ts">
import { onMounted, ref } from 'vue'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import UserDialog from '@/components/users/UserDialog.vue'
import PasswordDialog from '@/components/users/PasswordDialog.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { totpApi } from '@/api/totp'
import { useUsersStore } from '@/stores/users'
import { useAuthStore } from '@/stores/auth'
import type { UserInput, UserView } from '@/api/users'

const store = useUsersStore()
const auth = useAuthStore()
const toast = useToast()
const showDialog = ref(false)
const editing = ref<UserView | null>(null)
const pwUser = ref<UserView | null>(null)
const deleting = ref<UserView | null>(null)
const resetting = ref<UserView | null>(null)
const busy = ref(false)

onMounted(() => store.refresh())

async function run(fn: () => Promise<unknown>, done: string) {
  busy.value = true
  try {
    await fn()
    toast.add({ severity: 'success', summary: done, life: 4000 })
    showDialog.value = false
    pwUser.value = null
    deleting.value = null
    resetting.value = null
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
function openEdit(u: UserView) {
  editing.value = u
  showDialog.value = true
}
function save(input: UserInput) {
  if (editing.value) {
    const name = editing.value.name
    run(() => store.updateUser(name, { fullName: input.fullName, groups: input.groups, admin: input.admin }), 'Benutzer gespeichert')
  } else run(() => store.createUser(input), 'Benutzer angelegt')
}
defineExpose({ openNew })
</script>

<template>
  <Message v-if="store.error" severity="error" :closable="false">{{ store.error }}</Message>
  <div class="table-wrap">
    <table class="users">
      <thead>
        <tr>
          <th>Benutzer</th>
          <th>Rolle</th>
          <th>Gruppen</th>
          <th />
        </tr>
      </thead>
      <tbody>
        <tr v-for="u in store.users" :key="u.name" :data-test="`user-${u.name}`">
          <td>
            <div class="who">
              <span class="avatar-sm" aria-hidden="true">{{ u.name.slice(0, 1).toUpperCase() }}</span>
              <div>
                <strong>{{ u.name }}</strong>
                <div v-if="u.fullName" class="sub">{{ u.fullName }}</div>
              </div>
            </div>
          </td>
          <td>
            <Tag v-if="u.admin" value="Administrator" severity="info" />
            <span v-else class="sub">Benutzer</span>
          </td>
          <td>{{ u.groups.length ? u.groups.join(', ') : '—' }}</td>
          <td class="actions">
            <Button label="Bearbeiten" icon="pi pi-pencil" size="small" text @click="openEdit(u)" />
            <Button label="Passwort" icon="pi pi-key" size="small" text @click="pwUser = u" />
            <Button
              label="2FA zurücksetzen"
              icon="pi pi-lock-open"
              size="small"
              text
              data-test="user-reset-2fa"
              :disabled="u.name === auth.user"
              @click="resetting = u"
            />
            <Button
              label="Löschen"
              icon="pi pi-trash"
              size="small"
              text
              severity="danger"
              :disabled="u.name === auth.user"
              @click="deleting = u"
            />
          </td>
        </tr>
      </tbody>
    </table>
  </div>
  <UserDialog v-model:visible="showDialog" :groups="store.groups" :users="store.users" :user="editing" :busy="busy" @submit="save" />
  <PasswordDialog
    :visible="!!pwUser"
    :user="pwUser?.name ?? ''"
    :busy="busy"
    @update:visible="(v) => !v && (pwUser = null)"
    @submit="(pw) => run(() => store.setPassword(pwUser!.name, pw), 'Passwort geändert')"
  />
  <ConfirmDialog
    :visible="!!resetting"
    header="Zwei-Faktor-Anmeldung zurücksetzen"
    :message="`Die Zwei-Faktor-Anmeldung von „${resetting?.name}“ wird abgeschaltet (z. B. nach einem verlorenen Gerät). ${resetting?.name} kann sich danach nur mit dem Passwort anmelden und sie im Profil neu einrichten.`"
    confirm-label="Zurücksetzen"
    :busy="busy"
    @update:visible="(v) => !v && (resetting = null)"
    @confirm="run(() => totpApi.reset(resetting!.name), '2FA zurückgesetzt')"
  />
  <ConfirmDialog
    :visible="!!deleting"
    header="Benutzer löschen"
    :message="`Der Benutzer „${deleting?.name}“ wird entfernt. Dateien, die ihm gehören, bleiben auf den Platten erhalten.`"
    :busy="busy"
    @update:visible="(v) => !v && (deleting = null)"
    @confirm="run(() => store.deleteUser(deleting!.name), 'Benutzer gelöscht')"
  />
</template>

<style scoped>
.who {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.table-wrap {
  overflow-x: auto;
}
.users {
  width: 100%;
  border-collapse: collapse;
}
th,
td {
  text-align: left;
  padding: 0.7rem 0.9rem;
  border-bottom: 1px solid var(--bn-border-soft);
  vertical-align: top;
}
th {
  font-size: 0.8rem;
  color: var(--p-text-muted-color);
  font-weight: 600;
}
.sub {
  font-size: 0.8rem;
  color: var(--p-text-muted-color);
  margin-top: 0.15rem;
}
.actions {
  display: flex;
  gap: 0.2rem;
  justify-content: flex-end;
  flex-wrap: wrap;
}
</style>
