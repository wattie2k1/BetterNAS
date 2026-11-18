# Weitere App-Vorlagen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 23 neue App-Vorlagen (zusammen 34) für Docker → „Neuer Stack aus Vorlage“, mit Kategorien und Suche im Dialog.

**Architecture:** Reine Frontend-Änderung. `AppTemplate` bekommt `category` (und optional `notice`), `TemplateField` ein Flag `optional`; gemeinsame Helfer für Datenordner und `PUID`/`PGID` halten die Medien-Apps einheitlich. Der Dialog gruppiert nach Kategorie und filtert per Suchfeld. Neue Vorlagen sind Einträge in `APP_TEMPLATES`; die vorhandenen Listen-Tests werden um allgemeine Regeln erweitert, die jede neue Vorlage automatisch prüfen.

**Tech Stack:** TypeScript, Vue 3 + PrimeVue, Vitest (`cd frontend && npx vitest run`).

**Spec:** `docs/superpowers/specs/2026-11-18-app-templates-erweiterung-design.md`

## Global Constraints

- Muster der bestehenden Vorlagen: `compose` als Template-String mit `\${VARIABLE}`, jede Variable hat ein Feld und umgekehrt, Ordner-Vorgaben `appdata('…')` = `/srv/appdata/<Stack>/…`, Texte und Hinweise deutsch, Stack bleibt danach editierbar.
- Gemeinsamer Datenordner der Medien-Apps: Feld `DATA_DIR` (Vorgabe `/srv/pools`), im Container `/data`. Empfohlene Struktur steht im Hinweis: `/data/downloads`, `/data/media/{filme,serien,musik}`.
- Alle Dienste der Mediengruppe tragen `PUID` und `PGID` (Textfelder, Vorgabe `1000`), damit Hardlinks und Berechtigungen stimmen.
- Vorgabe-Ports sind über **alle** Vorlagen eindeutig (Test vorhanden). Schon belegt: 8096, 8081, 8082, 8384, 3001, 2283, 8000, 8053, 3000, 8054, 4533.
- Port 53 nur als `\${DNS_IP}:53:53` (Test vorhanden; betrifft hier keine neue Vorlage).
- Docker-Socket, `privileged` und `NET_ADMIN` nur mit `notice` an der Vorlage (neuer Test).
- Keine Änderung an Backend, Stack-Verwaltung, Update-Logik; keine neue Abhängigkeit.
- Weggelassen (nicht bauen): Watchtower, Nginx Proxy Manager, Scrutiny, Recyclarr, Frigate, Linkwarden, Readarr, Infinidysk, RomVault.
- Keine Commit-Attributionszeilen. Nicht pushen, nicht taggen.

## Review Focus

1. Suchbegriff ohne Treffer (`xyz`) und Suche mit Groß-/Kleinschreibung oder Leerzeichen am Rand: leere Liste mit Hinweis, kein Absturz; Suche nach `arr` findet alle *arr-Apps (Task 2).
2. Zweite Instanz derselben Vorlage (`existing` enthält den Stacknamen): Name wird fortlaufend vergeben, Ordner-Vorgaben folgen, Vorgabe-Ports bleiben identisch (bestehender Dialogtest, gilt für alle neuen) (Task 2).
3. Optionales Feld bleibt leer (Plex-Claim, Tailscale-Hostname): `.env` enthält `KEY=` mit leerem Wert, Eingabe-Prüfung meldet keinen Fehler (Task 1).
4. Zwei Medien-Vorlagen im selben Datenordner: beide binden `\${DATA_DIR}:/data` und tragen dieselben `PUID`/`PGID`-Vorgaben (Task 1, Regel gilt für alle).
5. Vorlage mit mehreren Containern (RomM, Wiki.js): Geheimnisse (Datenbankpasswort) kommen aus einem `secret`-Feld und tauchen nirgends im Klartext in der `compose` auf (Task 6).

---

### Task 1: Typen, Helfer und Kategorien der bestehenden Vorlagen

**Files:**
- Modify: `frontend/src/utils/appTemplates.ts`
- Test: `frontend/src/__tests__/app-templates.test.ts`

