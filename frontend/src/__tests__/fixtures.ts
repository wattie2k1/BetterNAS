import type { DiskView, FormatPreview, PoolView } from '@/api/storage'

export function disk(over: Partial<DiskView>): DiskView {
  return {
    id: 'd1',
    device: '/dev/sda',
    model: 'WDC WD40EFRX',
    serial: 'WD-WCC7K1234567',
    wwn: '',
    transport: 'sata',
    sizeBytes: 4000787030016,
    rotational: true,
    system: false,
    manageable: true,
    partitions: [],
    role: 'unassigned',
    state: 'online',
    ...over,
  }
}

export const disks: DiskView[] = [
  disk({ id: 'sys', device: '/dev/nvme0n1', model: 'Samsung SSD', serial: 'S1', role: 'system', system: true, manageable: false, reason: 'Systemplatte', rotational: false }),
  disk({ id: 'd1', role: 'data', pool: 'media', fsType: 'ext4' }),
  disk({ id: 'd2', device: '/dev/sdb', serial: 'SER2', role: 'data', fsType: 'ext4' }),
  disk({ id: 'd3', device: '/dev/sdc', serial: 'SER3', role: 'unassigned' }),
  disk({ id: 'd4', device: '', serial: 'SER4', role: 'data', state: 'missing' }),
]

export const pools: PoolView[] = [
  { name: 'media', branches: ['d1'], createPolicy: 'epmfs', minFree: '20G', path: '/srv/pools/media', sizeBytes: 4e12, freeBytes: 3e12, mounted: true },
]

export const preview: FormatPreview = {
  diskId: 'd3',
  device: '/dev/sdc',
  model: 'WDC WD40EFRX',
  serial: 'WD-WCC7K1234567',
  sizeBytes: 4000787030016,
  partitions: [{ device: '/dev/sdc1', fsType: 'ntfs', uuid: 'x', label: 'Backup', sizeBytes: 4e12, mountpoints: [] }],
  confirmToken: 'tok',
  expiresAt: '2026-11-15T12:00:00Z',
}

export const parityDisks: DiskView[] = [
  disk({ id: 'p1', device: '/dev/sde', model: 'Parity 4TB', serial: 'P1', role: 'parity', fsType: 'ext4' }),
  disk({ id: 'p2', device: '/dev/sdf', model: 'Parity 1TB', serial: 'P2', role: 'parity', fsType: 'ext4', sizeBytes: 1e12 }),
  disk({ id: 'p3', device: '/dev/sdg', model: 'Parity 4TB b', serial: 'P3', role: 'parity', fsType: 'ext4' }),
  disk({ id: 'p4', device: '/dev/sdh', model: 'Parity fremd', serial: 'P4', role: 'parity', fsType: 'ext4', pool: 'other' }),
]

export const snapraidDefaults = {
  syncSchedule: '*-*-* 03:00:00',
  scrubSchedule: 'Sun *-*-* 05:00:00',
  scrubPercent: 8,
  scrubOlderDays: 10,
  deleteThreshold: 500,
}
