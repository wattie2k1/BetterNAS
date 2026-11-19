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
  /** Schreibordner: muss in einem Pool liegen, die Wurzel /srv/pools selbst ist nicht erlaubt. */
  pool?: boolean
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
  pool: true,
  hint: 'Ordner auf einem Pool, z. B. /srv/pools/<Pool>/Daten. Alle Medien-Apps sollten denselben Ordner nutzen, mit den Unterordnern downloads und media/filme, media/serien, media/musik. Nur so funktionieren Hardlinks, ohne Dateien zu kopieren.',
}

const IDS_HINT = 'Benutzer- und Gruppen-ID, unter der die App Dateien anlegt. Die Gruppe 100 (users) nutzen auch die SMB-Freigaben, deshalb dürfen alle Nutzer die Dateien bearbeiten. Alle Medien-Apps sollten dieselben Werte nutzen.'
/** Benutzer- und Gruppen-ID der Medien-Apps (gleiche Werte in allen Stacks, damit Berechtigungen und Hardlinks stimmen). */
export const idFields: TemplateField[] = [
  { key: 'PUID', label: 'Benutzer-ID (PUID)', kind: 'text', def: () => '1000', hint: IDS_HINT },
  { key: 'PGID', label: 'Gruppen-ID (PGID)', kind: 'text', def: () => '100' },
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
      UMASK: "002"
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
      UMASK: "002"
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
      UMASK: "002"
      TZ: Europe/Berlin
    ports:
      - "\${PORT}:6789"
    volumes:
      - \${CONFIG_DIR}:/config
      - \${DATA_DIR}:/data
`,
  },
  {
    id: 'prowlarr',
    title: 'Prowlarr',
    description: 'Indexer-Verwaltung für alle *arr-Apps. Die Apps verbindest du in den Einstellungen mit URL und API-Key.',
    icon: 'pi pi-search',
    category: '*arr und Wunschliste',
    stack: 'prowlarr',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '9696' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      ...idFields,
    ],
    compose: `services:
  prowlarr:
    image: lscr.io/linuxserver/prowlarr:latest
    restart: unless-stopped
    environment:
      PUID: "\${PUID}"
      PGID: "\${PGID}"
      UMASK: "002"
      TZ: Europe/Berlin
    ports:
      - "\${PORT}:9696"
    volumes:
      - \${CONFIG_DIR}:/config
`,
  },
  {
    id: 'radarr',
    title: 'Radarr',
    description: 'Filmverwaltung der *arr-Suite. Nutzt denselben Datenordner wie der Download-Client, damit Dateien nicht kopiert werden.',
    icon: 'pi pi-video',
    category: '*arr und Wunschliste',
    stack: 'radarr',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '7878' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      dataField,
      ...idFields,
    ],
    compose: `services:
  radarr:
    image: lscr.io/linuxserver/radarr:latest
    restart: unless-stopped
    environment:
      PUID: "\${PUID}"
      PGID: "\${PGID}"
      UMASK: "002"
      TZ: Europe/Berlin
    ports:
      - "\${PORT}:7878"
    volumes:
      - \${CONFIG_DIR}:/config
      - \${DATA_DIR}:/data
`,
  },
  {
    id: 'sonarr',
    title: 'Sonarr',
    description: 'Serienverwaltung der *arr-Suite. Nutzt denselben Datenordner wie der Download-Client, damit Dateien nicht kopiert werden.',
    icon: 'pi pi-desktop',
    category: '*arr und Wunschliste',
    stack: 'sonarr',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8989' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      dataField,
      ...idFields,
    ],
    compose: `services:
  sonarr:
    image: lscr.io/linuxserver/sonarr:latest
    restart: unless-stopped
    environment:
      PUID: "\${PUID}"
      PGID: "\${PGID}"
      UMASK: "002"
      TZ: Europe/Berlin
    ports:
      - "\${PORT}:8989"
    volumes:
      - \${CONFIG_DIR}:/config
      - \${DATA_DIR}:/data
`,
  },
  {
    id: 'lidarr',
    title: 'Lidarr',
    description: 'Musikverwaltung der *arr-Suite. Nutzt denselben Datenordner wie der Download-Client, damit Dateien nicht kopiert werden.',
    icon: 'pi pi-headphones',
    category: '*arr und Wunschliste',
    stack: 'lidarr',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8686' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      dataField,
      ...idFields,
    ],
    compose: `services:
  lidarr:
    image: lscr.io/linuxserver/lidarr:latest
    restart: unless-stopped
    environment:
      PUID: "\${PUID}"
      PGID: "\${PGID}"
      UMASK: "002"
      TZ: Europe/Berlin
    ports:
      - "\${PORT}:8686"
    volumes:
      - \${CONFIG_DIR}:/config
      - \${DATA_DIR}:/data
`,
  },
  {
    id: 'bazarr',
    title: 'Bazarr',
    description: 'Untertitel für Radarr und Sonarr, ergänzt die *arr-Suite.',
    icon: 'pi pi-comments',
    category: '*arr und Wunschliste',
    stack: 'bazarr',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '6767' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      dataField,
      ...idFields,
    ],
    compose: `services:
  bazarr:
    image: lscr.io/linuxserver/bazarr:latest
    restart: unless-stopped
    environment:
      PUID: "\${PUID}"
      PGID: "\${PGID}"
      UMASK: "002"
      TZ: Europe/Berlin
    ports:
      - "\${PORT}:6767"
    volumes:
      - \${CONFIG_DIR}:/config
      - \${DATA_DIR}:/data
`,
  },
  {
    id: 'seerr',
    title: 'Seerr',
    description: 'Wunschliste für Filme und Serien: Nutzer bitten um Titel, Radarr und Sonarr besorgen sie.',
    icon: 'pi pi-star',
    category: '*arr und Wunschliste',
    stack: 'seerr',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '5055' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
    ],
    compose: `services:
  seerr:
    image: ghcr.io/seerr-team/seerr:latest
    init: true
    user: "0:0"
    restart: unless-stopped
    environment:
      TZ: Europe/Berlin
    ports:
      - "\${PORT}:5055"
    volumes:
      - \${CONFIG_DIR}:/app/config
`,
  },
  {
    id: 'plex',
    title: 'Plex',
    description: 'Medienserver für Filme, Serien und Musik mit Apps für fast jedes Gerät (im Host-Netz, Port 32400).',
    icon: 'pi pi-play',
    category: 'Medien',
    stack: 'plex',
    notice: 'Plex läuft im Host-Netz und braucht Port 32400. Ist die BetterNAS-Firewall eingeschaltet, wird dieser Port blockiert, bis du ihn dort freigibst.',
    fields: [
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      { ...dataField, pool: false, label: 'Medienordner (nur lesen)' },
      ...idFields,
      {
        key: 'PLEX_CLAIM',
        label: 'Claim-Token (optional)',
        kind: 'text',
        optional: true,
        def: () => '',
        hint: 'Ordnet den Server deinem Plex-Konto zu. Das Token von plex.tv/claim gilt nur 4 Minuten. Leer lassen, wenn du den Server später verbindest.',
      },
    ],
    device: {
      kind: 'gpu',
      label: 'Hardware-Transcoding (GPU)',
      hint: 'Reicht die Grafikeinheit an Plex durch. Hardware-Transcoding braucht Plex Pass.',
    },
    compose: `services:
  plex:
    image: lscr.io/linuxserver/plex:latest
    restart: unless-stopped
    network_mode: host
    environment:
      PUID: "\${PUID}"
      PGID: "\${PGID}"
      UMASK: "002"
      TZ: Europe/Berlin
      VERSION: docker
      PLEX_CLAIM: "\${PLEX_CLAIM}"
    volumes:
      - \${CONFIG_DIR}:/config
      - \${DATA_DIR}:/data:ro
    # @devices
`,
  },
  {
    id: 'emby',
    title: 'Emby',
    description: 'Medienserver für Filme, Serien und Musik mit Apps für viele Geräte.',
    icon: 'pi pi-play-circle',
    category: 'Medien',
    stack: 'emby',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8097' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      { ...dataField, pool: false, label: 'Medienordner (nur lesen)' },
      ...idFields,
    ],
    device: {
      kind: 'gpu',
      label: 'Hardware-Transcoding (GPU)',
      hint: 'Reicht die Grafikeinheit an Emby durch. Hardware-Transcoding braucht Emby Premiere.',
    },
    compose: `services:
  emby:
    image: lscr.io/linuxserver/emby:latest
    restart: unless-stopped
    environment:
      PUID: "\${PUID}"
      PGID: "\${PGID}"
      UMASK: "002"
      TZ: Europe/Berlin
    ports:
      - "\${PORT}:8096"
    volumes:
      - \${CONFIG_DIR}:/config
      - \${DATA_DIR}:/data:ro
    # @devices
`,
  },
  {
    id: 'audiobookshelf',
    title: 'Audiobookshelf',
    description: 'Server für Hörbücher und Podcasts mit Apps für Android und iOS.',
    icon: 'pi pi-book',
    category: 'Medien',
    stack: 'audiobookshelf',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '13378' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      { key: 'METADATA_DIR', label: 'Ordner für Metadaten', kind: 'path', def: appdata('metadata') },
      { key: 'AUDIOBOOKS_DIR', label: 'Ordner für Hörbücher', kind: 'path', def: () => '/srv/pools', hint: 'Ordner auf einem Pool, z. B. /srv/pools/<Pool>/Hoerbuecher' },
      { key: 'PODCASTS_DIR', label: 'Ordner für Podcasts', kind: 'path', pool: true, def: () => '/srv/pools', hint: 'Ordner auf einem Pool, z. B. /srv/pools/<Pool>/Podcasts' },
    ],
    compose: `services:
  audiobookshelf:
    image: ghcr.io/advplyr/audiobookshelf:latest
    restart: unless-stopped
    environment:
      TZ: Europe/Berlin
    ports:
      - "\${PORT}:80"
    volumes:
      - \${CONFIG_DIR}:/config
      - \${METADATA_DIR}:/metadata
      - \${AUDIOBOOKS_DIR}:/audiobooks
      - \${PODCASTS_DIR}:/podcasts
`,
  },
  {
    id: 'tdarr',
    title: 'Tdarr',
    description: 'Wandelt Videos automatisch in platzsparende Formate um (Server mit eingebautem Knoten). Schreibt in die Medienordner.',
    icon: 'pi pi-cog',
    category: 'Medien-Werkzeug',
    stack: 'tdarr',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8265' },
      { key: 'SERVER_PORT', label: 'Port für weitere Knoten', kind: 'port', def: () => '8266', hint: 'Über diesen Port melden sich zusätzliche Tdarr-Knoten am Server an.' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      { ...dataField, label: 'Medienordner (wird verändert)', hint: 'Tdarr ersetzt Dateien in diesem Ordner. Vorher eine Sicherung bedenken.' },
      { key: 'TRANSCODE_DIR', label: 'Ordner für Zwischendateien', kind: 'path', def: appdata('transcode'), hint: 'Auf einer lokalen, schnellen Platte, nicht auf dem Pool.' },
      ...idFields,
    ],
    device: {
      kind: 'gpu',
      label: 'Hardware-Transcoding (GPU)',
      hint: 'Reicht die Grafikeinheit an Tdarr durch. In Tdarr den Knoten für GPU-Arbeit freigeben.',
    },
    compose: `services:
  tdarr:
    image: ghcr.io/haveagitgat/tdarr:latest
    restart: unless-stopped
    environment:
      PUID: "\${PUID}"
      PGID: "\${PGID}"
      UMASK: "002"
      TZ: Europe/Berlin
      serverIP: 0.0.0.0
      serverPort: "8266"
      webUIPort: "8265"
      internalNode: "true"
      inContainer: "true"
      nodeName: InternalNode
    ports:
      - "\${PORT}:8265"
      - "\${SERVER_PORT}:8266"
    volumes:
      - \${CONFIG_DIR}/server:/app/server
      - \${CONFIG_DIR}/configs:/app/configs
      - \${CONFIG_DIR}/logs:/app/logs
      - \${DATA_DIR}:/data
      - \${TRANSCODE_DIR}:/temp
    # @devices
`,
  },
  {
    id: 'romm',
    title: 'RomM',
    description: 'Verwaltet und streamt deine Spielesammlung (ROMs) im Browser, mit Cover und Metadaten (mit MariaDB).',
    icon: 'pi pi-th-large',
    category: 'Medien',
    stack: 'romm',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8087' },
      { key: 'ROMS_DIR', label: 'Ordner der Spielesammlung', kind: 'path', pool: true, def: () => '/srv/pools', hint: 'Darin erwartet RomM den Unterordner roms mit einem Ordner je Plattform, z. B. <Ordner>/roms/snes.' },
      { key: 'ASSETS_DIR', label: 'Ordner für Spielstände und Medien', kind: 'path', def: appdata('assets') },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      { key: 'DB_DIR', label: 'Ordner für die Datenbank', kind: 'path', def: appdata('mariadb'), hint: 'Auf einer lokalen Platte, nicht auf einer Netzwerkfreigabe.' },
      { key: 'DB_PASSWORD', label: 'Datenbank-Passwort', kind: 'secret' },
      { key: 'AUTH_SECRET', label: 'Geheimer Schlüssel für Anmeldungen', kind: 'secret' },
    ],
    compose: `services:
  romm:
    image: rommapp/romm:latest
    restart: unless-stopped
    depends_on:
      romm-db:
        condition: service_healthy
    environment:
      DB_HOST: romm-db
      DB_NAME: romm
      DB_USER: romm
      DB_PASSWD: "\${DB_PASSWORD}"
      ROMM_AUTH_SECRET_KEY: "\${AUTH_SECRET}"
    ports:
      - "\${PORT}:8080"
    volumes:
      - \${ROMS_DIR}:/romm/library
      - \${ASSETS_DIR}:/romm/assets
      - \${CONFIG_DIR}:/romm/config
      - romm-resources:/romm/resources
  romm-db:
    image: mariadb:11
    restart: unless-stopped
    environment:
      MARIADB_ROOT_PASSWORD: "\${DB_PASSWORD}"
      MARIADB_DATABASE: romm
      MARIADB_USER: romm
      MARIADB_PASSWORD: "\${DB_PASSWORD}"
    healthcheck:
      test: ["CMD", "healthcheck.sh", "--connect", "--innodb_initialized"]
      start_period: 30s
      interval: 10s
      timeout: 5s
      retries: 10
    volumes:
      - \${DB_DIR}:/var/lib/mysql
volumes:
  romm-resources:
`,
  },
  {
    id: 'wikijs',
    title: 'Wiki.js',
    description: 'Wiki mit Markdown-Editor, Suche und Rechteverwaltung (mit PostgreSQL).',
    icon: 'pi pi-book',
    category: 'Wissen und Dokumente',
    stack: 'wikijs',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '3005' },
      { key: 'DB_DIR', label: 'Ordner für die Datenbank', kind: 'path', def: appdata('postgres'), hint: 'Auf einer lokalen Platte, nicht auf einer Netzwerkfreigabe.' },
      { key: 'DB_PASSWORD', label: 'Datenbank-Passwort', kind: 'secret' },
    ],
    compose: `services:
  wiki:
    image: ghcr.io/requarks/wiki:2
    restart: unless-stopped
    depends_on:
      - db
    environment:
      DB_TYPE: postgres
      DB_HOST: db
      DB_PORT: "5432"
      DB_USER: wikijs
      DB_PASS: "\${DB_PASSWORD}"
      DB_NAME: wiki
    ports:
      - "\${PORT}:3000"
  db:
    image: postgres:17
    restart: unless-stopped
    environment:
      POSTGRES_DB: wiki
      POSTGRES_USER: wikijs
      POSTGRES_PASSWORD: "\${DB_PASSWORD}"
    volumes:
      - \${DB_DIR}:/var/lib/postgresql/data
`,
  },
  {
    id: 'mealie',
    title: 'Mealie',
    description: 'Rezeptverwaltung mit Import von Webseiten, Wochenplaner und Einkaufsliste.',
    icon: 'pi pi-shopping-cart',
    category: 'Wissen und Dokumente',
    stack: 'mealie',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '9925' },
      { key: 'DATA_DIR', label: 'Ordner für Daten', kind: 'path', def: appdata('data') },
    ],
    compose: `services:
  mealie:
    image: ghcr.io/mealie-recipes/mealie:latest
    restart: unless-stopped
    environment:
      TZ: Europe/Berlin
      ALLOW_SIGNUP: "false"
      MAX_WORKERS: "1"
      WEB_CONCURRENCY: "1"
    ports:
      - "\${PORT}:9000"
    volumes:
      - \${DATA_DIR}:/app/data
`,
  },
  {
    id: 'stirling-pdf',
    title: 'Stirling-PDF',
    description: 'Werkzeugkasten für PDFs: zusammenfügen, teilen, drehen, komprimieren, Texterkennung.',
    icon: 'pi pi-file-pdf',
    category: 'Wissen und Dokumente',
    stack: 'stirling-pdf',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8091' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('configs') },
    ],
    compose: `services:
  stirling-pdf:
    image: docker.io/stirlingtools/stirling-pdf:latest
    restart: unless-stopped
    environment:
      LANGS: de_DE
    ports:
      - "\${PORT}:8080"
    volumes:
      - \${CONFIG_DIR}:/configs
`,
  },
  {
    id: 'gitea',
    title: 'Gitea',
    description: 'Eigener Git-Server mit Weboberfläche, Issues und Pull-Requests.',
    icon: 'pi pi-code',
    category: 'Entwicklung',
    stack: 'gitea',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '3006' },
      { key: 'SSH_PORT', label: 'SSH-Port für Git', kind: 'port', def: () => '2222', hint: 'Nicht 22: Dieser Port gehört dem SSH-Zugang des NAS.' },
      { key: 'DATA_DIR', label: 'Ordner für Daten', kind: 'path', def: appdata('data') },
    ],
    compose: `services:
  gitea:
    image: docker.gitea.com/gitea:latest
    restart: unless-stopped
    environment:
      USER_UID: "1000"
      USER_GID: "1000"
    ports:
      - "\${PORT}:3000"
      - "\${SSH_PORT}:22"
    volumes:
      - \${DATA_DIR}:/data
      - /etc/localtime:/etc/localtime:ro
`,
  },
  {
    id: 'portainer',
    title: 'Portainer',
    description: 'Weboberfläche zur Verwaltung von Docker-Containern, Images und Netzwerken.',
    icon: 'pi pi-server',
    category: 'Verwaltung',
    stack: 'portainer',
    notice: 'Portainer bekommt Zugriff auf den Docker-Socket. Wer Portainer steuert, hat damit Vollzugriff auf alle Container und praktisch auf das ganze NAS. Nur im Heimnetz erreichbar lassen und ein starkes Passwort setzen.',
    fields: [
      { key: 'PORT', label: 'Web-Port (HTTPS)', kind: 'port', def: () => '9443', hint: 'Portainer spricht nur HTTPS mit einem selbst signierten Zertifikat. Beim ersten Aufruf Administrator-Passwort setzen, bevor die Einrichtung abläuft.' },
      { key: 'DATA_DIR', label: 'Ordner für Daten', kind: 'path', def: appdata('data') },
    ],
    compose: `services:
  portainer:
    image: portainer/portainer-ce:lts
    restart: unless-stopped
    ports:
      - "\${PORT}:9443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - \${DATA_DIR}:/data
`,
  },
  {
    id: 'dozzle',
    title: 'Dozzle',
    description: 'Protokolle aller Container live im Browser ansehen.',
    icon: 'pi pi-list',
    category: 'Verwaltung',
    stack: 'dozzle',
    notice: 'Dozzle liest den Docker-Socket (nur lesend). Es zeigt die Protokolle aller Container, auch mit sensiblen Inhalten. Nur im Heimnetz erreichbar lassen.',
    fields: [{ key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8888' }],
    compose: `services:
  dozzle:
    image: amir20/dozzle:latest
    restart: unless-stopped
    ports:
      - "\${PORT}:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
`,
  },
  {
    id: 'heimdall',
    title: 'Heimdall',
    description: 'Startseite mit Kacheln für alle Dienste im Heimnetz.',
    icon: 'pi pi-th-large',
    category: 'Verwaltung',
    stack: 'heimdall',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8090' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      ...idFields,
    ],
    compose: `services:
  heimdall:
    image: lscr.io/linuxserver/heimdall:latest
    restart: unless-stopped
    environment:
      PUID: "\${PUID}"
      PGID: "\${PGID}"
      UMASK: "002"
      TZ: Europe/Berlin
    ports:
      - "\${PORT}:80"
    volumes:
      - \${CONFIG_DIR}:/config
`,
  },
  {
    id: 'duplicati',
    title: 'Duplicati',
    description: 'Verschlüsselte Sicherungen von Ordnern auf ein Ziel deiner Wahl (Ordner, Cloud, SFTP).',
    icon: 'pi pi-save',
    category: 'Verwaltung',
    stack: 'duplicati',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8200' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      { key: 'SOURCE_DIR', label: 'Zu sichernder Ordner (nur lesen)', kind: 'path', def: () => '/srv/pools', hint: 'Duplicati sieht ihn als /source.' },
      { key: 'BACKUP_DIR', label: 'Ordner für die Sicherungen', kind: 'path', pool: true, def: () => '/srv/pools', hint: 'Besser auf einem anderen Pool als die Originale. Duplicati sieht ihn als /backups.' },
      ...idFields,
    ],
    compose: `services:
  duplicati:
    image: lscr.io/linuxserver/duplicati:latest
    restart: unless-stopped
    environment:
      PUID: "\${PUID}"
      PGID: "\${PGID}"
      UMASK: "002"
      TZ: Europe/Berlin
    ports:
      - "\${PORT}:8200"
    volumes:
      - \${CONFIG_DIR}:/config
      - \${SOURCE_DIR}:/source:ro
      - \${BACKUP_DIR}:/backups
`,
  },
  {
    id: 'tailscale',
    title: 'Tailscale',
    description: 'Verbindet das NAS mit deinem Tailscale-Netz, damit du es von unterwegs erreichst (im Host-Netz).',
    icon: 'pi pi-globe',
    category: 'Netzwerk',
    stack: 'tailscale',
    notice: 'Tailscale läuft im Host-Netz mit erweiterten Rechten (NET_ADMIN) und braucht das Gerät /dev/net/tun. Den Auth-Key erzeugst du in der Tailscale-Verwaltung; er wird in der .env gespeichert. Die BetterNAS-Firewall blockiert den Tailscale-Port 41641/udp (dann läuft die Verbindung nur über Relays) und im Modus „Nur Heimnetz“ auch den Zugriff über das Tailscale-Netz.',
    fields: [
      { key: 'AUTH_KEY', label: 'Auth-Key von Tailscale', kind: 'text', def: () => '', hint: 'In der Tailscale-Verwaltung unter Einstellungen → Keys erzeugen (Form tskey-auth-…). Wiederverwendbar und ohne Ablauf, wenn der Dienst auch nach Neuinstallation wieder beitreten soll.' },
      { key: 'NODE_NAME', label: 'Name im Tailscale-Netz (optional)', kind: 'text', optional: true, def: () => 'betternas' },
      { key: 'STATE_DIR', label: 'Ordner für den Zustand', kind: 'path', def: appdata('state') },
    ],
    compose: `services:
  tailscale:
    image: tailscale/tailscale:latest
    restart: unless-stopped
    network_mode: host
    cap_add:
      - NET_ADMIN
    devices:
      - /dev/net/tun:/dev/net/tun
    environment:
      TS_AUTHKEY: "\${AUTH_KEY}"
      TS_HOSTNAME: "\${NODE_NAME}"
      TS_STATE_DIR: /var/lib/tailscale
      TS_USERSPACE: "false"
    volumes:
      - \${STATE_DIR}:/var/lib/tailscale
`,
  },
  {
    id: 'cloudflared',
    title: 'Cloudflare Tunnel',
    description: 'Macht Dienste im Heimnetz über einen Cloudflare-Tunnel von außen erreichbar, ohne Port-Freigabe am Router.',
    icon: 'pi pi-cloud-upload',
    category: 'Netzwerk',
    stack: 'cloudflared',
    notice: 'Der Tunnel hat keine eigene Oberfläche. Lege ihn in der Cloudflare-Verwaltung (Zero Trust → Netzwerke → Tunnels) an, kopiere den Token und trage dort unter „Öffentliche Hostnamen“ die Ziele ein, z. B. http://<NAS-IP>:8096. Was du freigibst, ist danach aus dem Internet erreichbar: schütze es mit Cloudflare Access oder der Anmeldung der App. Der Token steht im Klartext in der .env.',
    fields: [
      { key: 'TUNNEL_TOKEN', label: 'Tunnel-Token von Cloudflare', kind: 'text', def: () => '', hint: 'Beginnt mit „eyJ“ und steht in der Cloudflare-Verwaltung beim Anlegen des Tunnels hinter „cloudflared service install“.' },
    ],
    compose: `services:
  cloudflared:
    image: cloudflare/cloudflared:latest
    restart: unless-stopped
    command: tunnel --no-autoupdate run
    environment:
      TUNNEL_TOKEN: "\${TUNNEL_TOKEN}"
`,
  },
  {
    id: 'recyclarr',
    title: 'Recyclarr',
    description: 'Gleicht die Qualitätsprofile und Benennungsregeln (TRaSH-Guides) von Radarr und Sonarr automatisch ab. Läuft täglich im Hintergrund.',
    icon: 'pi pi-sync',
    category: '*arr und Wunschliste',
    stack: 'recyclarr',
    notice: 'Recyclarr hat keine Oberfläche. Beim ersten Start legt es im Einstellungsordner eine Beispieldatei recyclarr.yml an. Trage darin die Adressen und API-Schlüssel deiner Radarr- und Sonarr-Instanzen ein und wähle die gewünschten Profile (Anleitung: recyclarr.dev). Danach prüft es täglich, bei Bedarf manuell mit „docker exec recyclarr recyclarr sync“.',
    fields: [
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      ...idFields,
    ],
    compose: `services:
  recyclarr:
    image: ghcr.io/recyclarr/recyclarr:latest
    restart: unless-stopped
    user: "\${PUID}:\${PGID}"
    environment:
      TZ: Europe/Berlin
      UMASK: "002"
      CRON_SCHEDULE: "@daily"
      RECYCLARR_CREATE_CONFIG: "true"
    volumes:
      - \${CONFIG_DIR}:/config
`,
  },
  {
    id: 'nginx-proxy-manager',
    title: 'Nginx Proxy Manager',
    description: 'Reverse Proxy mit Weboberfläche: Dienste unter eigenen Namen erreichbar machen, mit Let’s-Encrypt-Zertifikaten.',
    icon: 'pi pi-directions',
    category: 'Netzwerk',
    stack: 'nginx-proxy-manager',
    notice: 'Die Ports 80 und 443 gehören auf dem NAS schon der BetterNAS-Oberfläche. Der Proxy läuft deshalb auf anderen Ports; am Router leitest du 80 und 443 von außen auf diese Ports um. Die Verwaltungsoberfläche meldet man beim ersten Start mit admin@example.com und dem Passwort changeme an (sofort ändern).',
    fields: [
      { key: 'ADMIN_PORT', label: 'Port der Verwaltungsoberfläche', kind: 'port', def: () => '8181' },
      { key: 'HTTP_PORT', label: 'Port für HTTP', kind: 'port', def: () => '8880', hint: 'Am Router von Port 80 auf diesen Port weiterleiten.' },
      { key: 'HTTPS_PORT', label: 'Port für HTTPS', kind: 'port', def: () => '8444', hint: 'Am Router von Port 443 auf diesen Port weiterleiten.' },
      { key: 'DATA_DIR', label: 'Ordner für Daten', kind: 'path', def: appdata('data') },
      { key: 'CERT_DIR', label: 'Ordner für Zertifikate', kind: 'path', def: appdata('letsencrypt') },
    ],
    compose: `services:
  nginx-proxy-manager:
    image: jc21/nginx-proxy-manager:latest
    restart: unless-stopped
    ports:
      - "\${ADMIN_PORT}:81"
      - "\${HTTP_PORT}:80"
      - "\${HTTPS_PORT}:443"
    volumes:
      - \${DATA_DIR}:/data
      - \${CERT_DIR}:/etc/letsencrypt
`,
  },
  {
    id: 'wg-easy',
    title: 'WireGuard (wg-easy)',
    description: 'Eigener WireGuard-VPN-Server mit Weboberfläche für Geräte und QR-Codes.',
    icon: 'pi pi-shield',
    category: 'Netzwerk',
    stack: 'wg-easy',
    notice: 'wg-easy braucht erweiterte Rechte (NET_ADMIN, SYS_MODULE) und leitet Datenverkehr im Netz des NAS weiter. Am Router muss der VPN-Port (UDP) auf das NAS weitergeleitet werden. Die Weboberfläche läuft unverschlüsselt (HTTP): nur im Heimnetz nutzen. Beim ersten Aufruf legst du den Administrator an. Die BetterNAS-Firewall muss den VPN-Port zulassen.',
    fields: [
      { key: 'WEB_PORT', label: 'Port der Weboberfläche', kind: 'port', def: () => '51821' },
      { key: 'VPN_PORT', label: 'VPN-Port (UDP)', kind: 'port', def: () => '51820', hint: 'Diesen UDP-Port am Router auf das NAS weiterleiten.' },
      { key: 'DATA_DIR', label: 'Ordner für Daten', kind: 'path', def: appdata('data') },
    ],
    compose: `services:
  wg-easy:
    image: ghcr.io/wg-easy/wg-easy:15
    restart: unless-stopped
    cap_add:
      - NET_ADMIN
      - SYS_MODULE
    sysctls:
      net.ipv4.ip_forward: "1"
      net.ipv4.conf.all.src_valid_mark: "1"
    environment:
      INSECURE: "true"
    ports:
      - "\${VPN_PORT}:51820/udp"
      - "\${WEB_PORT}:51821"
    volumes:
      - \${DATA_DIR}:/etc/wireguard
`,
  },
  {
    id: 'homepage',
    title: 'Homepage',
    description: 'Startseite mit Kacheln und Statusanzeigen für alle Dienste, per Textdatei konfiguriert.',
    icon: 'pi pi-home',
    category: 'Verwaltung',
    stack: 'homepage',
    notice: 'Homepage wird über YAML-Dateien im Einstellungsordner eingerichtet (services.yaml, widgets.yaml …). Beim ersten Start legt es Beispieldateien an. Der Eintrag „Erlaubte Adresse“ muss genau so lauten, wie du die Seite im Browser aufrufst, sonst zeigt Homepage nur einen Fehler.',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '3007' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      { key: 'ALLOWED_HOSTS', label: 'Erlaubte Adresse (mit Port)', kind: 'text', def: () => (hostIp() ? `${hostIp()}:3007` : ''), hint: 'Adresse samt Port, wie sie im Browser steht, z. B. 192.168.1.10:3007. Mehrere Adressen durch Komma trennen.' },
    ],
    compose: `services:
  homepage:
    image: ghcr.io/gethomepage/homepage:latest
    restart: unless-stopped
    environment:
      HOMEPAGE_ALLOWED_HOSTS: "\${ALLOWED_HOSTS}"
    ports:
      - "\${PORT}:3000"
    volumes:
      - \${CONFIG_DIR}:/app/config
`,
  },
  {
    id: 'linkding',
    title: 'Linkding',
    description: 'Schlanke Lesezeichen-Verwaltung mit Tags, Suche und Archivierung.',
    icon: 'pi pi-bookmark',
    category: 'Wissen und Dokumente',
    stack: 'linkding',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '9091' },
      { key: 'DATA_DIR', label: 'Ordner für Daten', kind: 'path', def: appdata('data') },
      { key: 'ADMIN_USER', label: 'Name des Administrators', kind: 'text', def: () => 'admin' },
      { key: 'ADMIN_PASSWORD', label: 'Passwort des Administrators', kind: 'secret' },
    ],
    compose: `services:
  linkding:
    image: sissel/linkding:latest
    restart: unless-stopped
    environment:
      LD_SUPERUSER_NAME: "\${ADMIN_USER}"
      LD_SUPERUSER_PASSWORD: "\${ADMIN_PASSWORD}"
    ports:
      - "\${PORT}:9090"
    volumes:
      - \${DATA_DIR}:/etc/linkding/data
`,
  },
  {
    id: 'freshrss',
    title: 'FreshRSS',
    description: 'Eigener RSS-Reader, der Feeds regelmäßig abruft und auch mobile Apps bedient.',
    icon: 'pi pi-megaphone',
    category: 'Wissen und Dokumente',
    stack: 'freshrss',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8093' },
      { key: 'DATA_DIR', label: 'Ordner für Daten', kind: 'path', def: appdata('data') },
      { key: 'EXT_DIR', label: 'Ordner für Erweiterungen', kind: 'path', def: appdata('extensions') },
    ],
    compose: `services:
  freshrss:
    image: freshrss/freshrss:latest
    restart: unless-stopped
    environment:
      TZ: Europe/Berlin
      CRON_MIN: "1,31"
    ports:
      - "\${PORT}:80"
    volumes:
      - \${DATA_DIR}:/var/www/FreshRSS/data
      - \${EXT_DIR}:/var/www/FreshRSS/extensions
`,
  },
  {
    id: 'metube',
    title: 'MeTube',
    description: 'Videos und Audio von YouTube und vielen anderen Seiten per Link herunterladen (yt-dlp).',
    icon: 'pi pi-download',
    category: 'Downloader',
    stack: 'metube',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8094' },
      { key: 'DOWNLOAD_DIR', label: 'Ordner für Downloads', kind: 'path', def: () => '/srv/pools', pool: true, hint: 'Ordner auf einem Pool, z. B. /srv/pools/<Pool>/Downloads' },
    ],
    compose: `services:
  metube:
    image: ghcr.io/alexta69/metube:latest
    restart: unless-stopped
    ports:
      - "\${PORT}:8081"
    volumes:
      - \${DOWNLOAD_DIR}:/downloads
`,
  },
  {
    id: 'gluetun',
    title: 'Gluetun (VPN-Client)',
    description: 'VPN-Container, über den andere Container (z. B. Downloader) ihren Datenverkehr leiten.',
    icon: 'pi pi-lock',
    category: 'Netzwerk',
    stack: 'gluetun',
    notice: 'Gluetun braucht erweiterte Rechte (NET_ADMIN) und das Gerät /dev/net/tun. Es hat keine Oberfläche und veröffentlicht von sich aus keine Ports. Andere Container nutzen es, indem du in ihrer compose.yaml `network_mode: "container:gluetun"` einträgst; ihre Web-Ports veröffentlichst du dann hier am gluetun-Dienst unter `ports:`. Anbieter und Zugangsdaten stehen bei deinem VPN-Anbieter (WireGuard-Konfiguration), die genauen Namen in der Gluetun-Wiki.',
    fields: [
      { key: 'VPN_PROVIDER', label: 'VPN-Anbieter', kind: 'text', def: () => 'mullvad', hint: 'Name wie in der Gluetun-Wiki, z. B. mullvad, protonvpn, nordvpn, airvpn oder custom.' },
      { key: 'WG_PRIVATE_KEY', label: 'WireGuard-Schlüssel (privat)', kind: 'text', def: () => '', hint: 'Der PrivateKey aus der WireGuard-Konfiguration deines Anbieters.' },
      { key: 'WG_ADDRESSES', label: 'WireGuard-Adresse', kind: 'text', def: () => '', hint: 'Das Feld Address aus der Konfiguration, z. B. 10.64.222.21/32.' },
      { key: 'COUNTRIES', label: 'Länder (optional)', kind: 'text', optional: true, def: () => '', hint: 'Servernstandort, z. B. Germany. Leer lassen für beliebige Server.' },
    ],
    compose: `services:
  gluetun:
    image: qmcgaw/gluetun:latest
    restart: unless-stopped
    cap_add:
      - NET_ADMIN
    devices:
      - /dev/net/tun:/dev/net/tun
    environment:
      VPN_SERVICE_PROVIDER: "\${VPN_PROVIDER}"
      VPN_TYPE: wireguard
      WIREGUARD_PRIVATE_KEY: "\${WG_PRIVATE_KEY}"
      WIREGUARD_ADDRESSES: "\${WG_ADDRESSES}"
      SERVER_COUNTRIES: "\${COUNTRIES}"
      TZ: Europe/Berlin
`,
  },
  {
    id: 'flaresolverr',
    title: 'FlareSolverr',
    description: 'Hilfsdienst für Prowlarr: löst Cloudflare-Abfragen, damit geschützte Indexer funktionieren.',
    icon: 'pi pi-filter',
    category: '*arr und Wunschliste',
    stack: 'flaresolverr',
    notice: 'Nach dem Start trägst du in Prowlarr unter Einstellungen → Indexer einen Proxy vom Typ FlareSolverr mit der Adresse http://<NAS-IP>:<Port> ein und gibst ihm ein Tag, das du an den betroffenen Indexern setzt.',
    fields: [{ key: 'PORT', label: 'Port', kind: 'port', def: () => '8191' }],
    compose: `services:
  flaresolverr:
    image: ghcr.io/flaresolverr/flaresolverr:latest
    restart: unless-stopped
    environment:
      LOG_LEVEL: info
      TZ: Europe/Berlin
    ports:
      - "\${PORT}:8191"
`,
  },
  {
    id: 'unpackerr',
    title: 'Unpackerr',
    description: 'Entpackt fertige Downloads für Radarr und Sonarr, wenn Releases als Archive ankommen.',
    icon: 'pi pi-box',
    category: '*arr und Wunschliste',
    stack: 'unpackerr',
    notice: 'Unpackerr hat keine Oberfläche. Es braucht denselben Datenordner wie Download-Client, Radarr und Sonarr, damit es die Archive im Ordner downloads findet. Den API-Schlüssel findest du in Radarr und Sonarr unter Einstellungen → Allgemein.',
    fields: [
      dataField,
      ...idFields,
      { key: 'RADARR_URL', label: 'Adresse von Radarr', kind: 'text', def: () => (hostIp() ? `http://${hostIp()}:7878` : ''), hint: 'z. B. http://192.168.1.10:7878' },
      { key: 'RADARR_KEY', label: 'API-Schlüssel von Radarr', kind: 'text', def: () => '' },
      { key: 'SONARR_URL', label: 'Adresse von Sonarr', kind: 'text', def: () => (hostIp() ? `http://${hostIp()}:8989` : ''), hint: 'z. B. http://192.168.1.10:8989' },
      { key: 'SONARR_KEY', label: 'API-Schlüssel von Sonarr', kind: 'text', def: () => '' },
    ],
    compose: `services:
  unpackerr:
    image: golift/unpackerr:latest
    restart: unless-stopped
    user: "\${PUID}:\${PGID}"
    environment:
      TZ: Europe/Berlin
      UMASK: "002"
      UN_RADARR_0_URL: "\${RADARR_URL}"
      UN_RADARR_0_API_KEY: "\${RADARR_KEY}"
      UN_RADARR_0_PATHS_0: /data/downloads
      UN_SONARR_0_URL: "\${SONARR_URL}"
      UN_SONARR_0_API_KEY: "\${SONARR_KEY}"
      UN_SONARR_0_PATHS_0: /data/downloads
    volumes:
      - \${DATA_DIR}:/data
`,
  },
  {
    id: 'forgejo',
    title: 'Forgejo',
    description: 'Eigener Git-Server mit Weboberfläche, Issues und Pull-Requests (Fork von Gitea).',
    icon: 'pi pi-code',
    category: 'Entwicklung',
    stack: 'forgejo',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '3008' },
      { key: 'SSH_PORT', label: 'SSH-Port für Git', kind: 'port', def: () => '2223', hint: 'Nicht 22: Dieser Port gehört dem SSH-Zugang des NAS.' },
      { key: 'DATA_DIR', label: 'Ordner für Daten', kind: 'path', def: appdata('data') },
    ],
    compose: `services:
  forgejo:
    image: codeberg.org/forgejo/forgejo:latest
    restart: unless-stopped
    environment:
      USER_UID: "1000"
      USER_GID: "1000"
    ports:
      - "\${PORT}:3000"
      - "\${SSH_PORT}:22"
    volumes:
      - \${DATA_DIR}:/data
      - /etc/localtime:/etc/localtime:ro
`,
  },
  {
    id: 'code-server',
    title: 'code-server',
    description: 'Visual Studio Code im Browser, mit Zugriff auf einen Ordner des NAS.',
    icon: 'pi pi-desktop',
    category: 'Entwicklung',
    stack: 'code-server',
    notice: 'Die Oberfläche läuft unverschlüsselt (HTTP). Einige Funktionen von VS Code (z. B. Zwischenablage, Webansichten) gehen im Browser nur über HTTPS oder localhost; dafür kann ein Reverse Proxy mit Zertifikat davor. Wer das Passwort kennt, kann auf dem NAS im Container Befehle ausführen und alle eingebundenen Ordner ändern.',
    fields: [
      { key: 'PORT', label: 'Web-Port', kind: 'port', def: () => '8445' },
      { key: 'CONFIG_DIR', label: 'Ordner für Einstellungen', kind: 'path', def: appdata('config') },
      { key: 'WORKSPACE_DIR', label: 'Arbeitsordner', kind: 'path', def: appdata('workspace'), hint: 'Der Ordner, den code-server als Projektordner öffnet.' },
      { key: 'PASSWORD', label: 'Passwort', kind: 'secret' },
      ...idFields,
    ],
    compose: `services:
  code-server:
    image: lscr.io/linuxserver/code-server:latest
    restart: unless-stopped
    environment:
      PUID: "\${PUID}"
      PGID: "\${PGID}"
      UMASK: "002"
      TZ: Europe/Berlin
      PASSWORD: "\${PASSWORD}"
      DEFAULT_WORKSPACE: /workspace
    ports:
      - "\${PORT}:8443"
    volumes:
      - \${CONFIG_DIR}:/config
      - \${WORKSPACE_DIR}:/workspace
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
      if (!/^[A-Za-z0-9_./+@-]+$/.test(v)) return 'Nur Buchstaben, Ziffern und _ . / + @ -'
      if (f.pool && v.replace(/\/+$/, '') === '/srv/pools') return 'Wähle einen Ordner in einem Pool (z. B. /srv/pools/<Pool>/Daten), nicht die Wurzel /srv/pools.'
      return ''
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

/** Dienstnamen einer compose.yaml in Reihenfolge (Schlüssel auf Ebene 2 unter `services:`). */
function serviceNames(compose: string): string[] {
  const out: string[] = []
  let inServices = false
  for (const line of compose.split('\n')) {
    if (/^services:\s*$/.test(line)) inServices = true
    else if (/^\S/.test(line)) inServices = false
    else if (inServices) {
      const m = /^ {2}([A-Za-z0-9_.-]+):\s*$/.exec(line)
      if (m) out.push(m[1])
    }
  }
  return out
}

/**
 * Container-Name eines Dienstes: ein einzelner Dienst oder der Dienst, der wie die Vorlage heißt, bekommt den
 * Stack-Namen, alle anderen `<Stack>-<Dienst>` (z. B. `cloud` und `cloud-db`).
 */
function containerNameOf(services: string[], svc: string, templateStack: string, stack: string): string {
  return services.length === 1 || svc === templateStack ? stack : `${stack}-${svc}`
}

/** Container-Namen, die ein Stack aus dieser Vorlage bekommt. */
export function containerNamesFor(t: AppTemplate, stack: string): string[] {
  const names = serviceNames(t.compose)
  return names.map((s) => containerNameOf(names, s, t.stack, stack))
}

/** Meldung, wenn einer der Container-Namen schon vergeben ist (leer = alles frei). */
export function containerNameError(t: AppTemplate, stack: string, taken: string[]): string {
  const clash = containerNamesFor(t, stack).filter((n) => taken.includes(n))
  if (!clash.length) return ''
  return `Der Container-Name ${clash.map((n) => `„${n}“`).join(', ')} ist schon vergeben. Wähle einen anderen Stack-Namen.`
}

/** Erzeugt die Stack-Dateien; der Aufrufer hat vorher alle Felder geprüft. */
export function renderTemplate(t: AppTemplate, name: string, values: Record<string, string>, devices: HostDevice[] = []): StackFiles {
  const env = t.fields.map((f) => `${f.key}=${values[f.key].trim()}`).join('\n') + '\n'
  // Der Marker `# @devices` wird durch die gewählten Geräte ersetzt oder verschwindet.
  let compose = t.compose.replace(/^[ \t]*# @devices\n/m, deviceSnippet(devices))
  // Feste Container-Namen statt `<Stack>-<Dienst>-1`; Vergabe prüft der Dialog vorher.
  const names = serviceNames(compose)
  compose = compose.replace(/^ {2}([A-Za-z0-9_.-]+):[ \t]*$/gm, (line, svc: string) => {
    return names.includes(svc) ? `${line}\n    container_name: ${containerNameOf(names, svc, t.stack, name)}` : line
  })
  return { name, compose, env }
}