**Interfaces:**
- Produces (alle in `appTemplates.ts`, von allen Folgetasks benutzt):
  - `export type TemplateCategory = 'Medien' | 'Downloader' | '*arr und Wunschliste' | 'Verwaltung' | 'Netzwerk' | 'Wissen und Dokumente' | 'Entwicklung' | 'Medien-Werkzeug'`
  - `export const TEMPLATE_CATEGORIES: TemplateCategory[]` (Anzeigereihenfolge wie oben)
  - `TemplateField.optional?: boolean`; `AppTemplate.category: TemplateCategory`; `AppTemplate.notice?: string` (Warnhinweis im Formular)
  - `export const dataField: TemplateField` (`DATA_DIR`, kind `path`, Vorgabe `/srv/pools`, Hinweis mit empfohlener Ordnerstruktur)
  - `export const idFields: TemplateField[]` (`PUID`, `PGID`, kind `text`, Vorgabe `'1000'`, gemeinsamer Hinweis)
  - `export function groupTemplates(list: AppTemplate[], query: string): { category: TemplateCategory; items: AppTemplate[] }[]`

- [ ] **Step 1: Tests schreiben (schlagen fehl)** in `app-templates.test.ts`:
  - `jede Vorlage hat eine gültige Kategorie` (`TEMPLATE_CATEGORIES` enthält `t.category`).
  - `Vorlagen mit Docker-Socket, privileged oder NET_ADMIN haben einen Hinweis`: für jede Vorlage, deren `compose` `docker.sock`, `privileged: true` oder `NET_ADMIN` enthält, ist `t.notice` nicht leer.
  - `Vorlagen mit PUID haben auch PGID und binden den Datenordner`: hat eine Vorlage ein Feld `PUID`, dann auch `PGID` und `DATA_DIR`, und die `compose` enthält `\${DATA_DIR}:/data` (Ausnahme: Vorlagen ohne Medienordner tragen kein `PUID`).
  - `optionale Felder sind leer erlaubt`: `fieldError({key:'X',label:'',kind:'text',optional:true}, '')` ist `''`, ohne `optional` `'Pflichtfeld'`; `renderTemplate` mit leerem optionalem Wert liefert `X=` in der `.env`.
  - `groupTemplates`: ohne Suchbegriff alle Vorlagen, nach `TEMPLATE_CATEGORIES` sortiert, leere Kategorien fehlen; `'  JELLY '` findet Jellyfin (Titel, `id` und Beschreibung, ohne Groß-/Kleinschreibung, Rand-Leerzeichen ignoriert); `'xyz'` liefert `[]`.
- [ ] **Step 2: Test laufen lassen:** `cd frontend && npx vitest run src/__tests__/app-templates.test.ts` → FAIL (Typen/Funktion fehlen).
- [ ] **Step 3: Implementieren** in `appTemplates.ts`: Typen und Helfer wie oben; `fieldError` liefert bei leerem Wert `''`, wenn `f.optional`; `renderTemplate` bleibt (leerer Wert → `KEY=`). Bestehende 11 Vorlagen bekommen `category`: jellyfin, immich, navidrome → `Medien`; nextcloud, paperless, syncthing → `Wissen und Dokumente`; homeassistant, vaultwarden, uptime-kuma → `Verwaltung`; pihole, adguardhome → `Netzwerk`.
- [ ] **Step 4: Alle Frontend-Tests:** `cd frontend && npx vitest run` → PASS, danach `npx vue-tsc --noEmit` → keine Fehler.
- [ ] **Step 5: Commit** `feat(docker): Kategorien und gemeinsame Helfer für App-Vorlagen`.

---

### Task 2: Dialog mit Kategorien, Suche und Hinweis

**Files:**
- Modify: `frontend/src/components/docker/TemplateDialog.vue`
- Test: `frontend/src/__tests__/app-templates.test.ts` (Block `TemplateDialog`)

**Interfaces:**
- Consumes: `groupTemplates`, `AppTemplate.notice` aus Task 1.
- Produces: `data-test`-Attribute `template-search` (Suchfeld), `template-group-<Kategorie>` (Überschrift je Gruppe), `template-empty` (Meldung ohne Treffer), `tpl-notice` (Warnhinweis im Formular).

