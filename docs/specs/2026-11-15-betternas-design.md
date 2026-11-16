# BetterNAS – Design-Spezifikation

Stand: 2026-11-15 · Status: Entwurf zur Prüfung

## 1. Ziel

BetterNAS ist eine **eigenständige, schlanke Linux-Distribution** für Heimnutzer, die als
**NAS**, **Docker-Host** und **KVM-Server** dient und vollständig über eine eigene
Web-GUI verwaltet wird. Ausgeliefert wird ein **bootfähiges ISO**, das per USB-Stick
gestartet wird und mit einem **eigenen Installer** das komplette System einrichtet –
der Nutzer installiert nie selbst ein Debian.

Intern basiert BetterNAS auf Debian-Paketen (Sicherheitsupdates, Hardware-Support).
Eigene Komponenten werden als `.deb` gebaut, ins Image integriert und darüber
aktualisiert. Zusätzliche Software kann jederzeit per `apt` oder als Container
installiert werden.

### Festgelegte Rahmenbedingungen

| Thema | Entscheidung |
|---|---|
| Zielplattform | x86_64; ARM64 später möglich, darf nicht verbaut werden |
| Basis | Debian 13 „Trixie“ (Pakete), eigenes Image |
| Auslieferung | Hybrid-ISO für USB-Sticks (dd, Rufus, Etcher, Ventoy); UEFI, BIOS und Secure Boot |
| Installation | Eigener Installer in Go mit Textoberfläche, entpackt ein fertiges Image (offline) |
| Zielgruppe | Heimnutzer (einfache Bedienung, sichere Voreinstellungen) |
| Storage | mergerfs als Pool-Basis, **SnapRAID optional pro Pool** |
| Backend | Go (ohne cgo, damit Cross-Compile für ARM64 trivial bleibt) |
| Frontend | Vue 3 + TypeScript + Vite |
| Persistenz | SQLite |

### Nicht-Ziele (vorerst)

- Active Directory / LDAP, Quotas, feingranulare Rollen
- Cluster- oder Hochverfügbarkeitsbetrieb
- ZFS / Btrfs-RAID als Pool-Typ
- Debian-Installer (d-i) als Installationsweg
- Installer im Browser für Geräte ohne Monitor (später optional)
- Zugriff aus dem Internet ohne zusätzliche Absicherung (Standard: nur LAN)

## 2. Architektur

```
Browser (Vue-SPA)
   │  HTTPS: REST (/api/v1) + WebSocket (/api/v1/ws)
   ▼
betternas-api      (Benutzer `betternas`, unprivilegiert)
   │  Auth, Sessions, Validierung, Jobs, Audit-Log, SQLite, statische UI
   │  Unix-Socket /run/betternas/agent.sock (JSON-RPC, nur Whitelist)
   ▼
betternas-agent    (root)
   │  Disks, Mount-Units, mergerfs, SnapRAID, Samba, Benutzer, apt, Netzwerk
   ├── systemd / udev
   ├── Docker   (API greift direkt auf docker.sock zu, Gruppe `docker`)
   └── libvirt  (API greift über go-libvirt zu, Gruppe `libvirt`)
```

### Prinzipien

1. **Rechtetrennung:** Die API läuft nie als root. Der Agent bietet nur eine feste
   Menge typisierter Operationen (z. B. `disk.format`, `pool.apply`), keine
   beliebigen Shell-Befehle. Jede Operation validiert ihre Parameter erneut.
2. **Deklarativer Soll-Zustand:** Die GUI ändert Datensätze in SQLite. Ein
   *Renderer* erzeugt daraus die Systemkonfiguration (systemd-Units,
   `snapraid.conf`, Samba-Fragmente) und wendet sie an. Generierte Dateien tragen
   einen Kopf-Kommentar „verwaltet von BetterNAS“.
3. **Nur eigene Dateien verwalten:** z. B. `/etc/samba/betternas.d/`,
   `/etc/systemd/system/srv-*.mount`, `/etc/betternas/`. Fremde Konfiguration
   wird nicht überschrieben; Abweichungen (Drift) werden erkannt und angezeigt.
