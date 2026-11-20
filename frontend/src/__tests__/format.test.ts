import { describe, expect, it } from 'vitest'
import { formatBytes, formatUptime } from '@/utils/format'

describe('formatBytes', () => {
  it('nutzt dezimale Einheiten und deutsches Komma', () => {
    expect(formatBytes(4000787030016)).toBe('4,0 TB')
    expect(formatBytes(512110190592)).toBe('512,1 GB')
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(999)).toBe('999 B')
  })
})

describe('formatUptime', () => {
  it('zeigt Tage, Stunden und Minuten', () => {
    expect(formatUptime(59)).toBe('0 Min.')
    expect(formatUptime(3 * 3600 + 5 * 60)).toBe('3 Std. 5 Min.')
    expect(formatUptime(2 * 86400 + 3600)).toBe('2 Tage 1 Std.')
    expect(formatUptime(86400)).toBe('1 Tag 0 Std.')
  })
})

import { formatUpFor } from '@/utils/format'
describe('formatUpFor', () => {
  it('nutzt den Docker-Stil', () => {
    expect(formatUpFor(1)).toBe('Up 1 second')
    expect(formatUpFor(300)).toBe('Up 5 minutes')
    expect(formatUpFor(3600)).toBe('Up 1 hour')
    expect(formatUpFor(10800)).toBe('Up 3 hours')
    expect(formatUpFor(3 * 86400)).toBe('Up 3 days')
  })
})