- [ ] **Step 1: Tests schreiben (schlagen fehl):** Dialog zeigt je Kategorie eine Überschrift; Eingabe `jelly` in `template-search` lässt nur `template-jellyfin` übrig; Eingabe `xyz` zeigt `template-empty` und keine Kacheln; Zurück aus dem Formular behält den Suchbegriff nicht (Suchfeld wird beim Öffnen geleert); Vorlage mit `notice` zeigt `tpl-notice` mit dem Text, eine ohne zeigt es nicht (Testvorlage per `APP_TEMPLATES.push`/Spy oder erste echte Vorlage mit Hinweis aus Task 7 – bis dahin lokale Testvorlage in der Testdatei).
- [ ] **Step 2: Test laufen lassen** → FAIL.
- [ ] **Step 3: Implementieren:** Suchfeld (`InputText`, `data-test="template-search"`, Platzhalter „Vorlage suchen“) über der Liste; Liste wird aus `groupTemplates(APP_TEMPLATES, query)` gerendert (Überschrift + bestehendes `.tpl-grid`); `query` wird beim Öffnen (`watch visible`) geleert; bei Treffern leer: Text „Keine Vorlage gefunden.“ (`template-empty`); im Formular `Message severity="warn"` mit `picked.notice` (`tpl-notice`), vor dem bestehenden Info-Hinweis. Bestehendes Styling und Tokens (`--bn-*`) wiederverwenden.
- [ ] **Step 4: Tests laufen lassen:** `cd frontend && npx vitest run` → PASS; `npx vue-tsc --noEmit` ohne Fehler.
- [ ] **Step 5: Commit** `feat(docker): Vorlagen-Dialog mit Kategorien und Suche`.

---

### Task 3: Downloader (qBittorrent, SABnzbd, NZBGet)

**Files:**
- Modify: `frontend/src/utils/appTemplates.ts`
- Test: bestehende Listen-Tests (kein neuer Test nötig; ein Test pro Vorlage in `app-templates.test.ts`: `rendert <id>` mit `renderTemplate` und prüft Port-Zeile und `/data`-Mount)

**Interfaces:**
- Consumes: `dataField`, `idFields`, `appdata`, `MEDIA_HINT`-Stil aus Task 1.
- Produces: Vorlagen-IDs `qbittorrent`, `sabnzbd`, `nzbget` (Kategorie `Downloader`, Icon `pi pi-download`).

Vorlagen (Image `lscr.io/linuxserver/<name>:latest`; Tag und Umgebungsvariablen gegen die Projektdokumentation prüfen):

| id | Felder (Vorgabe) | Container-Port → Feld | Besonderheit |
|----|------------------|----------------------|--------------|
| `qbittorrent` | `PORT` 8083 (Web), `TORRENT_PORT` 6881, `CONFIG_DIR`, `DATA_DIR`, `PUID`, `PGID` | 8080 → `PORT`; 6881 TCP+UDP → `TORRENT_PORT` | `WEBUI_PORT: 8080`; Hinweis: Das Anfangspasswort steht im Protokoll des Containers |
| `sabnzbd` | `PORT` 8085, `CONFIG_DIR`, `DATA_DIR`, `PUID`, `PGID` | 8080 → `PORT` | Hinweis: Zugangsdaten des Usenet-Anbieters in der Weboberfläche eintragen |
| `nzbget` | `PORT` 6789, `CONFIG_DIR`, `DATA_DIR`, `PUID`, `PGID` | 6789 → `PORT` | Hinweis: Standard-Zugang `nzbget` / `tegbzn6789` sofort ändern |

