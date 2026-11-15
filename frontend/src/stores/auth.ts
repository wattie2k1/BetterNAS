import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, setCsrfToken } from '@/api/client'

interface Me {
  username: string
  csrfToken: string
}

export const useAuthStore = defineStore('auth', () => {
  const user = ref<string | null>(null)

  function apply(me: Me) {
    user.value = me.username
    setCsrfToken(me.csrfToken)
  }

  async function login(username: string, password: string) {
    apply(await api.post<Me>('/api/v1/auth/login', { username, password }))
  }

  async function fetchMe(): Promise<boolean> {
    try {
      apply(await api.get<Me>('/api/v1/auth/me'))
      return true
    } catch {
      user.value = null
      return false
    }
  }

  async function logout() {
    try {
      await api.post('/api/v1/auth/logout')
    } finally {
      user.value = null
      setCsrfToken('')
    }
  }

  return { user, login, fetchMe, logout }
})