4. **Jobs:** Langlaufende Aktionen (Formatieren, SnapRAID-Sync, Image-Pull,
   VM-Erstellung) laufen als Job mit ID, Status, Fortschritt und Log; Updates
   gehen per WebSocket an die UI.
5. **Ereignisse:** udev, Docker-Events und libvirt-Events werden in einen internen
   Event-Bus eingespeist und an die UI weitergereicht.
6. **Architekturneutral:** Kein cgo. Architekturabhängiges (UEFI-Firmware, ISO-Bau,
   Paketnamen) ist hinter kleinen Abstraktionen gekapselt.

## 3. Verzeichnislayout auf dem Zielsystem

| Pfad | Inhalt |
|---|---|
| `/srv/disks/<disk-id>` | Einhängepunkte einzelner Datenplatten |
| `/srv/parity/<disk-id>` | Einhängepunkte von SnapRAID-Paritätsplatten |
| `/srv/pools/<pool>` | mergerfs-Pools (Ziel für Shares) |
| `/srv/appdata/` | Docker-Appdata und Stacks (Systemplatte/SSD) |
| `/srv/appdata/stacks/<stack>/compose.yaml` | Compose-Stacks |
| `/srv/vms/` | Standard-Speicherort für VM-Disks und ISOs (SSD) |
| `/etc/betternas/` | Konfiguration, TLS-Zertifikate |
| `/var/lib/betternas/betternas.db` | SQLite-Datenbank |

Docker `data-root`, Appdata und VM-Disks liegen bewusst **nicht** auf mergerfs
(FUSE verträgt sich schlecht mit overlay2, Datenbanken und VM-Images).

## 4. Module

### 4.0 Distribution und Installer

**Image-Build (CI)**
- `mmdebstrap` erzeugt das Root-Dateisystem des Zielsystems (Debian Trixie, `main` +
  `non-free-firmware`, feste Paketliste inkl. BetterNAS-Paketen) → `rootfs.squashfs` (zstd).
- `live-build` erzeugt das Live-System, das den Installer startet, und packt
  `rootfs.squashfs` mit ins ISO → `betternas-<version>-<arch>.iso` (iso-hybrid).
- Boot: UEFI (Debians signiertes shim + GRUB → Secure Boot) und BIOS.
- Architektur ist Build-Parameter (`amd64` jetzt, `arm64` später).

**Installer** (`betternas-installer`, Go, Bubble Tea, startet automatisch auf tty1)
1. Sprache (Deutsch, Englisch), Tastatur (wird sofort im Live-System aktiv), Zeitzone
   (Standard `Europe/Berlin`).
2. Systemdatenträger wählen (SSD empfohlen; HDD mit Warnung). Der USB-Stick selbst wird
   nicht angeboten. Alle anderen Platten bleiben unberührt.
3. Aufteilung: System (Standard 32 GiB, min. 16 GiB), Rest als `/srv/appdata`
   (Docker, VMs; entfällt bei < 8 GiB Rest).
4. Hostname (Standard `betternas`).
5. Netzwerk: Schnittstelle, DHCP (Standard) oder statisch (Adresse/CIDR, Gateway, DNS).
6. Admin-Benutzer + Passwort (Mitglied in `betternas-admin` und `sudo`, Samba-Passwort
   wird gesetzt; root bleibt gesperrt).
7. Zusammenfassung + ausdrückliche Bestätigung → Installation (Partitionieren,
   Image entpacken, Konfiguration schreiben, Bootloader für UEFI **und** BIOS).
8. Neustart. Konsole zeigt „BetterNAS ist bereit“ mit `https://<hostname>.local` und IP.

Partitionsschema (GPT): BIOS-Boot 1 MiB · EFI 512 MiB (vfat) · Root (ext4, `bnas-root`)
· Appdata (ext4, `bnas-appdata`, optional). Kein Swap-Partition; stattdessen zram.

**Unbeaufsichtigte Installation:** Liegt beim Start ein Datenträger mit Label `BNASCFG`
und Datei `answers.json` vor, läuft der Installer ohne Rückfragen (für CI-Tests und
Fortgeschrittene). Die Datei muss das Zielgerät zusätzlich in `confirmWipe` nennen.

