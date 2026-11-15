import { api, apiFetch } from './client'

export interface UserView {
  name: string
  fullName: string
  uid: number
  groups: string[]
  admin: boolean
  /** UID < 10000, z. B. der Administrator aus der Installation. */
  system: boolean
}

export interface GroupView {
  name: string
  gid: number
  members: string[]
}

export interface UserInput {
  name: string
  fullName: string
  password: string
  groups: string[]
  admin: boolean
}

export type UserUpdate = Pick<UserInput, 'fullName' | 'groups' | 'admin'>

export interface GroupInput {
  name: string
  members: string[]
}

/** Identisch zur Backend-Regel (users.NameRe). */
export const NAME_RE = /^[a-z_][a-z0-9_-]{0,31}$/
export const MIN_PASSWORD_LEN = 8

const enc = encodeURIComponent

export const usersApi = {
  users: () => api.get<UserView[]>('/api/v1/users'),
  groups: () => api.get<GroupView[]>('/api/v1/groups'),
  createUser: (u: UserInput) => api.post<{ name: string }>('/api/v1/users', u),
  updateUser: (name: string, u: UserUpdate) => api.patch<void>(`/api/v1/users/${enc(name)}`, u),
  setPassword: (name: string, password: string) =>
    apiFetch<void>(`/api/v1/users/${enc(name)}/password`, { method: 'PUT', body: JSON.stringify({ password }) }),
  deleteUser: (name: string) => api.del<void>(`/api/v1/users/${enc(name)}`),
  createGroup: (g: GroupInput) => api.post<{ name: string }>('/api/v1/groups', g),
  setMembers: (name: string, members: string[]) => api.patch<void>(`/api/v1/groups/${enc(name)}`, { members }),
  deleteGroup: (name: string) => api.del<void>(`/api/v1/groups/${enc(name)}`),
}
