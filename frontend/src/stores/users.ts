import { defineStore } from 'pinia'
import { ref } from 'vue'
import { usersApi, type GroupInput, type GroupView, type UserInput, type UserUpdate, type UserView } from '@/api/users'
import type { EventMessage } from '@/api/ws'

export const useUsersStore = defineStore('users', () => {
  const users = ref<UserView[]>([])
  const groups = ref<GroupView[]>([])
  const loading = ref(false)
  const error = ref('')

  async function refresh() {
    loading.value = true
    try {
      const [u, g] = await Promise.all([usersApi.users(), usersApi.groups()])
      users.value = u
      groups.value = g
      error.value = ''
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  function handleEvent(m: EventMessage) {
    if (m.type === 'users.changed') void refresh()
  }

  /** Führt eine Änderung aus und lädt danach neu – auch bei Fehlern, damit die Anzeige stimmt. */
  async function change(fn: () => Promise<unknown>) {
    try {
      await fn()
    } finally {
      await refresh()
    }
  }

  return {
    users,
    groups,
    loading,
    error,
    refresh,
    handleEvent,
    createUser: (u: UserInput) => change(() => usersApi.createUser(u)),
    updateUser: (name: string, u: UserUpdate) => change(() => usersApi.updateUser(name, u)),
    setPassword: (name: string, pw: string) => change(() => usersApi.setPassword(name, pw)),
    deleteUser: (name: string) => change(() => usersApi.deleteUser(name)),
    createGroup: (g: GroupInput) => change(() => usersApi.createGroup(g)),
    setMembers: (name: string, members: string[]) => change(() => usersApi.setMembers(name, members)),
    deleteGroup: (name: string) => change(() => usersApi.deleteGroup(name)),
  }
})
