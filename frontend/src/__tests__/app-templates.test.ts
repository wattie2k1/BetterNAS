import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import TemplateDialog from '@/components/docker/TemplateDialog.vue'
import { APP_TEMPLATES, roots, setRoots, TEMPLATE_CATEGORIES, containerNameError, containerNamesFor, defaultValues, fieldError, groupTemplates, randomSecret, renderTemplate } from '@/utils/appTemplates'

describe('Vorlagen: Speicherorte', () => {
  it('Einstellungen liegen unter appdata, alles andere unter dockerdata', () => {
    const before = { ...roots }
    setRoots({ appdata: '/srv/pools/a/cfg', dockerdata: '/srv/pools/b/dd' })
    try {
      for (const t of APP_TEMPLATES) {
        const v = defaultValues(t, t.stack)
        for (const f of t.fields.filter((x) => x.kind === 'path')) {
          const val = v[f.key]
          if (val.startsWith('/srv/pools/a/cfg/')) expect(['CONFIG_DIR', 'CONF_DIR', 'STATE_DIR', 'CERT_DIR'], `${t.id}.${f.key}`).toContain(f.key)
          else if (val.startsWith('/srv/pools/b/dd/')) expect(['CONFIG_DIR', 'CONF_DIR', 'STATE_DIR', 'CERT_DIR'], `${t.id}.${f.key}`).not.toContain(f.key)
          expect(val, `${t.id}.${f.key} hartes /srv/appdata`).not.toMatch(/^\/srv\/appdata\//)
        }
      }
      const jf = APP_TEMPLATES.find((x) => x.id === 'jellyfin')!
      const v = defaultValues(jf, 'jf')
      expect(v.CONFIG_DIR).toBe('/srv/pools/a/cfg/jf/config')
      expect(v.CACHE_DIR).toBe('/srv/pools/b/dd/jf/cache')
    } finally {
      setRoots(before)
    }
  })
})

describe('Vorlagen', () => {
  it('jede Vorlage hat für alle ${VARIABLEN} ein Feld und gültige Vorgaben', () => {
    for (const t of APP_TEMPLATES) {
      const used = [...t.compose.matchAll(/\$\{([A-Z_]+)\}/g)].map((m) => m[1])
      const keys = t.fields.map((f) => f.key)
      for (const u of used) expect(keys, `${t.id}: ${u}`).toContain(u)
      for (const k of keys) expect(used, `${t.id}: ${k} ungenutzt`).toContain(k)
      const v = defaultValues(t, t.stack)
      // Die IP-Adresse des NAS ist nur im Browser vorbelegt (aus der Adresszeile), im Test also leer.
      // Ausnahmen: die IP-Adresse (nur im Browser vorbelegt) und Pflichtangaben, die nur der Nutzer kennt (z. B. Auth-Key).
      for (const f of t.fields) {
        const userSupplied = f.kind === 'ip' || (f.kind === 'text' && !f.optional && v[f.key] === '')
        const err = fieldError(f, v[f.key])
        // Schreibordner: die Wurzel /srv/pools ist als Vorgabe bewusst ungültig (der Nutzer wählt einen Pool).
        if (f.pool) expect(err, `${t.id}.${f.key}`).toMatch(/Pool/)
        else expect(err, `${t.id}.${f.key}`).toBe(userSupplied ? 'Pflichtfeld' : '')
      }
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
    const hits = groupTemplates(APP_TEMPLATES, '  JELLY ').flatMap((g) => g.items.map((t) => t.id))
    expect(hits).toContain('jellyfin')
    expect(hits).not.toContain('radarr')
    expect(groupTemplates(APP_TEMPLATES, 'xyz')).toEqual([])
  })
})

describe('Suche und Duplicati', () => {
  it('die Suche findet auch über den Kategorienamen', () => {
    const ids = groupTemplates(APP_TEMPLATES, 'verwaltung').flatMap((g) => g.items.map((t) => t.id))
    expect(ids).toContain('duplicati')
    expect(ids).not.toContain('jellyfin')
  })

  it('Duplicati bekommt einen erzeugten Schlüssel für die Einstellungsdatenbank', () => {
    const { t, files, env } = tpl('duplicati')
    const f = t.fields.find((x) => x.key === 'SETTINGS_ENCRYPTION_KEY')!
    expect(f.kind).toBe('secret')
    expect(files.compose).toContain('SETTINGS_ENCRYPTION_KEY: "${SETTINGS_ENCRYPTION_KEY}"')
    expect(env).toMatch(/SETTINGS_ENCRYPTION_KEY=[A-Za-z0-9]{24}\n/)
  })
})

describe('Git-Server zeigen den SSH-Port in der Klon-URL', () => {
  for (const [id, pre] of [['gitea', 'GITEA'], ['forgejo', 'FORGEJO']]) {
    it(`${id} setzt ${pre}__server__SSH_PORT auf den gewählten Port`, () => {
      const { files, env } = tpl(id)
      expect(files.compose).toContain(`${pre}__server__SSH_PORT: "\${SSH_PORT}"`)
      expect(env).toMatch(/SSH_PORT=\d+\n/)
    })
  }
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

describe('Verwaltungs- und Netzwerk-Vorlagen', () => {
  it('rendert die Verwaltungs-Apps mit Kategorie, Port und Container-Port', () => {
    const cases: [string, string, string][] = [
      ['portainer', '9443', '${PORT}:9443'],
      ['dozzle', '8888', '${PORT}:8080'],
      ['heimdall', '8090', '${PORT}:80'],
      ['duplicati', '8200', '${PORT}:8200'],
    ]
    for (const [id, port, line] of cases) {
      const { t, files, env } = tpl(id)
      expect(t.category, id).toBe('Verwaltung')
      expect(files.compose, id).toContain(line)
      expect(env, id).toContain(`PORT=${port}\n`)
    }
  })

  it('Portainer, Dozzle und Tailscale warnen vor erweiterten Rechten', () => {
    for (const id of ['portainer', 'dozzle', 'tailscale']) expect(APP_TEMPLATES.find((t) => t.id === id)?.notice, id).toBeTruthy()
  })

  it('Dozzle bindet den Docker-Socket nur lesend, Portainer mit Datenordner', () => {
    expect(tpl('dozzle').files.compose).toContain('/var/run/docker.sock:/var/run/docker.sock:ro')
    expect(tpl('portainer').files.compose).toContain('${DATA_DIR}:/data')
  })

  it('Duplicati liest den Quellordner nur und schreibt Sicherungen in einen eigenen Ordner', () => {
    const { files } = tpl('duplicati')
    expect(files.compose).toContain('${SOURCE_DIR}:/source:ro')
    expect(files.compose).toContain('${BACKUP_DIR}:/backups')
  })

  it('Tailscale braucht NET_ADMIN, /dev/net/tun und das Host-Netz ohne Portfeld', () => {
    const { t, files } = tpl('tailscale')
    expect(t.category).toBe('Netzwerk')
    expect(files.compose).toContain('NET_ADMIN')
    expect(files.compose).toContain('/dev/net/tun')
    expect(files.compose).toContain('network_mode: host')
    expect(files.compose).not.toContain('ports:')
    expect(t.fields.some((f) => f.kind === 'port')).toBe(false)
  })

  it('Tailscale: Auth-Key ist Pflicht und wird nicht zufällig vorbelegt, Name ist optional', () => {
    const t = APP_TEMPLATES.find((x) => x.id === 'tailscale')!
    const key = t.fields.find((f) => f.key === 'AUTH_KEY')!
    expect(key.kind).toBe('text')
    expect(defaultValues(t, t.stack).AUTH_KEY).toBe('')
    expect(fieldError(key, '')).toBe('Pflichtfeld')
    expect(fieldError(key, ['tskey', 'auth', 'testwert'].join('-'))).toBe('')
    expect(t.fields.find((f) => f.key === 'NODE_NAME')?.optional).toBe(true)
  })
})

describe('Abschlussreview: Rechte, Ordner, Hinweise', () => {
  const withIds = () => APP_TEMPLATES.filter((t) => t.fields.some((f) => f.key === 'PUID'))

  it('Medien-Apps laufen in der Gruppe users (100) mit group-writable Dateien, wie die Samba-Freigaben', () => {
    expect(withIds().length).toBeGreaterThan(10)
    for (const t of withIds()) {
      expect(defaultValues(t, t.stack).PGID, t.id).toBe('100')
      expect(t.compose, t.id).toContain('UMASK: "002"')
    }
  })

  it('Schreibordner lehnen die Pool-Wurzel /srv/pools ab, Plex und Emby (nur lesen) nicht', () => {
    const data = APP_TEMPLATES.find((t) => t.id === 'radarr')!.fields.find((f) => f.key === 'DATA_DIR')!
    for (const bad of ['/srv/pools', '/srv/pools/']) expect(fieldError(data, bad), bad).toMatch(/Pool/)
    expect(fieldError(data, '/srv/pools/Medien')).toBe('')
    for (const [id, key] of [['qbittorrent', 'DATA_DIR'], ['tdarr', 'DATA_DIR'], ['romm', 'ROMS_DIR'], ['duplicati', 'BACKUP_DIR'], ['audiobookshelf', 'PODCASTS_DIR']]) {
      const t = APP_TEMPLATES.find((x) => x.id === id)!
      expect(fieldError(t.fields.find((f) => f.key === key)!, '/srv/pools'), `${id}.${key}`).toMatch(/Pool/)
    }
    for (const id of ['plex', 'emby']) {
      const t = APP_TEMPLATES.find((x) => x.id === id)!
      expect(fieldError(t.fields.find((f) => f.key === 'DATA_DIR')!, '/srv/pools'), id).toBe('')
    }
  })

  it('Seerr läuft als root, damit der von Docker angelegte Ordner beschreibbar ist', () => {
    const t = APP_TEMPLATES.find((x) => x.id === 'seerr')!
    expect(t.compose).toContain('user: "0:0"')
    expect(JSON.stringify(t.fields)).not.toContain('chown')
  })

  it('es gibt keine Vorlage für archivierte Projekte ohne Sicherheitsfixes (Filebrowser)', () => {
    expect(APP_TEMPLATES.map((t) => t.id)).not.toContain('filebrowser')
  })

  it('RomM bewahrt Cover und Metadaten in einem Volume auf', () => {
    const { files } = tpl('romm')
    expect(files.compose).toContain('romm-resources:/romm/resources')
    expect(files.compose).toMatch(/^volumes:\n(  .*\n)*  romm-resources:/m)
  })

  it('Plex und Tailscale weisen auf die BetterNAS-Firewall hin', () => {
    for (const id of ['plex', 'tailscale']) expect(APP_TEMPLATES.find((t) => t.id === id)?.notice, id).toMatch(/Firewall/)
  })
})

describe('Container-Namen', () => {
  it('jede Vorlage vergibt eindeutige feste Namen, alle beginnen mit dem Stack-Namen', () => {
    for (const t of APP_TEMPLATES) {
      const names = containerNamesFor(t, 'demo')
      for (const n of names) expect(n === 'demo' || n.startsWith('demo-'), t.title).toBe(true)
      expect(new Set(names).size, t.title).toBe(names.length)
      const files = renderTemplate(t, 'demo', defaultValues(t, 'demo'))
      for (const n of names) expect(files.compose, t.title).toContain(`container_name: ${n}\n`)
      expect(files.compose.match(/container_name:/g)?.length, t.title).toBe(names.length)
    }
  })

  it('meldet bereits vergebene Container-Namen', () => {
    const t = APP_TEMPLATES.find((x) => x.stack === 'nextcloud')!
    expect(containerNameError(t, 'cloud', ['andere'])).toBe('')
    expect(containerNameError(t, 'cloud', ['cloud-db'])).toContain('„cloud-db“')
  })
})
