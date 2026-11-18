// App-Vorlagen für Docker-Stacks: Eingaben (Ports, Pfade, Geheimnisse) landen in der .env,
// die compose.yaml verweist darauf per ${VARIABLE}. Der Stack bleibt danach frei editierbar.
import { STACK_NAME_RE, type HostDevice, type StackFiles } from '@/api/docker'
import { deviceSnippet } from '@/utils/deviceSnippet'

export type FieldKind = 'text' | 'port' | 'path' | 'secret'

export interface TemplateField {
  key: string
  label: string
  kind: FieldKind
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
  /** Vorschlag für den Stacknamen. */
  stack: string
  fields: TemplateField[]
  device?: TemplateDevice
  compose: string
}

const appdata = (sub: string) => (s: string) => `/srv/appdata/${s}/${sub}`
const MEDIA_HINT = 'Ordner auf einem Pool, z. B. /srv/pools/<Pool>/Medien'

export const APP_TEMPLATES: AppTemplate[] = [
  {
    id: 'jellyfin',
    title: 'Jellyfin',
    description: 'Medienserver für Filme, Serien und Musik.',
    icon: 'pi pi-video',
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
    stack: 'homeassistant',
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
]

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
  if (!v) return 'Pflichtfeld'
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