- [ ] **Step 1: Test schreiben:** für jede ID `renderTemplate(t, t.stack, defaultValues(t, t.stack))` enthält `.compose` `\${DATA_DIR}:/data`, `PUID`, `PGID` und die Port-Zeile aus der Tabelle; `.env` enthält `PORT=<Vorgabe>`.
- [ ] **Step 2: Laufen lassen** → FAIL (Vorlagen fehlen).
- [ ] **Step 3: Drei Einträge in `APP_TEMPLATES` ergänzen** (nach Muster von Jellyfin; `environment: PUID/PGID/TZ: Europe/Berlin`; Konfigordner nach `/config`, Datenordner nach `/data`).
- [ ] **Step 4: Alle Frontend-Tests** (`npx vitest run`) → PASS, inklusive der Listen-Tests (Ports eindeutig, Variablen vollständig).
- [ ] **Step 5: Commit** `feat(docker): Vorlagen qBittorrent, SABnzbd und NZBGet`.

---

### Task 4: *arr und Wunschliste (Prowlarr, Radarr, Sonarr, Lidarr, Bazarr, Seerr)

**Files:**
- Modify: `frontend/src/utils/appTemplates.ts`
- Test: `frontend/src/__tests__/app-templates.test.ts`

**Interfaces:**
- Consumes: Helfer aus Task 1.
- Produces: IDs `prowlarr`, `radarr`, `sonarr`, `lidarr`, `bazarr`, `seerr` (Kategorie `*arr und Wunschliste`; Icons: `pi pi-search`, `pi pi-video`, `pi pi-desktop`, `pi pi-headphones`, `pi pi-comments`, `pi pi-star`).

| id | Image | Vorgabe-Port → Container | Felder außer Port/`CONFIG_DIR` |
|----|-------|--------------------------|-------------------------------|
| `prowlarr` | `lscr.io/linuxserver/prowlarr` | 9696 → 9696 | `PUID`, `PGID` (kein `DATA_DIR`) |
| `radarr` | `lscr.io/linuxserver/radarr` | 7878 → 7878 | `DATA_DIR`, `PUID`, `PGID` |
| `sonarr` | `lscr.io/linuxserver/sonarr` | 8989 → 8989 | `DATA_DIR`, `PUID`, `PGID` |
| `lidarr` | `lscr.io/linuxserver/lidarr` | 8686 → 8686 | `DATA_DIR`, `PUID`, `PGID` |
| `bazarr` | `lscr.io/linuxserver/bazarr` | 6767 → 6767 | `DATA_DIR`, `PUID`, `PGID` |
| `seerr` | `ghcr.io/seerr-team/seerr` (Image und Datenordner `/app/config` prüfen) | 5055 → 5055 | nur `CONFIG_DIR` (Besitzer 1000, Hinweis im Feld) |

Beschreibungen nennen die Rolle in einem Satz und enthalten bei Prowlarr, Radarr, Sonarr, Lidarr und Bazarr das Wort „*arr“ (z. B. Prowlarr: „Verwaltet Indexer für alle *arr-Apps“), damit die Suche nach `arr` sie findet; ein Test in `groupTemplates` prüft, dass `arr` mindestens diese fünf liefert. Radarr/Sonarr/Lidarr-Hinweis am `DATA_DIR`: Download-Client dieselben `/data`-Pfade nutzen lassen (Verweis auf qBittorrent/SABnzbd). Hinweis bei Prowlarr: Apps untereinander verbindet man in den Einstellungen mit URL und API-Key.

- [ ] **Step 1: Test:** je ID rendert ohne Fehler, enthält die Port-Zeile aus der Tabelle; Vorlagen mit `DATA_DIR` binden `\${DATA_DIR}:/data`.
- [ ] **Step 2: Laufen lassen** → FAIL.
- [ ] **Step 3: Sechs Einträge ergänzen.**
- [ ] **Step 4: `npx vitest run`** → PASS.
- [ ] **Step 5: Commit** `feat(docker): *arr-Vorlagen Prowlarr, Radarr, Sonarr, Lidarr, Bazarr und Seerr`.

---

### Task 5: Medienserver und -werkzeug (Plex, Emby, Audiobookshelf, Tdarr)

**Files:**
- Modify: `frontend/src/utils/appTemplates.ts`
- Test: `frontend/src/__tests__/app-templates.test.ts`, `frontend/src/__tests__/docker-devices.test.ts` (GPU-Marker)

