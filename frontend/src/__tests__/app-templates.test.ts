import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import TemplateDialog from '@/components/docker/TemplateDialog.vue'
import { APP_TEMPLATES, TEMPLATE_CATEGORIES, defaultValues, fieldError, groupTemplates, randomSecret, renderTemplate } from '@/utils/appTemplates'

describe('Vorlagen', () => {
  it('jede Vorlage hat für alle ${VARIABLEN} ein Feld und gültige Vorgaben', () => {
    for (const t of APP_TEMPLATES) {
      const used = [...t.compose.matchAll(/\$\{([A-Z_]+)\}/g)].map((m) => m[1])
      const keys = t.fields.map((f) => f.key)
      for (const u of used) expect(keys, `${t.id}: ${u}`).toContain(u)
      for (const k of keys) expect(used, `${t.id}: ${k} ungenutzt`).toContain(k)
      const v = defaultValues(t, t.stack)
      // Die IP-Adresse des NAS ist nur im Browser vorbelegt (aus der Adresszeile), im Test also leer.
      for (const f of t.fields) expect(fieldError(f, v[f.key]), `${t.id}.${f.key}`).toBe(f.kind === 'ip' ? 'Pflichtfeld' : '')
    }
  })

  it('rendert die .env aus den Werten', () => {
    const t = APP_TEMPLATES.find((x) => x.id === 'jellyfin')!
    const files = renderTemplate(t, 'jf', { PORT: '9000', CONFIG_DIR: '/srv/appdata/jf/config', CACHE_DIR: '/srv/appdata/jf/cache', MEDIA_DIR: '/srv/pools/a' })
    expect(files.name).toBe('jf')
    expect(files.env).toBe('PORT=9000\nCONFIG_DIR=/srv/appdata/jf/config\nCACHE_DIR=/srv/appdata/jf/cache\nMEDIA_DIR=/srv/pools/a\n')
    expect(files.compose).toContain('${PORT}:8096')
  })

  it('hat eindeutige Namen und keine doppelten Vorgabe-Ports', () => {
    const ids = APP_TEMPLATES.map((t) => t.id)
    expect(new Set(ids).size).toBe(ids.length)
    expect(new Set(APP_TEMPLATES.map((t) => t.stack)).size).toBe(ids.length)
    const ports = APP_TEMPLATES.flatMap((t) => t.fields.filter((f) => f.kind === 'port').map((f) => `${t.id}.${f.key}=${f.def!('x')}`))
    const values = ports.map((p) => p.split('=')[1])
    expect(new Set(values).size, ports.join(', ')).toBe(values.length)
  })

  it('belegt Port 53 nie auf allen Adressen (dort laufen schon systemd-resolved und libvirt)', () => {
    for (const t of APP_TEMPLATES) {
      for (const m of t.compose.matchAll(/"([^"]*):?53:53\/(tcp|udp)"/g)) expect(m[0], t.id).toMatch(/^"\$\{DNS_IP\}:53:53/)
    }
    const dns = APP_TEMPLATES.filter((t) => t.compose.includes(':53:53'))
    expect(dns.map((t) => t.id).sort()).toEqual(['adguardhome', 'pihole'])
  })

  it('prüft IPv4-Adressen', () => {
    const ip = { key: 'I', label: '', kind: 'ip' as const }
    expect(fieldError(ip, '192.168.1.10')).toBe('')
    for (const bad of ['', '192.168.1', '192.168.1.256', 'nas.local', '1.2.3.4.5', '::1']) expect(fieldError(ip, bad), bad).not.toBe('')
  })

  it('prüft Eingaben', () => {
    const port = { key: 'P', label: '', kind: 'port' as const }
    const path = { key: 'D', label: '', kind: 'path' as const }
    const secret = { key: 'S', label: '', kind: 'secret' as const }
    expect(fieldError(port, '0')).not.toBe('')
    expect(fieldError(port, '65536')).not.toBe('')
    expect(fieldError(port, '8080')).toBe('')
    expect(fieldError(path, 'relativ')).not.toBe('')
    expect(fieldError(path, '/srv/../etc')).not.toBe('')
    expect(fieldError(path, '/srv/a b')).not.toBe('')
    expect(fieldError(path, '/srv/pools/x')).toBe('')
    expect(fieldError(secret, 'kurz')).not.toBe('')
    expect(fieldError(secret, randomSecret())).toBe('')
    expect(randomSecret()).not.toBe(randomSecret())
  })
})

