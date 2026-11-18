# BetterNAS

Eigenständige, Debian-basierte Distribution für Heimnutzer: NAS (mergerfs, optional SnapRAID),
Docker-Host und KVM-Server mit eigener Web-GUI. Ausgeliefert als bootfähiges ISO mit eigenem Installer.

- Spezifikation: [docs/specs/2026-11-15-betternas-design.md](docs/specs/2026-11-15-betternas-design.md)
- Pläne: [docs/superpowers/plans/](docs/superpowers/plans/)

## ISO bauen

Voraussetzung: Docker (auf macOS mit Apple Silicon z. B. Colima mit Rosetta:
`colima start --vm-type vz --vz-rosetta --cpu 6 --memory 8 --disk 80`) und Go ≥ 1.24.

```bash
make builder ARCH=amd64
make rootfs ARCH=amd64
make iso ARCH=amd64 VERSION=0.0.1-dev
```

Ergebnis: `dist/betternas-<version>-amd64.iso` – per `dd`, balenaEtcher, Rufus oder Ventoy auf einen USB-Stick schreiben.

### Empfohlen: nativ auf dem Entwicklungsrechner (amd64)

Unter Emulation (Apple Silicon) sind debootstrap/Python unzuverlässig. Schneller und stabiler ist der Bau auf
einem BetterNAS-/Debian-Rechner mit KVM (SSH-Alias `betternas-dev`, sudo ohne Passwort):

```bash
make remote VERSION=0.0.3-dev MODES="uefi bios secureboot"
```

Das baut Paket, Root-Dateisystem und ISO dort, führt die QEMU-Tests mit KVM aus und holt ISO und `.deb` nach `dist/`.

### Nur das Paket auf den Entwicklungsrechner bringen

```bash
make build web deb deploy VERSION=0.0.3-dev
```

## SnapRAID (optional pro Pool)

SnapRAID schützt einen Pool vor dem Ausfall von 1–2 Datenplatten (Parität, keine Echtzeit-RAID):

1. Unter **Speicher → Datenträger** eine Platte formatieren und als **Paritätsplatte** verwenden
   (jede Paritätsplatte muss mindestens so groß sein wie die größte Datenplatte des Pools).
2. Unter **Speicher → Pools** am Pool **SnapRAID einrichten**: Paritätsplatten wählen, Zeitpläne und Schwellen prüfen.
3. BetterNAS synchronisiert täglich um 03:00 Uhr (neue und geänderte Dateien in die Parität) und prüft sonntags um
   05:00 Uhr 8 % der Daten, die älter als 10 Tage sind. Beides lässt sich von Hand starten.

Sicherheitsnetze: Der Sync hält an, wenn mehr als 500 Dateien gelöscht wurden (z. B. leere oder neu formatierte Platte);
die GUI fragt dann nach („Trotzdem synchronisieren“). Fehlt eine Platte des Pools, läuft nichts. Solange SnapRAID aktiv
ist, lassen sich Datenplatten nicht aus dem Pool entfernen und Paritätsplatten nicht freigeben – zuerst SnapRAID
abschalten. Die geführte Wiederherstellung einer ausgefallenen Platte folgt in Phase 6; bis dahin hilft
`snapraid -c /etc/betternas/snapraid/<pool>.conf fix -d <platte>` auf der Konsole.

Dateien: `/etc/betternas/snapraid/<pool>.conf` und `.settings`, Zustand und Log in `/var/lib/betternas/snapraid/`,
Timer `betternas-snapraid-sync-<pool>.timer` und `…-scrub-<pool>.timer`.

## Updates und APT-Repository

BetterNAS-Pakete kommen aus einem eigenen, signierten APT-Repository (Suite `stable`, Komponente `main`);
das Basissystem aus den Debian-Quellen. Auf der Seite **System → Updates** lassen sich Updates suchen und installieren
(die Installation läuft in einer eigenen systemd-Unit und übersteht den Neustart von BetterNAS selbst).

```bash
make apt-key                         # einmalig: Signierschlüssel; privater Teil nach ~/.betternas/apt-signing-key.asc
make build web deb VERSION=0.1.0
make repo VERSION=0.1.0              # signiertes Repo nach dist/apt (auf einen Webserver hochladen)
make rootfs iso APT_URL=https://…    # Adresse des Repos ins Image eintragen (ohne APT_URL: keine BetterNAS-Quelle)
```