**Interfaces:**
- Consumes: Helfer aus Task 1, `TemplateDevice` (`kind: 'gpu'`), Marker `# @devices`.
- Produces: IDs `plex`, `emby`, `audiobookshelf` (Kategorie `Medien`, Icons `pi pi-play`, `pi pi-play-circle`, `pi pi-book`), `tdarr` (Kategorie `Medien-Werkzeug`, Icon `pi pi-cog`).

| id | Image | Netz/Port | Felder | Besonderheit |
|----|-------|-----------|--------|--------------|
| `plex` | `lscr.io/linuxserver/plex` | `network_mode: host` (Plex-Port 32400 fest, **kein** Portfeld) | `CONFIG_DIR`, `DATA_DIR` (nur lesen: `\${DATA_DIR}:/data:ro`), `PUID`, `PGID`, `PLEX_CLAIM` (`optional`, `text`) | GPU-Gerät (`device`, Marker `# @devices`); Hinweis am Claim-Feld: Token von plex.tv/claim, nur 4 Minuten gültig, leer lassen und Server später per Browser verbinden |
| `emby` | `lscr.io/linuxserver/emby` | `PORT` 8097 → 8096 | `CONFIG_DIR`, `DATA_DIR` (`:ro`), `PUID`, `PGID` | GPU-Gerät wie Jellyfin |
| `audiobookshelf` | `ghcr.io/advplyr/audiobookshelf` | `PORT` 13378 → 80 | `CONFIG_DIR`, `METADATA_DIR`, `AUDIOBOOKS_DIR`, `PODCASTS_DIR` (alle `path`) | Ordner `/config`, `/metadata`, `/audiobooks`, `/podcasts` |
| `tdarr` | `ghcr.io/haveagitgat/tdarr` (Server mit internem Knoten) | `PORT` 8265 → 8265, `SERVER_PORT` 8266 → 8266 | `CONFIG_DIR`, `DATA_DIR` (Medien lesen und schreiben), `TRANSCODE_DIR` (`path`, Vorgabe `appdata('transcode')`), `PUID`, `PGID` | `internalNode=true`; GPU-Gerät; Hinweis: Tdarr schreibt in die Medienordner, vorher Sicherung bedenken |

- [ ] **Step 1: Tests:** (a) `plex` enthält `network_mode: host`, **keine** Zeile `ports:`; `PLEX_CLAIM` ist `optional`; `renderTemplate` ohne Claim liefert `PLEX_CLAIM=`. (b) `plex`, `emby`, `tdarr` haben `device.kind === 'gpu'` und der Marker `# @devices` steht in ihrer `compose` (Muster aus `docker-devices.test.ts`). (c) Vorgabe-Ports bleiben eindeutig (bestehender Test).
- [ ] **Step 2: Laufen lassen** → FAIL.
- [ ] **Step 3: Vier Einträge ergänzen.**
- [ ] **Step 4: `npx vitest run`** → PASS.
- [ ] **Step 5: Commit** `feat(docker): Vorlagen Plex, Emby, Audiobookshelf und Tdarr`.

---

### Task 6: Apps mit Datenbank (RomM, Wiki.js, Gitea, Mealie, Stirling-PDF)

**Files:**
- Modify: `frontend/src/utils/appTemplates.ts`
- Test: `frontend/src/__tests__/app-templates.test.ts`

**Interfaces:**
- Consumes: Helfer aus Task 1; Muster der Datenbank-Container aus Immich/Paperless (Passwort als `secret`-Feld, Datenordner auf lokaler Platte).
- Produces: IDs `romm` (Medien, `pi pi-th-large`), `wikijs` (Wissen und Dokumente, `pi pi-book`), `mealie` (Wissen und Dokumente, `pi pi-shopping-cart`), `stirling-pdf` (Wissen und Dokumente, `pi pi-file-pdf`), `gitea` (Entwicklung, `pi pi-code`).

