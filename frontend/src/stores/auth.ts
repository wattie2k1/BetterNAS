import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, setCsrfToken } from '@/api/client'
import { useSetupStore } from './setup'

interface Me {
  username: string
  csrfToken: string
  totp?: boolean
  totpRequired?: boolean
}

interface MfaRequired {
  mfaRequired: true
  mfaToken: string
}

export const useAuthStore = defineStore('auth', () => {
  const user = ref<string | null>(null)
  /** Die Anmelde-Einstellungen verlangen 2FA, der Benutzer hat sie noch nicht eingerichtet. */
  const totpRequired = ref(false)

  function apply(me: Me) {
    user.value = me.username
    totpRequired.value = !!me.totpRequired
    setCsrfToken(me.csrfToken)
  }

  /** Meldet an. Ist 2FA aktiv, liefert es den mfaToken für den zweiten Schritt (noch keine Sitzung). */
  async function login(username: string, password: string): Promise<string | null> {
    const res = await api.post<Me | MfaRequired>('/api/v1/auth/login', { username, password })
    if ('mfaRequired' in res) return res.mfaToken
    apply(res)
    return null
  }

  async function loginTotp(mfaToken: string, code: string) {
    apply(await api.post<Me>('/api/v1/auth/login/totp', { mfaToken, code }))
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
      totpRequired.value = false
      setCsrfToken('')
      useSetupStore().reset()
    }
  }

  return { user, totpRequired, login, loginTotp, fetchMe, logout }
})
