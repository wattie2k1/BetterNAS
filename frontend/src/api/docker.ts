import { api } from './client'

export interface DockerStatus {
  installed: boolean
  active: boolean
  version: string
}

export interface StackInfo {
  name: string
  running: number
  total: number
}

export interface StackFiles {
  name: string
  compose: string
  env: string
}

export interface StackCheck {
  ok: boolean
  message: string
}

export interface ContainerInfo {
  id: string
  name: string
  image: string
  state: string
  status: string
  stack: string
  ports: string[]
  created: string
}

export type StackAction = 'up' | 'down' | 'stop' | 'start' | 'restart' | 'pull' | 'update'
export type ContainerAction = 'start' | 'stop' | 'restart' | 'remove'

/** Identisch zu docker.StackNameRe im Backend. */
export const STACK_NAME_RE = /^[a-z0-9][a-z0-9_-]{0,39}$/

export const STACK_TEMPLATE = `services:
  web:
    image: nginx:stable
    ports:
      - "8080:80"
    restart: unless-stopped
`

const enc = encodeURIComponent

export const dockerApi = {
  status: () => api.get<DockerStatus>('/api/v1/docker/status'),
  stacks: () => api.get<StackInfo[]>('/api/v1/stacks'),
  stack: (name: string) => api.get<StackFiles>(`/api/v1/stacks/${enc(name)}`),
  create: (s: StackFiles) => api.post<{ name: string }>('/api/v1/stacks', s),
  update: (s: StackFiles) => api.put<void>(`/api/v1/stacks/${enc(s.name)}`, s),
  remove: (name: string) => api.del<void>(`/api/v1/stacks/${enc(name)}`),
  validate: (name: string) => api.post<StackCheck>(`/api/v1/stacks/${enc(name)}/validate`),
  run: (name: string, action: StackAction) => api.post<{ jobId: string }>(`/api/v1/stacks/${enc(name)}/${action}`),
  containers: () => api.get<ContainerInfo[]>('/api/v1/containers'),
  containerAction: (id: string, action: ContainerAction) => api.post<void>(`/api/v1/containers/${enc(id)}/${action}`),
}