| id | Container | Felder (Vorgabe) | Besonderheit |
|----|-----------|------------------|--------------|
| `romm` | `rommapp/romm` + `mariadb` | `PORT` 8087 → 8080, `ROMS_DIR` (`path`, Vorgabe `/srv/pools`, Hinweis: Struktur `roms/<Plattform>/`), `ASSETS_DIR`, `CONFIG_DIR`, `DB_DIR` (lokal), `DB_PASSWORD` (`secret`), `AUTH_SECRET` (`secret`; prüfen, ob RomM die erzeugten 24 Zeichen akzeptiert, sonst das Feld auf `randomSecret(48)` vorbelegen) | Hinweis: Metadaten-Anbieter (IGDB, ScreenScraper) optional in `.env` ergänzen |
| `wikijs` | `ghcr.io/requarks/wiki:2` + `postgres:17` | `PORT` 3005 → 3000, `DB_DIR` (lokal), `DB_PASSWORD` (`secret`) | Passwort ausschließlich über `\${DB_PASSWORD}` |
| `mealie` | `ghcr.io/mealie-recipes/mealie` | `PORT` 9925 → 9000, `DATA_DIR` | SQLite, kein zweiter Container |
| `stirling-pdf` | `docker.io/stirlingtools/stirling-pdf` | `PORT` 8091 → 8080, `CONFIG_DIR` | kein Datenordner nötig |
| `gitea` | `docker.gitea.com/gitea` | `PORT` 3006 → 3000, `SSH_PORT` 2222 → 22, `DATA_DIR` | SQLite; Hinweis: SSH-Port 2222, weil 22 der BetterNAS-SSH gehört |

- [ ] **Step 1: Tests:** (a) `romm` und `wikijs` enthalten die Datenbank-Container und **kein** Passwort im Klartext: die `compose` enthält `\${DB_PASSWORD}` und keine feste Zeichenfolge hinter `PASSWORD`/`SECRET` ohne `\${`. (b) `gitea`: Vorgabe `SSH_PORT` ≠ 22. (c) alle fünf rendern, Ports laut Tabelle.
- [ ] **Step 2: Laufen lassen** → FAIL.
- [ ] **Step 3: Fünf Einträge ergänzen** (Healthchecks nur, wo das Projekt sie vorsieht; `depends_on` für Datenbanken wie bei Paperless).
- [ ] **Step 4: `npx vitest run`** → PASS.
- [ ] **Step 5: Commit** `feat(docker): Vorlagen RomM, Wiki.js, Mealie, Stirling-PDF und Gitea`.

---

### Task 7: Verwaltung und Netzwerk (Portainer, Dozzle, Heimdall, Duplicati, Tailscale (Filebrowser beim Abschlussreview entfernt: Projekt archiviert))

**Files:**
- Modify: `frontend/src/utils/appTemplates.ts`
- Test: `frontend/src/__tests__/app-templates.test.ts`

**Interfaces:**
- Consumes: `AppTemplate.notice` (Task 1), Dialog-Hinweis (Task 2).
- Produces: IDs `portainer`, `dozzle`, `filebrowser`, `heimdall`, `duplicati` (Kategorie `Verwaltung`; Icons `pi pi-server`, `pi pi-list`, `pi pi-folder`, `pi pi-th-large`, `pi pi-save`), `tailscale` (Kategorie `Netzwerk`, `pi pi-globe`).

| id | Image | Port → Container | Felder | Hinweis (`notice`) |
|----|-------|------------------|--------|--------------------|
| `portainer` | `portainer/portainer-ce` | `PORT` 9443 → 9443 (HTTPS) | `DATA_DIR` | **ja:** Zugriff auf den Docker-Socket (`/var/run/docker.sock`) bedeutet Vollzugriff auf das NAS |
| `dozzle` | `amir20/dozzle` | `PORT` 8888 → 8080 | keine außer `PORT` | **ja:** Docker-Socket nur lesend (`:ro`), zeigt aber alle Protokolle |
| `filebrowser` | `filebrowser/filebrowser` | `PORT` 8089 → 80 | `DATA_DIR` (Vorgabe `/srv/pools`, im Container `/srv`), `CONFIG_DIR` | Hinweis nur im Feld: Das Anfangspasswort steht im Protokoll |
| `heimdall` | `lscr.io/linuxserver/heimdall` | `PORT` 8090 → 80 | `CONFIG_DIR`, `PUID`, `PGID` | – |
| `duplicati` | `lscr.io/linuxserver/duplicati` | `PORT` 8200 → 8200 | `CONFIG_DIR`, `SOURCE_DIR` (nur lesen, Vorgabe `/srv/pools`, im Container `/source`), `BACKUP_DIR` (`path`, im Container `/backups`), `PUID`, `PGID` | – |
| `tailscale` | `tailscale/tailscale` | `network_mode: host`, **kein** Portfeld | `AUTH_KEY` (`text`, Pflicht, Hinweis: Schlüssel aus der Tailscale-Verwaltung), `HOSTNAME` (`text`, `optional`, Vorgabe `betternas`), `STATE_DIR` | **ja:** `NET_ADMIN` und `/dev/net/tun`; `TS_STATE_DIR=/var/lib/tailscale` |