**Nach dem ersten Login** führt ein Einrichtungsassistent in der Web-GUI durch Datenplatten,
Pool (optional SnapRAID) und erste Freigabe (ab Phase 2, sobald Freigaben existieren).

**Updates:** über APT – Debian-Repos für das Basissystem, eigenes BetterNAS-Repo für
die eigenen Pakete.

### 4.1 Storage

**Disk-Erkennung**
- Quelle: `lsblk --json --bytes -O`, `/dev/disk/by-id`, `smartctl --json`.
- Stabile Identität über WWN bzw. `by-id` (Modell + Seriennummer), nie über `/dev/sdX`.
- Die Systemplatte (alle Platten mit Partitionen, die `/`, `/boot` oder Swap tragen)
  ist **gesperrt** und erscheint nur lesend.
- Live-Aktualisierung über udev (netlink).

**Rollen einer Platte:** `unassigned` · `data` · `parity` · `system`.

**Platte vorbereiten** (`disk.format`)
- GPT, eine Partition, Dateisystem ext4 (Standard) oder XFS, Label `bnas-<kurz-id>`.
- Sicherheitsstufen: Bestätigung durch Eintippen der Seriennummer; Dry-Run zeigt,
  was gelöscht wird; bestehende Signaturen werden angezeigt.
- Vorhandene Dateisysteme können **ohne Formatieren** übernommen werden (Import).

**Mounts**
- Pro Platte eine systemd-Mount-Unit (`srv-disks-<id>.mount`) mit `nofail`.
- Pool als Mount-Unit `srv-pools-<name>.mount`, `Type=fuse.mergerfs`,
  abhängig von den Branch-Mounts.

**Pool (mergerfs)**
- Attribute: Name, Branches (Datenplatten), Create-Policy (Standard `epmfs`,
  Auswahl `mfs`, `lfs`, `pfrd`), `minfreespace` (Standard 20G), weitere Optionen
  mit sicheren Defaults (`cache.files=off`, `category.create=…`, `moveonenospc=true`).
- **Anlegen:** Platten wählen → Unit schreiben → starten.
- **Bearbeiten:** Branches hinzufügen/entfernen, Policy ändern → Unit neu schreiben,
  Remount (Shares werden kurz neu geladen). Entfernen eines Branches ist nur
  erlaubt, wenn die Platte nicht gleichzeitig Parität-relevant ist bzw. nach
  Warnung (Daten auf dieser Platte sind danach nicht mehr im Pool sichtbar).
- **Löschen:** nur wenn keine Shares/Stacks darauf zeigen (sonst Liste der
  Abhängigkeiten anzeigen). Daten auf den Platten bleiben erhalten.

**SnapRAID (optional pro Pool)**
- Aktivierbar beim Anlegen oder später. Benötigt 1–2 Paritätsplatten
  (UI begrenzt vorerst auf 2), jede ≥ größte Datenplatte (wird geprüft).
- Generiert `/etc/betternas/snapraid/<pool>.conf` (Content-Dateien auf mindestens
  zwei Datenplatten und der Systemplatte).
- Zeitpläne als systemd-Timer: `sync` (Standard täglich 03:00), `scrub`
  (Standard wöchentlich, 8 %, älter als 10 Tage).
- Schutz vor Massenlöschung: Sync bricht ab, wenn mehr als N Dateien gelöscht
  wurden (Standard 500); UI fragt nach.
- Status in der UI: letzter Sync, Anzahl ungeschützter Änderungen (`snapraid diff`),
  letzter Scrub, gefundene Fehler.
- Wiederherstellung einer ausgefallenen Platte als geführter Assistent
  (neue Platte zuweisen → `snapraid fix -d <name>` als Job) – Phase 6.

### 4.2 Freigaben (Samba)

- Share = Name, Pfad (Pool oder Unterordner), Beschreibung, sichtbar ja/nein,
  Gast-Zugriff, Papierkorb (`vfs_recycle`), macOS-Unterstützung (`vfs_fruit`),
  Rechte pro Benutzer/Gruppe: `keine` · `lesen` · `lesen/schreiben`.
