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

export interface ContainerStats {
  id: string
  name: string
  cpu: number
  memUsage: number
  memLimit: number
  memPercent: number
}

export interface UpdateInfo {
  outdated: string[]
  errors: string[]
}

export interface UpdateReport {
  checkedAt: string
  stacks: Record<string, UpdateInfo>
}

export interface ImageInfo {
  id: string
  ref: string
  repository: string
  tag: string
  sizeBytes: number
  created: string
  dangling: boolean
}

export interface NetworkInfo {
  id: string
  name: string
  driver: string
  scope: string
  builtin: boolean
}

export interface VolumeInfo {
  name: string
  driver: string
  unused: boolean
}

export type ResourceKind = 'images' | 'networks' | 'volumes'

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

/** WebSocket-Adresse zu einem API-Pfad (gleicher Host, ws/wss passend zu http/https). */
export function wsUrl(path: string): string {
  return `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}${path}`
}

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
  stats: () => api.get<ContainerStats[]>('/api/v1/containers/stats'),
  updates: () => api.get<UpdateReport>('/api/v1/docker/updates'),
  checkUpdates: () => api.post<{ jobId: string }>('/api/v1/docker/updates/check'),
  images: () => api.get<ImageInfo[]>('/api/v1/docker/images'),
  networks: () => api.get<NetworkInfo[]>('/api/v1/docker/networks'),
  volumes: () => api.get<VolumeInfo[]>('/api/v1/docker/volumes'),
  removeResource: (kind: ResourceKind, ref: string) => api.del<void>(`/api/v1/docker/${kind}?ref=${enc(ref)}`),
  prune: (kind: ResourceKind, all: boolean) => api.post<{ reclaimed: string }>(`/api/v1/docker/${kind}/prune`, { all }),
}
