// App-Vorlagen für Docker-Stacks: Eingaben (Ports, Pfade, Geheimnisse) landen in der .env,
// die compose.yaml verweist darauf per ${VARIABLE}. Der Stack bleibt danach frei editierbar.
import { STACK_NAME_RE, type HostDevice, type StackFiles } from '@/api/docker'
import { deviceSnippet } from '@/utils/deviceSnippet'

export type FieldKind = 'text' | 'port' | 'path' | 'secret' | 'ip'

export type TemplateCategory =
  | 'Medien'
  | 'Downloader'
  | '*arr und Wunschliste'
  | 'Verwaltung'
  | 'Netzwerk'
  | 'Wissen und Dokumente'
  | 'Entwicklung'
  | 'Medien-Werkzeug'

/** Anzeigereihenfolge der Kategorien im Vorlagen-Dialog. */
export const TEMPLATE_CATEGORIES: TemplateCategory[] = [
  'Medien',
  'Downloader',
  '*arr und Wunschliste',
  'Verwaltung',
  'Netzwerk',
  'Wissen und Dokumente',
  'Entwicklung',
  'Medien-Werkzeug',
]

export interface TemplateField {
  key: string
  label: string
  kind: FieldKind
  /** Darf leer bleiben (landet als `KEY=` in der .env). */
  optional?: boolean
  /** Vorgabe; bei Pfaden mit dem Stacknamen. */
  def?: (stack: string) => string
  hint?: string
}

/** Optionales Gerät (z. B. GPU für Hardware-Transcoding); der Marker `# @devices` in der compose.yaml zeigt, wo es hinkommt. */
export interface TemplateDevice {
  kind: 'gpu' | 'serial'
  label: string
  hint?: string
}

export interface AppTemplate {
  id: string
  title: string
  description: string
  icon: string
  category: TemplateCategory
  /** Warnhinweis im Formular (z. B. Docker-Socket, erweiterte Rechte). */
  notice?: string
  /** Vorschlag für den Stacknamen. */
  stack: string
  fields: TemplateField[]
  device?: TemplateDevice
  compose: string
}

const appdata = (sub: string) => (s: string) => `/srv/appdata/${s}/${sub}`
/** IPv4-Adresse, unter der diese Oberfläche erreicht wird (Vorgabe für Dienste, die eine eigene Adresse brauchen, z. B. DNS). */
const hostIp = () => {
  const h = typeof location === 'undefined' ? '' : location.hostname
  return /^\d{1,3}(\.\d{1,3}){3}$/.test(h) ? h : ''
}
const DNS_HINT =
  'IPv4-Adresse des NAS im Heimnetz. Port 53 ist auf dem NAS schon von Systemdiensten belegt, deshalb bindet der Container nur an diese Adresse. Geräte tragen sie später als DNS-Server ein.'
const MEDIA_HINT = 'Ordner auf einem Pool, z. B. /srv/pools/<Pool>/Medien'

/** Gemeinsamer Datenordner der Medien-Apps: Downloads und Medien liegen darin, damit Hardlinks funktionieren. */
export const dataField: TemplateField = {
  key: 'DATA_DIR',
  label: 'Datenordner (Downloads und Medien)',
  kind: 'path',
  def: () => '/srv/pools',
  hint: 'Ordner auf einem Pool, z. B. /srv/pools/<Pool>/Daten. Alle Medien-Apps sollten denselben Ordner nutzen, mit den Unterordnern downloads und media/filme, media/serien, media/musik. Nur so funktionieren Hardlinks, ohne Dateien zu kopieren.',
}

const IDS_HINT = 'Benutzer- und Gruppen-ID, unter der die App Dateien anlegt. Alle Medien-Apps sollten dieselben Werte nutzen.'
/** Benutzer- und Gruppen-ID der Medien-Apps (gleiche Werte in allen Stacks, damit Berechtigungen und Hardlinks stimmen). */
export const idFields: TemplateField[] = [
  { key: 'PUID', label: 'Benutzer-ID (PUID)', kind: 'text', def: () => '1000', hint: IDS_HINT },
  { key: 'PGID', label: 'Gruppen-ID (PGID)', kind: 'text', def: () => '1000' },
]