- [ ] **Step 1: Tests:** (a) `portainer`, `dozzle`, `tailscale` haben `notice`; (b) `dozzle` bindet den Socket mit `:ro`; (c) `tailscale` enthält `NET_ADMIN`, `/dev/net/tun`, `network_mode: host` und kein `ports:`; `AUTH_KEY` ist kein `secret` (wird nicht zufällig vorbelegt) und hat Vorgabe `''`, die Pflichtprüfung meldet „Pflichtfeld“; (d) alle sechs rendern.
- [ ] **Step 2: Laufen lassen** → FAIL.
- [ ] **Step 3: Sechs Einträge ergänzen.** Wo ein Feld keine Vorgabe hat (`AUTH_KEY`), lässt `defaultValues` den Wert leer; `fieldError` für `text` erlaubt Schlüssel der Form `tskey-auth-…`.
- [ ] **Step 4: Alle Frontend-Tests** (`npx vitest run`) → PASS; `npx vue-tsc --noEmit`.
- [ ] **Step 5: Commit** `feat(docker): Vorlagen Portainer, Dozzle, Filebrowser, Heimdall, Duplicati und Tailscale`.

---

### Task 8: Echtprüfung auf dem Testgerät und Ausrollen

**Files:**
- Create: Skript im Scratchpad (nicht ins Repo): rendert jede neue Vorlage mit Standardwerten per vite-node in ein Temp-Verzeichnis des Testgeräts, `docker compose config -q`, dann Stichproben-Start.
- Modify: `memory`-Notiz nach Abschluss (Stand, Vorlagenzahl) – nicht im Repo.

**Interfaces:**
- Consumes: alle Vorlagen aus Tasks 3–7, `renderTemplate`, `defaultValues`.

- [ ] **Step 1: Alle 23 neuen Vorlagen** rendern und auf dem Testgerät (`ssh betternas-dev`) mit `docker compose -p tpltest-<id> config -q` prüfen → keine Fehler. Ordner aus den Standardwerten vorher anlegen.
- [ ] **Step 2: Stichproben starten** (`up -d`, HTTP-Abfrage, danach `down -v --rmi all` und Ordner entfernen): je eine pro Gruppe – `qbittorrent`, `radarr`, `emby`, `romm` (zwei Container), `wikijs`, `portainer`, `gitea`, `heimdall` – und `plex` (Host-Netz, nur Port 32400 antwortet). `tailscale` nur `config -q` (braucht Schlüssel). Nach jedem Test Aufräumen prüfen (`docker ps -a`, `docker volume ls`).
- [ ] **Step 3: Gefundene Fehler** (falsche Variablen, Image-Tags, Ports, Umgebungsvariablen) in `appTemplates.ts` korrigieren; betroffene Tests anpassen; `npx vitest run` → PASS.
- [ ] **Step 4: Version hochzählen** (`0.7.0+dev33`), bauen und ausrollen wie bisher (`make build web deb deploy VERSION=…`); API startet (`systemctl is-active betternas-api`).
- [ ] **Step 5: Commit** `fix(docker): Korrekturen der Vorlagen nach Prüfung auf dem Testgerät` (nur wenn Step 3 etwas geändert hat). **Nicht pushen**.