**Den privaten Schlüssel `~/.betternas/apt-signing-key.asc` selbst sichern.** Geht er verloren, können keine
Updates mehr für bereits installierte Systeme signiert werden; der öffentliche Schlüssel liegt im Paket
(`packaging/apt/betternas-archive-keyring.gpg`).

Updates ohne Webserver testen: `make dev-repo VERSION=<neue-version>` veröffentlicht das Repo auf dem Entwicklungsrechner
(`http://127.0.0.1:8088`) und trägt es dort als Quelle ein.

## Auf echter Hardware installieren

1. USB-Stick (≥ 2 GB, wird komplett überschrieben) mit balenaEtcher beschreiben – oder auf macOS:
   ```bash
   diskutil list                                  # Gerät des Sticks finden, z. B. /dev/disk6
   diskutil unmountDisk /dev/disk6
   sudo dd if=dist/betternas-0.0.1-dev-amd64.iso of=/dev/rdisk6 bs=4m status=progress
   diskutil eject /dev/disk6
   ```
2. Im BIOS/UEFI des Zielrechners (Lenovo ThinkCentre: F1 = Setup, F12 = Bootmenü) vom USB-Stick starten.
   Secure Boot kann eingeschaltet bleiben.
3. Im Bootmenü „BetterNAS installieren“ wählen; der Installer startet automatisch.
4. Nach der Installation USB-Stick abziehen, neu starten. Am Bildschirm erscheint
   `BetterNAS ist bereit: https://<name>.local  https://<ip>`.
5. Im Browser die Adresse öffnen (das selbstsignierte Zertifikat bestätigen) und mit dem Admin-Benutzer anmelden.
   Der Einrichtungsassistent führt durch Datenplatten, Speicherpool und die erste Freigabe.

Fortgeschritten: Ein zweiter Stick (oder eine Partition) mit FAT-Label `BNASCFG` und einer `answers.json`
(Felder siehe `backend/internal/installer/answers.go`, Pflicht: `targetDevice`, `confirmWipe`, `adminUser`,
`adminPassword`; optional `adminSSHKey`) startet eine unbeaufsichtigte Installation.

## Tests

```bash
make test                # Go-Unit-Tests
make e2e MODE=uefi       # QEMU: unbeaufsichtigte Installation + Boot (bios|uefi|secureboot),
                         # danach Datenplatte, Pool, Benutzer, Freigabe, SMB-Zugriff und Neustart
make integration         # echte Konten-, Samba-, Storage- und SnapRAID-Tests auf dem Entwicklungsrechner (root)
```

Der SnapRAID-Integrationstest (`TestSnapraidLifecycle`) braucht root, `losetup`, `mkfs.ext4` und `snapraid`, aber kein
systemd; er läuft auch in einem privilegierten Container, z. B. mit Colima:
`docker run --rm --privileged -v <ordner-mit-test-binary>:/t debian:trixie sh -c 'apt-get update -qq && apt-get install -y -qq snapraid e2fsprogs && /t/agent.test -test.run TestSnapraidLifecycle -test.v'`
(Test-Binary: `go test -c -tags integration ./internal/agent`).

Der Daten-Teil des e2e-Tests braucht `python3` und `smbclient` auf dem Rechner, der ihn ausführt (ohne `smbclient` entfallen
die SMB-Prüfungen).

## Lizenz

BetterNAS steht unter der GNU General Public License, Version 3 oder neuer (GPL-3.0-or-later), siehe
[LICENSE](LICENSE). Beiträge: siehe [CONTRIBUTING.md](CONTRIBUTING.md).

## Release veröffentlichen (GitHub)

APT-Repo über GitHub Pages, ISO als Release-Asset:

```bash
make remote VERSION=0.6.0 APT_URL=https://<owner>.github.io/<repo> MODES="uefi bios secureboot"
tools/release.sh <owner>/<repo> 0.6.0
```

`APT_URL` muss beim ISO-Bau gesetzt sein, sonst trägt das Image keine BetterNAS-Paketquelle ein.
Das Repo braucht aktivierte Pages (Quelle: Branch `gh-pages`); der Signierschlüssel liegt unter
`~/.betternas/apt-signing-key.asc` und muss gesichert sein (`make apt-key` erzeugt ihn einmalig).