export const APP_TEMPLATES: AppTemplate[] = [
  {
    id: 'jellyfin',
    title: 'Jellyfin',
    description: 'Medienserver für Filme, Serien und Musik.',
    icon: 'pi pi-video',
    category: 'Medien',
    stack: 'jellyfin',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8096' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      { key: 'CACHE_DIR', label: 'Ordner für Cache', kind: 'path', def: appdata('cache') },
      { key: 'MEDIA_DIR', label: 'Medienordner (nur lesen)', kind: 'path', def: () => '/srv/pools', hint: MEDIA_HINT },
    ],
    device: {
      kind: 'gpu',
      label: 'Hardware-Transcoding (GPU)',
      hint: 'Reicht die Grafikeinheit an Jellyfin durch. Danach in Jellyfin unter Wiedergabe → Transcodierung „VAAPI“ bzw. „Intel QuickSync“ wählen.',
    },
    compose: `services:
  jellyfin:
    image: jellyfin/jellyfin:latest
    restart: unless-stopped
    ports:
      - "\${PORT}:8096"
    volumes:
      - \${CONFIG_DIR}:/config
      - \${CACHE_DIR}:/cache
      - \${MEDIA_DIR}:/media:ro
    # @devices
`,
  },
  {
    id: 'nextcloud',
    title: 'Nextcloud',
    description: 'Eigene Cloud für Dateien, Kalender und Kontakte (mit MariaDB).',
    icon: 'pi pi-cloud',
    category: 'Wissen und Dokumente',
    stack: 'nextcloud',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8081' },
      { key: 'DATA_DIR', label: 'Ordner für Nextcloud-Dateien', kind: 'path', def: appdata('html') },
      { key: 'DB_DIR', label: 'Ordner für die Datenbank', kind: 'path', def: appdata('db') },
      { key: 'DB_PASSWORD', label: 'Datenbank-Passwort', kind: 'secret' },
      { key: 'DB_ROOT_PASSWORD', label: 'Datenbank-Root-Passwort', kind: 'secret' },
    ],
    compose: `services:
  db:
    image: mariadb:11
    restart: unless-stopped
    command: --transaction-isolation=READ-COMMITTED --binlog-format=ROW
    environment:
      MYSQL_ROOT_PASSWORD: \${DB_ROOT_PASSWORD}
      MYSQL_PASSWORD: \${DB_PASSWORD}
      MYSQL_DATABASE: nextcloud
      MYSQL_USER: nextcloud
    volumes:
      - \${DB_DIR}:/var/lib/mysql
  app:
    image: nextcloud:stable
    restart: unless-stopped
    depends_on:
      - db
    ports:
      - "\${PORT}:80"
    environment:
      MYSQL_HOST: db
      MYSQL_DATABASE: nextcloud
      MYSQL_USER: nextcloud
      MYSQL_PASSWORD: \${DB_PASSWORD}
    volumes:
      - \${DATA_DIR}:/var/www/html
`,
  },
  {
    id: 'homeassistant',
    title: 'Home Assistant',
    description: 'Smart-Home-Zentrale (im Host-Netz, damit Geräte gefunden werden).',
    icon: 'pi pi-home',
    category: 'Verwaltung',
    stack: 'homeassistant',
    notice: 'Home Assistant läuft mit erweiterten Rechten (privileged) im Host-Netz, damit Geräte und Bluetooth/USB gefunden werden. Das gibt dem Container weitgehenden Zugriff auf das NAS.',
    fields: [{ key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') }],
    compose: `services:
  homeassistant:
    image: ghcr.io/home-assistant/home-assistant:stable
    restart: unless-stopped
    network_mode: host
    privileged: true
    volumes:
      - \${CONFIG_DIR}:/config
      - /etc/localtime:/etc/localtime:ro
`,
  },
  {
    id: 'vaultwarden',
    title: 'Vaultwarden',
    description: 'Passwortmanager, kompatibel mit Bitwarden (HTTPS über einen Reverse-Proxy nötig).',
    icon: 'pi pi-key',
    category: 'Verwaltung',
    stack: 'vaultwarden',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8082' },
      { key: 'DATA_DIR', label: 'Ordner für Daten', kind: 'path', def: appdata('data') },
    ],
    compose: `services:
  vaultwarden:
    image: vaultwarden/server:latest
    restart: unless-stopped
    ports:
      - "\${PORT}:80"
    volumes:
      - \${DATA_DIR}:/data
`,
  },
  {
    id: 'syncthing',
    title: 'Syncthing',
    description: 'Dateien zwischen Geräten abgleichen.',
    icon: 'pi pi-sync',
    category: 'Wissen und Dokumente',
    stack: 'syncthing',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8384' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      { key: 'DATA_DIR', label: 'Ordner mit den zu synchronisierenden Daten', kind: 'path', def: () => '/srv/pools', hint: MEDIA_HINT },
    ],
    compose: `services:
  syncthing:
    image: syncthing/syncthing:latest
    restart: unless-stopped
    ports:
      - "\${PORT}:8384"
      - "22000:22000/tcp"
      - "22000:22000/udp"
      - "21027:21027/udp"
    volumes:
      - \${CONFIG_DIR}:/var/syncthing/config
      - \${DATA_DIR}:/var/syncthing/data
`,
  },
  {
    id: 'uptime-kuma',
    title: 'Uptime Kuma',
    description: 'Überwacht Dienste und Webseiten und benachrichtigt bei Ausfall.',
    icon: 'pi pi-heart',
    category: 'Verwaltung',
    stack: 'uptime-kuma',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '3001' },
      { key: 'DATA_DIR', label: 'Ordner für Daten', kind: 'path', def: appdata('data') },
    ],
    compose: `services:
  uptime-kuma:
    image: louislam/uptime-kuma:1
    restart: unless-stopped
    ports:
      - "\${PORT}:3001"
    volumes:
      - \${DATA_DIR}:/app/data
`,
  },
  {
    id: 'immich',
    title: 'Immich',
    description: 'Foto- und Videoarchiv mit Gesichts- und Objekterkennung (App für Android und iOS).',
    icon: 'pi pi-images',
    category: 'Medien',
    stack: 'immich',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '2283' },
      { key: 'UPLOAD_DIR', label: 'Ordner für Fotos und Videos', kind: 'path', def: () => '/srv/pools', hint: 'Ordner auf einem Pool, z. B. /srv/pools/<Pool>/Fotos. Hier liegen alle hochgeladenen Dateien.' },
      { key: 'DB_DIR', label: 'Ordner für die Datenbank', kind: 'path', def: appdata('postgres'), hint: 'Auf einer lokalen Platte, nicht auf einer Netzwerkfreigabe.' },
      { key: 'DB_PASSWORD', label: 'Datenbank-Passwort', kind: 'secret' },
    ],
    compose: `services:
  immich-server:
    image: ghcr.io/immich-app/immich-server:v3
    restart: unless-stopped
    depends_on:
      - redis
      - database
    ports:
      - "\${PORT}:2283"
    environment:
      DB_USERNAME: postgres
      DB_DATABASE_NAME: immich
      DB_PASSWORD: "\${DB_PASSWORD}"
    volumes:
      - \${UPLOAD_DIR}:/data
      - /etc/localtime:/etc/localtime:ro
  immich-machine-learning:
    image: ghcr.io/immich-app/immich-machine-learning:v3
    restart: unless-stopped
    environment:
      MACHINE_LEARNING_MODEL_REVISION: v2
    volumes:
      - model-cache:/cache
  redis:
    image: docker.io/valkey/valkey:9
    restart: unless-stopped
  database:
    image: ghcr.io/immich-app/postgres:14-vectorchord0.4.3-pgvectors0.2.0
    restart: unless-stopped
    shm_size: 128mb
    environment:
      POSTGRES_USER: postgres
      POSTGRES_DB: immich
      POSTGRES_PASSWORD: "\${DB_PASSWORD}"
      POSTGRES_INITDB_ARGS: "--data-checksums"
    volumes:
      - \${DB_DIR}:/var/lib/postgresql/data
volumes:
  model-cache:
`,
  },
  {
    id: 'paperless',
    title: 'Paperless-ngx',
    description: 'Dokumentenarchiv: scannt, erkennt Text (OCR) und macht Dokumente durchsuchbar.',
    icon: 'pi pi-file',
    category: 'Wissen und Dokumente',
    stack: 'paperless',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8000' },
      { key: 'CONSUME_DIR', label: 'Eingangsordner (Scans hier ablegen)', kind: 'path', def: appdata('consume'), hint: 'Dateien in diesem Ordner werden automatisch importiert, z. B. von einem Scanner per Freigabe.' },
      { key: 'MEDIA_DIR', label: 'Ordner für die Dokumente', kind: 'path', def: () => '/srv/pools', hint: 'Ordner auf einem Pool, z. B. /srv/pools/<Pool>/Dokumente.' },
      { key: 'DATA_DIR', label: 'Ordner für Einstellungen und Index', kind: 'path', def: appdata('data') },
      { key: 'DB_DIR', label: 'Ordner für die Datenbank', kind: 'path', def: appdata('db') },
      { key: 'ADMIN_USER', label: 'Administrator-Name', kind: 'text', def: () => 'admin' },
      { key: 'ADMIN_PASSWORD', label: 'Administrator-Passwort', kind: 'secret' },
      { key: 'SECRET_KEY', label: 'Geheimer Schlüssel', kind: 'secret' },
      { key: 'DB_PASSWORD', label: 'Datenbank-Passwort', kind: 'secret' },
    ],
    compose: `services:
  broker:
    image: docker.io/valkey/valkey:9-alpine
    restart: unless-stopped
  db:
    image: docker.io/library/postgres:18
    restart: unless-stopped
    environment:
      POSTGRES_DB: paperless
      POSTGRES_USER: paperless
      POSTGRES_PASSWORD: "\${DB_PASSWORD}"
    volumes:
      - \${DB_DIR}:/var/lib/postgresql
  webserver:
    image: ghcr.io/paperless-ngx/paperless-ngx:latest
    restart: unless-stopped
    depends_on:
      - db
      - broker
    ports:
      - "\${PORT}:8000"
    environment:
      PAPERLESS_REDIS: redis://broker:6379
      PAPERLESS_DBENGINE: postgresql
      PAPERLESS_DBHOST: db
      PAPERLESS_DBPASS: "\${DB_PASSWORD}"
      PAPERLESS_SECRET_KEY: "\${SECRET_KEY}"
      PAPERLESS_ADMIN_USER: "\${ADMIN_USER}"
      PAPERLESS_ADMIN_PASSWORD: "\${ADMIN_PASSWORD}"
      PAPERLESS_TIME_ZONE: Europe/Berlin
      PAPERLESS_OCR_LANGUAGE: deu+eng
    volumes:
      - \${DATA_DIR}:/usr/src/paperless/data
      - \${MEDIA_DIR}:/usr/src/paperless/media
      - \${CONSUME_DIR}:/usr/src/paperless/consume
`,
  },
  {
    id: 'pihole',
    title: 'Pi-hole',
    description: 'Werbe- und Tracking-Blocker für das ganze Heimnetz, arbeitet als DNS-Server.',
    icon: 'pi pi-shield',
    category: 'Netzwerk',
    stack: 'pihole',
    fields: [
      { key: 'DNS_IP', label: 'IP-Adresse des NAS', kind: 'ip', def: hostIp, hint: DNS_HINT },
      { key: 'WEB_PORT', label: 'Web-Port der Oberfläche', kind: 'port', def: () => '8053' },
      { key: 'PASSWORD', label: 'Passwort der Oberfläche', kind: 'secret' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('etc-pihole') },
    ],
    compose: `services:
  pihole:
    image: pihole/pihole:latest
    restart: unless-stopped
    ports:
      - "\${DNS_IP}:53:53/tcp"
      - "\${DNS_IP}:53:53/udp"
      - "\${WEB_PORT}:80/tcp"
    environment:
      TZ: Europe/Berlin
      FTLCONF_webserver_api_password: "\${PASSWORD}"
      FTLCONF_dns_listeningMode: ALL
    volumes:
      - \${CONFIG_DIR}:/etc/pihole
`,
  },
  {
    id: 'adguardhome',
    title: 'AdGuard Home',
    description: 'Werbe- und Tracking-Blocker als DNS-Server (Alternative zu Pi-hole, nicht gleichzeitig verwenden).',
    icon: 'pi pi-shield',
    category: 'Netzwerk',
    stack: 'adguardhome',
    fields: [
      { key: 'DNS_IP', label: 'IP-Adresse des NAS', kind: 'ip', def: hostIp, hint: DNS_HINT },
      { key: 'SETUP_PORT', label: 'Port der Ersteinrichtung', kind: 'port', def: () => '3000', hint: 'Dort startet der Einrichtungsassistent. Wähle darin als Port der Oberfläche 80.' },
      { key: 'WEB_PORT', label: 'Web-Port der Oberfläche', kind: 'port', def: () => '8054', hint: 'Unter diesem Port erreichst du AdGuard Home nach der Einrichtung.' },
      { key: 'CONF_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('conf') },
      { key: 'WORK_DIR', label: 'Ordner für Daten und Statistik', kind: 'path', def: appdata('work') },
    ],
    compose: `services:
  adguardhome:
    image: adguard/adguardhome:latest
    restart: unless-stopped
    ports:
      - "\${DNS_IP}:53:53/tcp"
      - "\${DNS_IP}:53:53/udp"
      - "\${SETUP_PORT}:3000/tcp"
      - "\${WEB_PORT}:80/tcp"
    volumes:
      - \${CONF_DIR}:/opt/adguardhome/conf
      - \${WORK_DIR}:/opt/adguardhome/work
`,
  },
  {
    id: 'navidrome',
    title: 'Navidrome',
    description: 'Musikserver für die eigene Sammlung, kompatibel mit vielen Subsonic-Apps.',
    icon: 'pi pi-headphones',
    category: 'Medien',
    stack: 'navidrome',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '4533' },
      { key: 'DATA_DIR', label: 'Ordner für Daten', kind: 'path', def: appdata('data') },
      { key: 'MUSIC_DIR', label: 'Musikordner (nur lesen)', kind: 'path', def: () => '/srv/pools', hint: 'Ordner auf einem Pool, z. B. /srv/pools/<Pool>/Musik' },
    ],
    compose: `services:
  navidrome:
    image: deluan/navidrome:latest
    restart: unless-stopped
    ports:
      - "\${PORT}:4533"
    environment:
      ND_SCANSCHEDULE: 1h
      ND_LOGLEVEL: info
    volumes:
      - \${DATA_DIR}:/data
      - \${MUSIC_DIR}:/music:ro
`,
  },
  {
    id: 'qbittorrent',
    title: 'qBittorrent',
    description: 'Torrent-Client mit Weboberfläche. Das Anfangspasswort steht im Protokoll des Containers.',
    icon: 'pi pi-download',
    category: 'Downloader',
    stack: 'qbittorrent',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8083' },
      { key: 'TORRENT_PORT', label: 'Port für Torrent-Verbindungen', kind: 'port', def: () => '6881', hint: 'Wird für TCP und UDP geöffnet. Im Router weiterleiten, wenn Verbindungen von außen erlaubt sein sollen.' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      dataField,
      ...idFields,
    ],
    compose: `services:
  qbittorrent:
    image: lscr.io/linuxserver/qbittorrent:latest
    restart: unless-stopped
    environment:
      PUID: "\${PUID}"
      PGID: "\${PGID}"
      TZ: Europe/Berlin
      WEBUI_PORT: "8080"
    ports:
      - "\${PORT}:8080"
      - "\${TORRENT_PORT}:6881"
      - "\${TORRENT_PORT}:6881/udp"
    volumes:
      - \${CONFIG_DIR}:/config
      - \${DATA_DIR}:/data
`,
  },
  {
    id: 'sabnzbd',
    title: 'SABnzbd',
    description: 'Usenet-Downloader mit Weboberfläche. Die Zugangsdaten des Usenet-Anbieters trägst du dort ein.',
    icon: 'pi pi-download',
    category: 'Downloader',
    stack: 'sabnzbd',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8085' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      dataField,
      ...idFields,
    ],
    compose: `services:
  sabnzbd:
    image: lscr.io/linuxserver/sabnzbd:latest
    restart: unless-stopped
    environment:
      PUID: "\${PUID}"
      PGID: "\${PGID}"
      TZ: Europe/Berlin
    ports:
      - "\${PORT}:8080"
    volumes:
      - \${CONFIG_DIR}:/config
      - \${DATA_DIR}:/data
`,
  },
  {
    id: 'nzbget',
    title: 'NZBGet',
    description: 'Schlanker Usenet-Downloader. Standard-Zugang nzbget / tegbzn6789, bitte sofort ändern.',
    icon: 'pi pi-download',
    category: 'Downloader',
    stack: 'nzbget',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '6789' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      dataField,
      ...idFields,
    ],
    compose: `services:
  nzbget:
    image: lscr.io/linuxserver/nzbget:latest
    restart: unless-stopped
    environment:
      PUID: "\${PUID}"
      PGID: "\${PGID}"
      TZ: Europe/Berlin
    ports:
      - "\${PORT}:6789"
    volumes:
      - \${CONFIG_DIR}:/config
      - \${DATA_DIR}:/data
`,
  },
]

