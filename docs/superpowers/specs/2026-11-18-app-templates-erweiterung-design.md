# Weitere App-Vorlagen für Docker-Stacks (Design)

Stand: 2026-11-18 · Status: Entwurf zur Freigabe (kurze Fassung)

## Intention

Die Vorlagen unter Docker → „Neuer Stack aus Vorlage“ sollen die gängigen Heimserver-Apps abdecken, damit niemand
compose-Dateien von Hand schreiben muss. Gewünscht sind vor allem Downloader und die *arr-Suite, dazu Medienserver,
Verwaltungswerkzeuge und was sonst zu einem NAS passt. Heute gibt es 11 Vorlagen (`frontend/src/utils/appTemplates.ts`).

## Umfang

Neu, in Gruppen (Gruppenname = Kategorie im Dialog):

- **Downloader:** qBittorrent, SABnzbd, NZBGet
- ***arr und Wunschliste:** Prowlarr, Radarr, Sonarr, Lidarr, Bazarr, Seerr
- **Medien:** Plex (mit GPU-Option), Emby (mit GPU-Option), Audiobookshelf, RomM (mit MariaDB)
- **Verwaltung:** Portainer, Dozzle, Filebrowser, Heimdall, Duplicati
- **Netzwerk:** Tailscale
- **Wissen und Dokumente:** Wiki.js (mit Postgres), Mealie, Stirling-PDF
- **Entwicklung:** Gitea
- **Medien-Werkzeug:** Tdarr (Server mit eingebautem Knoten, mit GPU-Option)

Das sind 23 neue Vorlagen (zusammen 34). Die bestehenden 11 bekommen nachträglich eine Kategorie.
Vaultwarden gibt es schon.

## Ziele

- Jede Vorlage folgt dem bestehenden Muster: `compose.yaml` mit `${VARIABLEN}`, Eingabefelder, Vorgabeordner unter
  `/srv/appdata/<Stack>`, deutsche Beschriftungen und Hinweise. Der Stack bleibt danach frei editierbar.
- Downloader, *arr, Plex und Emby sind **einzelne Stacks mit gemeinsamem Datenordner**: alle fragen nach demselben
  Ordner (Vorgabe `/srv/pools`) und binden ihn als `/data` ein. Nur so funktionieren Hardlinks und Verschieben ohne
  Kopieren. Die Hinweise nennen die empfohlene Struktur (`/data/downloads`, `/data/media/{filme,serie,musik}`).
- Der Vorlagen-Dialog bleibt bei 34 Einträgen bedienbar: Kategorien als Überschriften und ein Suchfeld.

## Nicht-Ziele

- Kein Komplettpaket („alles in einem Stack“); das kann später als eigene Vorlage folgen.
- Keine automatische Verdrahtung der Apps untereinander (URLs und API-Keys trägt der Nutzer ein).
- Keine Änderung an Backend, Stack-Verwaltung oder Update-Logik.

## Anti-Ziele

- Keine Vorlage, die mit etwas kollidiert, das BetterNAS selbst bereitstellt (siehe „Bewusst weggelassen“).
- Kein Docker-Socket oder `privileged` ohne ausdrücklichen Hinweis im Formular.
- Keine Vorlage, deren Image oder Konfiguration ich nicht begründen kann.

## Bewusst weggelassen

- **Watchtower:** BetterNAS aktualisiert Stacks selbst mit Gesundheitsprüfung und Rollback; zwei Werkzeuge würden
  dieselben Container neu starten. Das Original-Projekt ist zudem archiviert.
- **Nginx Proxy Manager:** BetterNAS hat einen Reverse-Proxy mit Caddy und Let's Encrypt; beide wollen Port 80/443.
- **Scrutiny:** SMART-Überwachung ist schon eingebaut (Monitor, smartmontools).
- **Recyclarr, Frigate, Linkwarden:** Recyclarr hat keine Oberfläche und braucht eine eigene Konfigurationsdatei, Frigate
  und Linkwarden brauchen zu viel Zusatzkonfiguration bzw. mehrere Dienste für eine Formular-Vorlage.
- **Infinidysk, RomVault:** vom Nutzer gestrichen (nicht zuordenbar bzw. kein Linux-Image).
- **Readarr:** vom Projekt eingestellt.

## Randbedingungen

- Die bestehenden Tests bleiben maßgeblich: für jede `${VARIABLE}` gibt es ein Feld und umgekehrt, Namen und
  Stacknamen sind eindeutig, **Vorgabe-Ports sind über alle Vorlagen eindeutig**, Port 53 nur mit `${DNS_IP}`.
- Plex und Tailscale laufen im Host-Netz; ihre festen Ports (32400, Tailscale) müssen frei von Vorgabe-Ports anderer
  Vorlagen sein.
- Tailscale braucht `/dev/net/tun` und `NET_ADMIN`, Portainer und Dozzle den Docker-Socket: Hinweise im Formular.
- Geheimnisse, die der Nutzer von außen bekommt (Tailscale-Auth-Key, Plex-Claim-Token), sind Textfelder, keine
  erzeugten Zufallswerte. Zeichen müssen zur Feldprüfung passen.

## Ansätze

- **A, einzelne Vorlagen mit gemeinsamem Datenordner** *(empfohlen und von dir übernommen, weil du nicht widersprochen hast)*:
  jede App ein Stack, einheitliche Ordner. Installieren, Aktualisieren, Rollback und Limits je Dienst.
- **B, zusätzlich ein Komplettpaket:** bequemer für Einsteiger, aber groß und starr. Später als Ergänzung möglich.
- **C, Vorlagen aus Dateien nachladen** (statt im Code): flexibel, aber ein größerer Umbau und für diesen Umfang nicht nötig.

## Meine Entscheidungen (zum Gegenprüfen)

- Kategorien und Suche im Dialog (kleine Erweiterung von `AppTemplate` um `category`, Dialog gruppiert und filtert).
- Images: `lscr.io/linuxserver/*` für Downloader, *arr, Heimdall, Emby und Plex-Alternativen, wo es sie gibt; sonst die
  offiziellen Images. Immer `latest` bzw. das vom Projekt empfohlene Tag, wie bei den bestehenden Vorlagen.
- Alle Dienste der Mediengruppe laufen mit derselben Benutzer- und Gruppen-ID (`PUID`/`PGID`), damit Hardlinks und
  Berechtigungen stimmen. Der Wert wird als Feld vorbelegt, passend zu dem, was die Samba-Freigaben der Pools verwenden
  (vom Bauenden zu prüfen).
- Seerr statt Overseerr (Nachfolgeprojekt), Gitea statt Forgejo (bekannter; Forgejo wäre ein Austausch des Images).
- Icons aus PrimeIcons wie bisher, je Kategorie sinnvoll gewählt.

## Dem Bauenden überlassen

- Genaue Images, Tags, Ports, Umgebungsvariablen und Healthchecks je App (gegen die jeweilige Projektdokumentation prüfen).
- Vorgabe-Ports (eindeutig über alle Vorlagen), Reihenfolge innerhalb einer Kategorie, genaue Hinweistexte.
- Testabdeckung: bestehende Prüfungen reichen für Formale; zusätzlich Test für Kategorien/Suche im Dialog.

## Offen

Nichts.