- Renderer schreibt `/etc/samba/betternas.d/<share>.conf`, ein von BetterNAS
  verwaltetes `include` in `smb.conf`, prüft mit `testparm -s`, dann
  `smbcontrol all reload-config`.
- Netzwerkerkennung: `wsdd2` (Windows), `avahi` (macOS/Linux, `betternas.local`).
- NFS und Time-Machine-Ziel: später.

### 4.3 Benutzer und Gruppen

- Linux-Benutzer (UID ≥ 10000, Shell `/usr/sbin/nologin` für reine NAS-Benutzer)
  und Gruppen; Samba-Passwort wird bei jeder Passwortänderung synchronisiert.
- Rollen: **Admin** (Gruppe `betternas-admin`, darf GUI bedienen) und
  **Benutzer** (nur Freigaben, optional Self-Service-Seite fürs Passwort).
- GUI-Login über PAM; Sitzung als HttpOnly-/Secure-/SameSite-Cookie,
  CSRF-Token, Login-Rate-Limit. TOTP-2FA optional (Phase 6).
- Audit-Log in SQLite: Zeit, Benutzer, Aktion, Ziel, Ergebnis.

### 4.4 Docker

- Stacks als Dateien unter `/srv/appdata/stacks/<stack>/` (`compose.yaml`, `.env`),
  bleiben per CLI nutzbar.
- Editor: Monaco mit YAML-Modus und Compose-Spec-JSON-Schema; vor dem Deploy
  `docker compose config` als Validierung.
- Ausführung über `docker compose` CLI (Job mit Live-Log); Lesen von Status, Logs,
  Stats und Events über das Docker-Go-SDK.
- Aktionen: deploy, start, stop, restart, down, löschen; Update-Prüfung per
  Digest-Vergleich, Update = `pull` + `up -d`.
- Container: Logs (Stream), Stats, Shell (xterm.js über WebSocket), Ports, Volumes.
- Images/Netzwerke/Volumes: Liste, entfernen, prune (mit Bestätigung).
- App-Vorlagen (Jellyfin, Nextcloud, Home Assistant …) mit Pfad-Picker: Phase 6.

### 4.5 Virtuelle Maschinen (KVM)

- Anbindung: `github.com/digitalocean/go-libvirt` (reines Go).
- Assistent: Name, OS-Typ, vCPU, RAM, Disk (qcow2, Größe, Speicherort), ISO
  (Upload/Auswahl), Firmware (UEFI Standard, BIOS), TPM (swtpm), Netzwerk
  (Bridge `br0` oder NAT `default`), VirtIO-Treiber-ISO für Windows.
- Betrieb: start, shutdown, force-off, reboot, autostart, Snapshots, klonen,
  löschen (mit/ohne Disks).
- Konsole: noVNC; API proxyt den VNC-Socket über WebSocket (nur authentifiziert).
- Später: PCI-/GPU-Passthrough, USB, XML-Expertenmodus.

### 4.6 System

- Dashboard: CPU, RAM, Netz, Platten-Belegung und -Temperatur, SMART-Warnungen,
  SnapRAID-Status.
- Netzwerk: DHCP/statisch, Hostname, DNS, Bridge `br0` für VMs (systemd-networkd).
- Updates: APT-Updates anzeigen/einspielen; BetterNAS-Pakete über eigenes APT-Repo.
- TLS: selbstsigniertes Zertifikat bei Installation; eigenes Zertifikat hochladbar.
- Benachrichtigungen (Mail, ntfy): Phase 6.
- Konfigurations-Backup/-Restore (DB + `/etc/betternas`): Phase 6.
- Energie: HDD-Spindown pro Platte (`hdparm -S`), Phase 6.

## 5. Datenmodell (SQLite, Auszug)

```
disks(id, by_id_path, wwn, serial, model, size_bytes, role, fs_type, fs_uuid, label, created_at)
pools(id, name, mount_path, create_policy, min_free, options_json, snapraid_enabled, created_at)
pool_branches(pool_id, disk_id, position)
snapraid(pool_id, sync_schedule, scrub_schedule, scrub_percent, scrub_older_days, delete_threshold)
snapraid_parity(pool_id, disk_id, level)          -- level 1 oder 2
shares(id, name, path, comment, browsable, guest, recycle, fruit, created_at)
share_acl(share_id, principal_type, principal_name, access)   -- user|group, none|ro|rw
stacks(id, name, path, created_at)
jobs(id, kind, target, status, progress, log, started_at, finished_at, started_by)
audit(id, ts, actor, action, target, result, details_json)
settings(key, value)
```

