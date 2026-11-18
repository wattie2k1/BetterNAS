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

  const q = (sel: string) => document.querySelector<HTMLElement>(sel)
  const setSearch = async (w: ReturnType<typeof mountIt>, v: string) => {
    const el = q('[data-test="template-search"]') as HTMLInputElement
    el.value = v
    el.dispatchEvent(new Event('input'))
    await w.vm.$nextTick()
  }

  it('zeigt je Kategorie eine Überschrift', async () => {
    const w = mountIt()
    await w.vm.$nextTick()
    expect(q('[data-test="template-group-Medien"]')).toBeTruthy()
    expect(q('[data-test="template-group-Netzwerk"]')).toBeTruthy()
    w.unmount()
  })

  it('filtert per Suche und meldet fehlende Treffer', async () => {
    const w = mountIt()
    await w.vm.$nextTick()
    await setSearch(w, 'jelly')
    expect(document.querySelectorAll('[data-test^="template-"][data-test$="fin"]').length).toBe(1)
    expect(q('[data-test="template-nextcloud"]')).toBeNull()
    await setSearch(w, 'xyz')
    expect(q('[data-test="template-empty"]')).toBeTruthy()
    expect(q('[data-test="template-jellyfin"]')).toBeNull()
    w.unmount()
  })

  it('leert die Suche beim erneuten Öffnen', async () => {
    const w = mountIt()
    await w.vm.$nextTick()
    await setSearch(w, 'jelly')
    await w.setProps({ visible: false })
    await w.setProps({ visible: true })
    await w.vm.$nextTick()
    expect((q('[data-test="template-search"]') as HTMLInputElement).value).toBe('')
    w.unmount()
  })

  it('zeigt den Warnhinweis nur bei Vorlagen mit notice', async () => {
    const w = mountIt()
    await w.vm.$nextTick()
    q('[data-test="template-homeassistant"]')!.click()
    await w.vm.$nextTick()
    expect(q('[data-test="tpl-notice"]')?.textContent).toContain('privileged')
    await w.setProps({ visible: false })
    await w.setProps({ visible: true })
    await w.vm.$nextTick()
    q('[data-test="template-jellyfin"]')!.click()
    await w.vm.$nextTick()
    expect(q('[data-test="tpl-notice"]')).toBeNull()
    w.unmount()
  })
})

const tpl = (id: string) => {
  const t = APP_TEMPLATES.find((x) => x.id === id)
  expect(t, `Vorlage ${id} fehlt`).toBeTruthy()
  const files = renderTemplate(t!, t!.stack, defaultValues(t!, t!.stack))
  return { t: t!, files, env: files.env ?? '' }
}

describe('Downloader-Vorlagen', () => {
  const cases: [string, string, string][] = [
    ['qbittorrent', '8083', '${PORT}:8080'],
    ['sabnzbd', '8085', '${PORT}:8080'],
    ['nzbget', '6789', '${PORT}:6789'],
  ]
  for (const [id, port, line] of cases) {
    it(`rendert ${id}`, () => {
      const { t, files, env } = tpl(id)
      expect(t.category).toBe('Downloader')
      expect(files.compose).toContain(line)
      expect(files.compose).toContain('${DATA_DIR}:/data')
      expect(files.compose).toContain('PUID')
      expect(files.compose).toContain('PGID')
      expect(env).toContain(`PORT=${port}\n`)
    })
  }

  it('qBittorrent veröffentlicht den Torrent-Port für TCP und UDP', () => {
    const { files, env } = tpl('qbittorrent')
    expect(files.compose).toContain('"${TORRENT_PORT}:6881"')
    expect(files.compose).toContain('"${TORRENT_PORT}:6881/udp"')
    expect(env).toContain('TORRENT_PORT=6881\n')
  })
})

describe('*arr-Vorlagen', () => {
  const cases: [string, string, string, boolean][] = [
    ['prowlarr', '9696', '${PORT}:9696', false],
    ['radarr', '7878', '${PORT}:7878', true],
    ['sonarr', '8989', '${PORT}:8989', true],
    ['lidarr', '8686', '${PORT}:8686', true],
    ['bazarr', '6767', '${PORT}:6767', true],
    ['seerr', '5055', '${PORT}:5055', false],
  ]
  for (const [id, port, line, data] of cases) {
    it(`rendert ${id}`, () => {
      const { t, files, env } = tpl(id)
      expect(t.category).toBe('*arr und Wunschliste')
      expect(files.compose).toContain(line)
      expect(env).toContain(`PORT=${port}\n`)
      expect(files.compose.includes('${DATA_DIR}:/data')).toBe(data)
    })
  }

  it('die Suche nach arr findet Prowlarr, Radarr, Sonarr, Lidarr und Bazarr', () => {
    const ids = groupTemplates(APP_TEMPLATES, 'arr').flatMap((g) => g.items.map((t) => t.id))
    for (const id of ['prowlarr', 'radarr', 'sonarr', 'lidarr', 'bazarr']) expect(ids).toContain(id)
  })
})