/** Vorlagen nach Kategorie gruppiert (in der Reihenfolge von TEMPLATE_CATEGORIES), gefiltert nach Titel, ID und Beschreibung. */
export function groupTemplates(list: AppTemplate[], query: string): { category: TemplateCategory; items: AppTemplate[] }[] {
  const q = query.trim().toLowerCase()
  const hit = (t: AppTemplate) => !q || [t.title, t.id, t.description].some((x) => x.toLowerCase().includes(q))
  return TEMPLATE_CATEGORIES.map((category) => ({ category, items: list.filter((t) => t.category === category && hit(t)) })).filter((g) => g.items.length > 0)
}

/** Zufälliges Geheimnis aus Buchstaben und Ziffern (in der .env ohne Quoting verwendbar). */
export function randomSecret(len = 24): string {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789'
  const buf = new Uint32Array(len)
  crypto.getRandomValues(buf)
  return Array.from(buf, (n) => chars[n % chars.length]).join('')
}

/** Vorbelegte Werte einer Vorlage (Geheimnisse frisch erzeugt). */
export function defaultValues(t: AppTemplate, stack: string): Record<string, string> {
  const v: Record<string, string> = {}
  for (const f of t.fields) v[f.key] = f.kind === 'secret' ? randomSecret() : (f.def?.(stack) ?? '')
  return v
}