Benutzer und Gruppen leben im System (`/etc/passwd`, `/etc/group`), nicht in der DB.
VMs leben in libvirt, Container in Docker – die DB speichert nur ergänzende Metadaten.

## 6. API (Auszug)

```
POST   /api/v1/auth/login | /auth/logout      GET /api/v1/auth/me
GET    /api/v1/disks                          POST /api/v1/disks/{id}/format | /import
GET    /api/v1/pools                          POST /api/v1/pools
PATCH  /api/v1/pools/{id}                     DELETE /api/v1/pools/{id}
POST   /api/v1/pools/{id}/snapraid/sync | /scrub    GET /api/v1/pools/{id}/snapraid/status
CRUD   /api/v1/shares   /api/v1/users   /api/v1/groups
CRUD   /api/v1/stacks   POST /api/v1/stacks/{id}/deploy | /stop | /update
GET    /api/v1/containers  …/logs (WS)  …/exec (WS)
CRUD   /api/v1/vms      POST /api/v1/vms/{id}/start | /shutdown | …   GET …/console (WS)
GET    /api/v1/jobs/{id}                      WS  /api/v1/ws  (Events, Job-Fortschritt)
```

Fehler einheitlich als `{ "error": { "code": "...", "message": "...", "details": {...} } }`.

## 7. Repository-Struktur

```
BetterNAS/
├── backend/                 Go-Modul
│   ├── cmd/betternas-api/
│   ├── cmd/betternas-agent/
│   ├── cmd/betternas-installer/
│   └── internal/
│       ├── installer/       Antworten, Partitionsplan, Ausführung, TUI
│       ├── api/             HTTP-Handler, Middleware, WS
│       ├── agent/           RPC-Server + Client, Operationen
│       ├── auth/  jobs/  events/  db/  audit/
│       ├── storage/         Disks, Pools, mergerfs, SnapRAID, Renderer
│       ├── shares/  users/  docker/  vms/  system/
│       └── sysexec/         Abstraktion für Befehle/Dateien (testbar)
├── frontend/                Vue 3 + TS + Vite
├── packaging/               Paketdefinitionen, systemd-Units, postinst
├── image/                   Root-Dateisystem (mmdebstrap), Live-ISO (live-build), Paketlisten
├── test/image/              QEMU-Ende-zu-Ende-Test (Installation BIOS/UEFI/Secure Boot)
├── test/integration/        QEMU-Testumgebung mit virtuellen Platten
├── test/e2e/                Playwright
└── docs/
```

## 8. Phasen

| Phase | Inhalt | Ergebnis |
|---|---|---|
| 0a | Image-Build: Root-Dateisystem, Live-ISO, UEFI/BIOS/Secure Boot, QEMU-Boot-Test in CI | Bootfähiges ISO |
| 0b | Installer (TUI + unbeaufsichtigter Modus) | **ISO installiert ein lauffähiges System** |
| 0c | Repo-Grundlagen, Go-Gerüst API/Agent, Vue-Gerüst, Login, Jobs, Audit, .deb im Image | GUI im installierten System |
| 1 | Disk-Erkennung, Formatieren/Importieren, Mount-Units, mergerfs-Pools CRUD | Pools per GUI |
| 2 | Benutzer/Gruppen, Samba-Sync, Shares, Einrichtungsassistent nach erstem Login, APT-Repo | **Erstes Release als ISO (MVP)** |
| 3 | SnapRAID optional pro Pool, Timer, Status (umgesetzt, 0.2.0) | Redundanz |
| 4a | Docker installiert/eingerichtet, Stacks (Editor, Validierung, Deploy/Update/Löschen), Container-Liste und -Aktionen (umgesetzt) | Docker-Verwaltung |
| 4b | Logs-Stream, Shell (xterm.js), Stats, Update-Prüfung per Digest, Images/Netzwerke/Volumes (umgesetzt) | Docker-Verwaltung komplett |
| 5 | KVM: Assistent, noVNC, Snapshots, Bridge | VM-Verwaltung |
| 6 | Ausbau: SMART-Alarme, Benachrichtigungen, App-Vorlagen, Spindown, 2FA, Backup, Restore-Assistent, Passthrough, NFS, Browser-Installer | Feature-Reife |

