# Design: appdata und dockerdata trennen, Speicherorte vor der Docker-Nutzung festlegen

Stand: 2026-11-20 (überarbeitet: Einrichtung vor der ersten Nutzung). Umfang: kleine bis mittlere Änderung.

## Absicht

Bisher liegt alles, was Docker-Container ablegen, unter `/srv/appdata` (Systemplatte oder Restpartition des
Installers). Die Einstellungen der Apps (z. B. Emby) sind klein, Datenbanken, Caches und Daten werden groß. Beide
Bereiche sollen auf einer Platte oder einem Pool liegen können, nicht zwingend auf der Systemplatte.

Nach einer frischen Installation gibt es deshalb **keine Vorgabe**: Der Nutzer bereitet in der Oberfläche zuerst
eine Platte oder einen Pool vor und legt dann die beiden Speicherorte fest. Erst danach ist Docker nutzbar.

## Ziele

- Zwei Wurzeln: **appdata** (Einstellungen der Apps, Stacks, Reverse-Proxy, Sicherungen) und **dockerdata**
  (alle übrigen App-Ordner, außerdem der Docker-`data-root` für Images und Container).
- Beide sind in den Einstellungen festlegbar und später änderbar.
- Auf einer frischen Installation ist Docker gesperrt, bis beide gesetzt sind. Die Docker-Ansichten leiten zur
  Einrichtung.
- Vorhandene Installationen (z. B. das Testgerät) laufen unverändert weiter und müssen nichts einrichten.

## Nicht-Ziele

- Kein automatisches Verschieben vorhandener Daten und kein Umschreiben vorhandener `compose.yaml`.
  Eine Änderung gilt nur für Neues, bestehende Stacks behalten ihre Pfade.
- Keine Änderung an `/srv/vms`.
- Der Installer legt weiter die Restpartition an (siehe Offene Punkte).

## Anti-Ziele

- Kein Pfadwechsel darf einen laufenden Stack seine Daten verlieren lassen.
- Der Agent (root) beschreibt nie einen Pfad außerhalb der geprüften Wurzeln.
- Ein Update darf Docker auf bestehenden Installationen nicht plötzlich sperren.

## Ansätze

1. **Datei `/etc/betternas/paths.json`, vom Agenten geschrieben (gewählt).** Fehlt sie, gilt Docker als „nicht
   eingerichtet“. Eine Stelle, die auch ohne API lesbar ist.
2. Wurzeln in der Datenbank der API und bei jedem Aufruf an den Agenten schicken. Verworfen: Pfade verteilen sich
   über alle Aufrufe, der Agent braucht sie auch ohne API.
3. Feste Pfade und Mounts/Symlinks durch den Nutzer. Vom Nutzer abgelehnt.

## Design

**Zustände.** `paths.json` enthält `{"appdata": "...", "dockerdata": "..."}`.
- Datei vorhanden: eingerichtet.
- Datei fehlt, aber `/srv/appdata/stacks` enthält Stacks: **bestehende Installation**. Der Agent legt beim ersten
  Start die Datei mit `appdata=/srv/appdata` und `dockerdata=/srv/appdata` an. So bleiben alle bisherigen Pfade
  gültig (auch `/srv/appdata/<stack>/…` und der Docker-`data-root` `/srv/appdata/docker`).
- Sonst: **nicht eingerichtet**.

**Sperre.** Im nicht eingerichteten Zustand lehnen alle Docker-RPCs und -Endpunkte mit einem eigenen Fehlercode
(`DockerNotConfigured`) ab, außer `GET /api/v1/docker/status` und den Pfad-Endpunkten. `status` meldet
`configured: false`. Das Frontend zeigt in Stacks, Container, Ressourcen und Proxy eine Leerseite „Docker ist noch
nicht eingerichtet“ mit Knopf zu Einstellungen → Docker. In `postinst.sh` entfällt das Anlegen von
`/srv/appdata/{stacks,docker}` und von `daemon.json`. Docker wird bis zur Einrichtung nicht gestartet bzw.
deaktiviert gehalten (`docker.service` und `docker.socket` per `systemctl disable --now`; beim ersten Setzen werden
sie wieder aktiviert und gestartet), und `/etc/docker/daemon.json` entsteht erst bei der Einrichtung.