/** Prüft einen Wert; liefert eine Fehlermeldung oder ''. */
export function fieldError(f: TemplateField, value: string): string {
  const v = value.trim()
  if (!v) return f.optional ? '' : 'Pflichtfeld'
  switch (f.kind) {
    case 'port': {
      const n = Number(v)
      return /^\d+$/.test(v) && n >= 1 && n <= 65535 ? '' : 'Port zwischen 1 und 65535'
    }
    case 'path':
      if (!v.startsWith('/') || v.includes('..')) return 'Absoluter Pfad ohne „..“'
      return /^[A-Za-z0-9_./+@-]+$/.test(v) ? '' : 'Nur Buchstaben, Ziffern und _ . / + @ -'
    case 'secret':
      return /^[A-Za-z0-9_.@+-]{8,}$/.test(v) ? '' : 'Mindestens 8 Zeichen aus Buchstaben, Ziffern und _ . @ + -'
    case 'ip': {
      const parts = v.split('.')
      return parts.length === 4 && parts.every((x) => /^\d{1,3}$/.test(x) && Number(x) <= 255) ? '' : 'IPv4-Adresse, z. B. 192.168.1.10'
    }
    default:
      return /^[^\n\r$"'#\s]+$/.test(v) ? '' : 'Keine Leerzeichen und keine Zeichen $ " \' #'
  }
}

export function stackNameError(name: string, existing: string[]): string {
  if (!STACK_NAME_RE.test(name)) return 'Nur Kleinbuchstaben, Ziffern, - und _ (höchstens 40 Zeichen).'
  if (existing.includes(name)) return 'Ein Stack mit diesem Namen existiert bereits.'
  return ''
}

/** Erzeugt die Stack-Dateien; der Aufrufer hat vorher alle Felder geprüft. */
export function renderTemplate(t: AppTemplate, name: string, values: Record<string, string>, devices: HostDevice[] = []): StackFiles {
  const env = t.fields.map((f) => `${f.key}=${values[f.key].trim()}`).join('\n') + '\n'
  // Der Marker `# @devices` wird durch die gewählten Geräte ersetzt oder verschwindet.
  const compose = t.compose.replace(/^[ \t]*# @devices\n/m, deviceSnippet(devices))
  return { name, compose, env }
}