describe('Medienserver-Vorlagen', () => {
  it('Plex läuft im Host-Netz ohne Portfeld, der Claim ist optional', () => {
    const { t, files, env } = tpl('plex')
    expect(t.category).toBe('Medien')
    expect(files.compose).toContain('network_mode: host')
    expect(files.compose).not.toContain('ports:')
    expect(t.fields.some((f) => f.kind === 'port')).toBe(false)
    expect(t.fields.find((f) => f.key === 'PLEX_CLAIM')?.optional).toBe(true)
    expect(env).toContain('PLEX_CLAIM=\n')
    expect(files.compose).toContain('${DATA_DIR}:/data:ro')
  })

  it('Emby, Audiobookshelf und Tdarr rendern mit den Vorgabe-Ports', () => {
    expect(tpl('emby').files.compose).toContain('${PORT}:8096')
    expect(tpl('emby').env).toContain('PORT=8097\n')
    expect(tpl('emby').files.compose).toContain('${DATA_DIR}:/data:ro')
    const a = tpl('audiobookshelf')
    expect(a.t.category).toBe('Medien')
    expect(a.files.compose).toContain('${PORT}:80')
    expect(a.env).toContain('PORT=13378\n')
    for (const m of ['/config', '/metadata', '/audiobooks', '/podcasts']) expect(a.files.compose).toContain(m)
    const d = tpl('tdarr')
    expect(d.t.category).toBe('Medien-Werkzeug')
    expect(d.env).toContain('PORT=8265\n')
    expect(d.env).toContain('SERVER_PORT=8266\n')
    expect(d.files.compose).toContain('internalNode')
    expect(d.files.compose).toContain('${DATA_DIR}:/data')
  })

  it('Plex, Emby und Tdarr bieten das Durchreichen der GPU an', () => {
    for (const id of ['plex', 'emby', 'tdarr']) {
      const t = APP_TEMPLATES.find((x) => x.id === id)!
      expect(t.device?.kind, id).toBe('gpu')
      expect(t.compose, id).toMatch(/^[ \t]*# @devices\n/m)
    }
  })
})

describe('Vorlagen mit Datenbank und weitere Apps', () => {
  it('RomM und Wiki.js haben eine Datenbank und kein Passwort im Klartext', () => {
    for (const id of ['romm', 'wikijs']) {
      const { t, files } = tpl(id)
      expect(files.compose, id).toMatch(/mariadb|postgres/)
      expect(files.compose, id).toContain('${DB_PASSWORD}')
      expect(t.fields.find((f) => f.key === 'DB_PASSWORD')?.kind, id).toBe('secret')
      for (const m of files.compose.matchAll(/^\s*[A-Za-z_]*(?:PASS|PASSWD|PASSWORD|SECRET)[A-Za-z_]*:\s*(.+)$/gm)) expect(m[1], `${id}: ${m[0]}`).toMatch(/^"\$\{[A-Z_]+\}"$/)
    }
    expect(tpl('romm').files.compose).toContain('${AUTH_SECRET}')
  })

  it('rendert die Apps mit Kategorie und Port', () => {
    const cases: [string, string, string, string][] = [
      ['romm', 'Medien', '8087', '${PORT}:8080'],
      ['wikijs', 'Wissen und Dokumente', '3005', '${PORT}:3000'],
      ['mealie', 'Wissen und Dokumente', '9925', '${PORT}:9000'],
      ['stirling-pdf', 'Wissen und Dokumente', '8091', '${PORT}:8080'],
      ['gitea', 'Entwicklung', '3006', '${PORT}:3000'],
    ]
    for (const [id, cat, port, line] of cases) {
      const { t, files, env } = tpl(id)
      expect(t.category, id).toBe(cat)
      expect(files.compose, id).toContain(line)
      expect(env, id).toContain(`PORT=${port}\n`)
    }
  })

  it('Gitea nutzt für SSH nie Port 22 (gehört dem BetterNAS-SSH)', () => {
    const { files, env } = tpl('gitea')
    expect(files.compose).toContain('${SSH_PORT}:22')
    expect(env).toContain('SSH_PORT=2222\n')
  })
})