**Einrichten.** Neue Karte „Speicherorte“ in `DockerSettings.vue`, bei nicht eingerichtetem Docker als Dialog beim
Öffnen. Zwei Ordnerfelder (mit dem vorhandenen Ordner-Picker und „Ordner anlegen“) und Erklärung: appdata
(klein, Einstellungen), dockerdata (groß: Daten, Datenbanken, Images). Ohne Pool zeigt der Dialog einen Hinweis mit
Link zu Speicher („Zuerst einen Pool oder eine Platte vorbereiten“). Beim Speichern schreibt der Agent `paths.json`,
legt `<appdata>/stacks`, `<appdata>/reverse-proxy`, `<appdata>/backups` und `<dockerdata>/docker` an, schreibt
`/etc/docker/daemon.json` mit `data-root: <dockerdata>/docker` (nur wenn keine eigene vorhanden ist), aktiviert und
startet Docker. Ein späteres Ändern in den Einstellungen verändert den `data-root` einer laufenden Docker-Installation
**nicht**; `daemon.json` wird nur bei der Ersteinrichtung geschrieben (Hinweis in der Oberfläche).

**Erlaubte Orte und Prüfung (Agent).** Absoluter, bereinigter Pfad, existierender Ordner (oder anlegbar, wenn der
Elternordner existiert), nur unterhalb von `/srv/pools`, `/srv/disks` oder (für bestehende Installationen)
`/srv/appdata`. Nie `/` und nie eine der Wurzeln selbst (`/srv/pools`, `/srv/disks`). appdata und dockerdata dürfen
sich nicht verschachteln. Dateisystemprüfung:
- Liegt der Pfad auf einem mergerfs-Pool: **Fehler für dockerdata** (Docker-`data-root` läuft auf FUSE nicht
  zuverlässig), **Warnung** für appdata (Datenbanken, Sperren). Hinweis: einen btrfs-Pool oder eine einzelne Platte
  wählen.
- Liegt der Pfad auf einer Netzwerkfreigabe (nfs/cifs): Fehler.
- Liegt der Pfad auf demselben Gerät wie `/`: Warnung (nur Hinweis).

**API.** `GET/PUT /api/v1/docker/paths` (Rechte wie die übrigen Docker-Einstellungen, Audit beim Ändern). Die
Antwort enthält die Warnungen und die Zahl der Stacks, die Pfade der alten Wurzel nutzen.

**Ordner-Picker.** Erlaubte Wurzeln: `/srv/pools`, `/srv/disks` und die eingerichteten Wurzeln. `FolderRoots`,
`StacksDir`, `ProxyDir`, `appdataRoot`, `backupBase` und das `bindPathRe` in `docker_backup.go` werden Funktionen auf
Basis von `paths.json`.

**Vorlagen (Frontend).** `appdata(sub)` und `dockerdata(sub)`, Wurzeln aus dem Store. Schlüssel `CONFIG_DIR`,
`CONF_DIR`, `STATE_DIR` sowie die Ordner `etc-pihole`, `letsencrypt` und `configs` kommen in appdata, alle anderen
(`DATA_DIR`, `DB_DIR`, `CACHE_DIR`, `TRANSCODE_DIR`, `STORAGE_DIR`, `METADATA_DIR`, `ASSETS_DIR`, `EXT_DIR`,
`WORK_DIR`, `WORKSPACE_DIR`, `CONSUME_DIR`, `WEB_DIR` …) in dockerdata. Die Zuordnung prüft der Umsetzer je Vorlage.
Unterordner wie bisher `<Stackname>/<sub>`. Ohne Einrichtung öffnet sich die Vorlagenauswahl gar nicht.

**Datensicherung vor Updates.** Gesichert werden Bind-Mounts unter appdata und dockerdata mit der bisherigen
Prüfung. Der Hinweis „keine Daten“ nennt beide Wurzeln.

## Dem Umsetzer überlassen

- Ort und Name des Pfad-Pakets, Dateiformat-Details, Aufbau der Karte und des Dialogs.
- Genaue Zuordnung appdata/dockerdata je Vorlage.
- Wie Docker bis zur Einrichtung deaktiviert wird (Image-Bau und Paket-Update beachten, Image-Bau läuft im chroot
  ohne systemd).
- Tests: Pfadprüfungen, Zustände (bestehend/frisch/eingerichtet), Sperre, Backup mit beiden Wurzeln, Vorlagenpfade.

## Offene Punkte / meine Entscheidungen

- **Meine Entscheidung:** Der Docker-`data-root` wird bei der Ersteinrichtung auf `<dockerdata>/docker` gesetzt.
  Nachträgliches Umstellen verschiebt die Images nicht.
- **Meine Entscheidung:** Auch appdata muss auf einer Platte oder einem Pool liegen dürfen, nicht nur dockerdata.
  Der Installer mountet seine Restpartition weiter auf `/srv/appdata`; auf einer frischen Installation lässt sich
  `/srv/appdata` aber nicht mehr als Docker-Ort wählen, solange dort keine Stacks liegen. Ob der Installer die
  Restpartition künftig weglässt oder anders nutzt, ist eine eigene Entscheidung.
- Auf dem Testgerät (0.7.3+dev11) liegen die Stack-Daten weiter unter `/srv/appdata/…`. Es gilt als eingerichtet mit
  beiden Wurzeln gleich `/srv/appdata`.