Abweichung Phase 3: Sync/Scrub laufen nicht als Job-RPC, sondern in einer eigenen Unit des Agents
(`betternas-agent snapraid-run`, von Timern und GUI gestartet; `flock` gegen Parallelläufe, Ergebnis in
`/var/lib/betternas/snapraid/<pool>.state.json`). Die GUI zeigt den Lauf trotzdem als Job mit Log. SnapRAID wird am
Pool nach dem Anlegen eingerichtet, nicht im Anlege-Dialog.

Abweichung Phase 4a: Die API greift nicht selbst auf `docker.sock` zu (Gruppe `docker` ist root-gleich); alle
Docker-Operationen sind typisierte Agent-RPCs über die `docker`-CLI. Stacks sind nur Dateien unter
`/srv/appdata/stacks/<name>/` (root, 0600/0700), es gibt keine `stacks`-Tabelle. Deploy/Pull/Down laufen in einer
transienten Unit `betternas-stack-<name>-<ts>` (Job pollt `stack.runstatus`). Docker `data-root` ist
`/srv/appdata/docker` (`/etc/docker/daemon.json`, nur gesetzt, wenn keine eigene existiert). Der Editor ist Monaco mit
YAML-Grundmodus (lazy geladen); validiert wird serverseitig mit `docker compose config`.

Abweichung Phase 4b: Logs und Shell laufen über einen bidirektionalen RPC-Kanal (`rpc.Session`, Agent-Sitzungen
`container.logs`/`container.shell` mit PTY über `creack/pty`), den die API per WebSocket an den Browser reicht
(`/api/v1/containers/{id}/logs|shell`). Stats sind ein einfacher Aufruf (`docker stats --no-stream`, GUI fragt alle 5 s).
Die Update-Prüfung vergleicht je Stack die lokalen RepoDigests mit `docker buildx imagetools inspect` (Job, Ergebnis in
`settings`/`docker.updates`). Images, Netzwerke und Volumes: Liste, Entfernen, Prune mit Bestätigung. Nicht umgesetzt:
Compose-Schema-Validierung im Editor (`monaco-yaml`).

Entwickelt und getestet wird ab Phase 0b in VMs, die aus dem eigenen ISO installiert wurden.

## 9. Teststrategie

- **Unit:** Renderer (Soll-Zustand → Dateien) als Golden-File-Tests; Systemzugriffe
  über `sysexec`-Interfaces gemockt.
- **Integration:** QEMU-VM (Debian 13) mit 4–6 virtuellen Platten; Szenarien:
  Pool anlegen/ändern/löschen, SnapRAID-Sync und Fix einer „ausgefallenen“ Platte,
  Share per `smbclient` prüfen, Stack deployen, verschachtelte VM starten.
- **E2E:** Playwright gegen die Test-VM.
- **CI:** Lint, Unit-Tests, Build amd64 + arm64, .deb-Build; Integration nightly.

## 10. Sicherheit

- GUI standardmäßig nur aus privaten Netzen (RFC 1918 / ULA) erreichbar, Port 443.
- Agent-Socket nur für Benutzer `betternas` (Dateirechte + `SO_PEERCRED`-Prüfung).
- Destruktive Operationen: Server-seitige Bestätigung (Token aus Dry-Run nötig).
- Uploads (ISOs) mit Größenlimit und Pfadprüfung; keine Pfade außerhalb erlaubter Wurzeln.

## 11. Offene Punkte

- Lizenz des Projekts (z. B. GPL-3.0 oder MIT/Apache-2.0).
- Name/Branding endgültig „BetterNAS“?
- Hosting von ISO-Downloads und APT-Repository (z. B. GitHub Releases + Pages).
