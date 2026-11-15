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
   `BetterNAS ist bereit: https://<name>.local  https://<ip>`; Anmeldung per SSH mit dem Admin-Benutzer.

Fortgeschritten: Ein zweiter Stick (oder eine Partition) mit FAT-Label `BNASCFG` und einer `answers.json`
(Felder siehe `backend/internal/installer/answers.go`, Pflicht: `targetDevice`, `confirmWipe`, `adminUser`,
`adminPassword`; optional `adminSSHKey`) startet eine unbeaufsichtigte Installation.

## Tests

```bash
make test                # Go-Unit-Tests
make e2e MODE=uefi       # QEMU: unbeaufsichtigte Installation + Boot (bios|uefi|secureboot)
```
