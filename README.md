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

## Tests

```bash
make test                # Go-Unit-Tests
make e2e MODE=uefi       # QEMU: unbeaufsichtigte Installation + Boot (bios|uefi|secureboot)
```
