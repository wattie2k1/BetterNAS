import { describe, expect, it } from 'vitest'
import { APP_TEMPLATES } from '@/utils/appTemplates'
import { formatMemory, memoryString, parseMemory, readLimits, setLimits } from '@/utils/composeLimits'

const sample = `# Medienserver
services:
  web:
    image: nginx # Webserver
    mem_limit: 512m
    cpus: 1.5
    ports:
      - "8080:80"

  db:
    image: postgres:18
    deploy:
      resources:
        limits:
          memory: 2G
          cpus: "2"
        reservations:
          memory: 1G
  plain:
    image: busybox
`

describe('readLimits', () => {
  it('liest mem_limit/cpus und deploy.resources.limits', () => {
    const r = readLimits(sample)
    expect(r.ok).toBe(true)
    if (!r.ok) return
    expect(r.services).toEqual([
      { service: 'web', memory: '512m', cpus: '1.5', where: 'service' },
      { service: 'db', memory: '2G', cpus: '2', where: 'deploy' },
      { service: 'plain', memory: null, cpus: null, where: 'service' },
    ])
  })
  it('meldet ungültiges YAML und akzeptiert Dateien ohne Dienste', () => {
    expect(readLimits('services: [\n  a').ok).toBe(false)
    expect(readLimits('')).toEqual({ ok: true, services: [] })
    expect(readLimits('name: x\n')).toEqual({ ok: true, services: [] })
  })
  it('lässt Variablen als Rohtext stehen', () => {
    const r = readLimits('services:\n  a:\n    image: x\n    mem_limit: ${MEM}\n')
    expect(r.ok && r.services[0].memory).toBe('${MEM}')
    expect(parseMemory('${MEM}')).toBeNull()
  })
})

describe('setLimits', () => {
  it('setzt Werte, behält Kommentare und alles andere', () => {
    const out = setLimits(sample, 'plain', { memory: '256m', cpus: '0.5' })
    expect(out).toContain('  plain:\n    image: busybox\n    mem_limit: 256m\n    cpus: 0.5\n')
    expect(out.replace(/    mem_limit: 256m\n    cpus: 0.5\n/, '')).toBe(sample)
  })
  it('ändert vorhandene Werte an ihrem Ort (deploy bleibt deploy, kein Doppelwert)', () => {
    const out = setLimits(sample, 'db', { memory: '3g', cpus: '4' })
    expect(out).toContain('memory: 3g')
    expect(out).toContain('cpus: 4')
    expect(out).toContain('reservations:\n          memory: 1G')
    expect(out).not.toMatch(/db:[\s\S]*\n    mem_limit/)
    expect(out).toContain('# Medienserver')
    expect(out).toContain('# Webserver')
    const web = setLimits(sample, 'web', { memory: '1g' })
    expect(web).toContain('mem_limit: 1g')
    expect(web).toContain('cpus: 1.5')
  })
  it('entfernt Limits und räumt leere deploy-Blöcke auf', () => {
    const out = setLimits(sample, 'web', { memory: null, cpus: null })
    expect(out).not.toContain('mem_limit')
    expect(out).not.toContain('cpus: 1.5')
    const db = setLimits(setLimits(sample, 'db', { memory: null, cpus: null }), 'plain', {})
    expect(db).toContain('reservations:') // Reservierungen bleiben
    expect(db).not.toContain('limits:') // leerer limits-Block ist weg
    const only = setLimits('services:\n  a:\n    image: x\n    deploy:\n      resources:\n        limits:\n          memory: 1g\n', 'a', { memory: null })
    expect(only).toBe('services:\n  a:\n    image: x\n')
  })
  it('lässt Unbekanntes und ungültige Dateien unverändert', () => {
    expect(setLimits(sample, 'gibtsnicht', { memory: '1g' })).toBe(sample)
    expect(setLimits('services: [\n', 'a', { memory: '1g' })).toBe('services: [\n')
    expect(setLimits(sample, 'web', {})).toBe(sample)
  })
  it('formatiert unberührte Zeilen nicht um', () => {
    const src = 'services:\n  a:\n    image: x\n    command: ["sleep", "600"]\n    environment: {A: "1", B: "2"}\n'
    expect(setLimits(src, 'a', { memory: '1g' })).toBe(src + '    mem_limit: 1g\n')
  })
  it('bricht lange Zeilen nicht um', () => {
    const long = `services:\n  a:\n    image: x\n    command: ${'wort '.repeat(40)}\n`
    expect(setLimits(long, 'a', { memory: '1g' })).toContain(`command: ${'wort '.repeat(40).trim()}`)
  })
  it('Hin und zurück ergibt für jede Vorlage und jeden Dienst den Originaltext', () => {
    for (const t of APP_TEMPLATES) {
      const r = readLimits(t.compose)
      expect(r.ok, t.id).toBe(true)
      if (!r.ok) continue
      for (const s of r.services) {
        const there = setLimits(t.compose, s.service, { memory: '512m', cpus: '1' })
        expect(there, `${t.id}.${s.service}`).not.toBe(t.compose)
        expect(setLimits(there, s.service, { memory: null, cpus: null }), `${t.id}.${s.service}`).toBe(t.compose)
      }
    }
  })
})

describe('Speichergrößen', () => {
  it('liest compose-Größen', () => {
    expect(parseMemory('512m')).toBe(512 * 1024 ** 2)
    expect(parseMemory('2G')).toBe(2 * 1024 ** 3)
    expect(parseMemory('1.5gb')).toBe(1.5 * 1024 ** 3)
    expect(parseMemory('1024')).toBe(1024)
    expect(parseMemory('64k')).toBe(64 * 1024)
    expect(parseMemory('viel')).toBeNull()
    expect(parseMemory('')).toBeNull()
  })
  it('zerlegt und baut Größen', () => {
    expect(formatMemory(2 * 1024 ** 3)).toEqual({ value: 2, unit: 'g' })
    expect(formatMemory(512 * 1024 ** 2)).toEqual({ value: 512, unit: 'm' })
    expect(formatMemory(1536 * 1024 ** 2)).toEqual({ value: 1536, unit: 'm' })
    expect(memoryString(2, 'g')).toBe('2g')
    expect(memoryString(1.5, 'g')).toBe('1536m')
    expect(memoryString(512, 'm')).toBe('512m')
  })
})