describe('Vorlagen: Regeln für alle', () => {
  it('jede Vorlage hat eine gültige Kategorie', () => {
    for (const t of APP_TEMPLATES) expect(TEMPLATE_CATEGORIES, t.id).toContain(t.category)
  })

  it('Vorlagen mit Docker-Socket, privileged oder NET_ADMIN haben einen Hinweis', () => {
    for (const t of APP_TEMPLATES) {
      if (/docker\.sock|privileged: true|NET_ADMIN/.test(t.compose)) expect(t.notice, t.id).toBeTruthy()
    }
  })

  it('Vorlagen mit PUID haben auch PGID und binden den Datenordner', () => {
    for (const t of APP_TEMPLATES) {
      const keys = t.fields.map((f) => f.key)
      if (!keys.includes('PUID')) continue
      expect(keys, t.id).toContain('PGID')
      if (keys.includes('DATA_DIR')) expect(t.compose, t.id).toMatch(/\$\{DATA_DIR\}:\/\w+/)
    }
  })

  it('optionale Felder dürfen leer sein', () => {
    const f = { key: 'X', label: '', kind: 'text' as const }
    expect(fieldError({ ...f, optional: true }, '')).toBe('')
    expect(fieldError(f, '')).toBe('Pflichtfeld')
    const t = { id: 't', title: 't', description: '', icon: '', stack: 't', category: 'Verwaltung' as const, fields: [{ ...f, optional: true }, { key: 'Y', label: '', kind: 'port' as const }], compose: '' }
    expect(renderTemplate(t, 't', { X: '', Y: '80' }).env).toBe('X=\nY=80\n')
  })

  it('gruppiert und filtert die Vorlagen', () => {
    const all = groupTemplates(APP_TEMPLATES, '')
    expect(all.flatMap((g) => g.items).length).toBe(APP_TEMPLATES.length)
    const order = all.map((g) => TEMPLATE_CATEGORIES.indexOf(g.category))
    expect(order).toEqual([...order].sort((a, b) => a - b))
    expect(all.every((g) => g.items.length > 0)).toBe(true)
    expect(groupTemplates(APP_TEMPLATES, '  JELLY ').flatMap((g) => g.items.map((t) => t.id))).toEqual(['jellyfin'])
    expect(groupTemplates(APP_TEMPLATES, 'xyz')).toEqual([])
  })
})

describe('TemplateDialog', () => {
  const mountIt = () => mount(TemplateDialog, { props: { visible: true, existing: ['jellyfin'] }, global: { plugins: [PrimeVue] }, attachTo: document.body })

  it('wählt Vorlage, vergibt freien Namen und liefert die Dateien', async () => {
    const w = mountIt()
    await w.vm.$nextTick()
    document.querySelector<HTMLButtonElement>('[data-test="template-jellyfin"]')!.click()
    await w.vm.$nextTick()
    const name = document.querySelector<HTMLInputElement>('#tpl-name')!
    expect(name.value).toBe('jellyfin-2')
    expect(document.querySelector<HTMLInputElement>('[data-test="tpl-field-CONFIG_DIR"]')!.value).toBe('/srv/appdata/jellyfin-2/config')
    document.querySelector<HTMLButtonElement>('[data-test="template-next"]')!.click()
    await w.vm.$nextTick()
    const ev = w.emitted('create')![0][0] as { name: string; env: string }
    expect(ev.name).toBe('jellyfin-2')
    expect(ev.env).toContain('PORT=8096')
    w.unmount()
  })
})
