# BetterNAS Phase 0a + 0b (Distribution & Installer) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ein Hybrid-ISO `betternas-<version>-amd64.iso`, das von USB bootet (UEFI, BIOS, Secure Boot), den BetterNAS-Installer startet und ein lauffähiges BetterNAS-Grundsystem auf die gewählte Systemplatte installiert – interaktiv oder unbeaufsichtigt.

**Architecture:** `mmdebstrap` baut das Root-Dateisystem des Zielsystems aus einer festen Paketliste und packt es als `rootfs.squashfs`. `live-build` baut ein minimales Live-System, das `rootfs.squashfs` im ISO mitführt und auf tty1 den Go-Installer startet. Der Installer trennt strikt: `Answers` (Eingaben) → `Layout` (berechneter Plan, reine Logik) → `Execute` (Befehle über `sysexec.Runner`, testbar mit Fake). Ein QEMU-Test installiert in CI unbeaufsichtigt und bootet das Ergebnis.

**Tech Stack:** mmdebstrap ≥ 1.5, live-build (Trixie), squashfs-tools, xorriso, QEMU + OVMF · Go 1.24 (`CGO_ENABLED=0`), `github.com/charmbracelet/bubbletea`, `bubbles`, `lipgloss`.

**Spec:** `docs/specs/2026-11-15-betternas-design.md` (Abschnitt 4.0)

**Ausführungsreihenfolge:** Plan `2026-11-15-phase0-1-fundament-storage.md` Task 1 (Repo), Task 2 (sysexec) und Task 10 (lsblk) → **dieser Plan** → restliche Tasks des Fundament-Plans.

## Global Constraints

- Debian 13 „Trixie“, Komponenten `main non-free-firmware`; Architektur als Build-Parameter `ARCH` (`amd64` jetzt; `arm64` darf nicht verbaut werden – keine x86-Pfade fest im Code, sondern über `ARCH`).
- Alle Go-Binaries `CGO_ENABLED=0`.
- ISO-Name `betternas-<version>-<arch>.iso`, Volume-Label `BETTERNAS`, Squashfs-Pfad im ISO `/betternas/rootfs.squashfs`, im Live-System erreichbar unter `/run/live/medium/betternas/rootfs.squashfs`.
- Partitionen (GPT): `1` BIOS-Boot 1 MiB (`EF02`) · `2` EFI 512 MiB (`EF00`, vfat, Label `EFI`) · `3` Root (`8300`, ext4, Label `bnas-root`, Standard 32 GiB, min. 16 GiB) · `4` Appdata (`8300`, ext4, Label `bnas-appdata`, nur wenn Rest ≥ 8 GiB). Mindestgröße der Systemplatte 24 GiB.
- Kein Swap-Partition; `systemd-zram-generator` im Image.
- Sprachen `de` (Standard), `en`; Locales `de_DE.UTF-8`, `en_US.UTF-8` im Image vorgeneriert. Tastaturen `de`, `de-nodeadkeys`, `ch`, `at`, `us`. Zeitzone Standard `Europe/Berlin`. Hostname Standard `betternas`.
- Admin-Benutzer: Mitglied in `betternas-admin` und `sudo`, Samba-Passwort gesetzt; root gesperrt.
- Netzwerk im Zielsystem über `systemd-networkd` + `systemd-resolved`; Zeit über `systemd-timesyncd`.
- Kernel-Kommandozeile Zielsystem: `quiet console=tty0 console=ttyS0,115200`.
- Konsolen-Banner im Zielsystem: Zeile `BetterNAS ist bereit: https://<hostname>.local  https://<ip>` (eine Zeile je IPv4).
- UI-Texte Deutsch (Englisch als zweite Sprache).

## Umsetzungsnotizen (Abweichungen beim Bau)

- Banner: statt Skript + Timer eine statische `/etc/issue.d/betternas.issue` mit agetty-Escapes `\n` (Hostname) und `\4` (IPv4); agetty aktualisiert die Anzeige selbst.
- UEFI-Booteintrag: `--bootloader-id=debian`, weil Debians signiertes GRUB seine Konfiguration fest unter `EFI/debian` sucht. Zusätzlich wird **immer** (auch im BIOS-Modus) der Fallback `--removable --no-nvram` installiert, damit die Platte auch später per UEFI bootet.
- Windows-Netzwerkerkennung: Paket heißt in Trixie `wsdd2`.
- Builder-Container: `/bin/sh` → bash, weil dash unter Rosetta keine Dateideskriptoren ≥ 10 umleiten kann.
- Neues optionales Feld `adminSSHKey` (nur unbeaufsichtigt): öffentlicher Schlüssel für den Admin – für Tests und Entwicklungsrechner.
- Live-Installer läuft als eigener Dienst `betternas-installer.service` auf tty1 (statt Autologin-Getty).

## Review Focus

1. **Installation auf den falschen Datenträger** – USB-Stick, von dem gebootet wurde, darf nie wählbar sein; ohne ausdrückliche Bestätigung wird nichts gelöscht; im unbeaufsichtigten Modus muss `confirmWipe` exakt dem Zielgerät entsprechen (Tests in Task 3 und 6).
2. **Tastaturlayout beim Passwort-Tippen** – das gewählte Layout muss im Live-System aktiv sein, bevor Benutzername/Passwort eingegeben werden, sonst passt das Passwort nach dem Neustart nicht (Test in Task 5).
3. **NVMe- und eMMC-Gerätenamen** – Partitionen heißen `nvme0n1p1` bzw. `mmcblk0p1`, nicht `nvme0n11` (Test in Task 3).
4. **Mainboards, die den UEFI-Booteintrag verlieren** – zusätzlich Fallback-Bootloader `EFI/BOOT/BOOTX64.EFI` installieren (Test in Task 4).
5. **Neu installierte Systeme teilen Identitäten** – `machine-id` und SSH-Host-Keys dürfen nicht aus dem Image stammen (Tests in Task 1 und 4).

---

## Dateistruktur

```
image/
├── build-rootfs.sh            mmdebstrap → rootfs.tar → rootfs.squashfs
├── build-iso.sh               live-build → ISO
├── packages/rootfs.list       Paketliste Zielsystem
├── packages/live.list         Paketliste Live-System
├── hooks/rootfs/*.sh          mmdebstrap --customize-hook Skripte
├── overlay/rootfs/            Dateien fürs Zielsystem (Banner, networkd-Defaults, apt sources)
├── live/                      live-build-Konfiguration (auto/config, includes.chroot, includes.binary, bootloaders)
├── tests/check-rootfs.sh      Prüfungen auf dem gebauten rootfs.tar
└── Dockerfile.builder         Build-Umgebung (debian:trixie + mmdebstrap, live-build, squashfs-tools, xorriso)
backend/
├── cmd/betternas-installer/main.go
└── internal/installer/
    ├── answers.go  validate.go  layout.go       reine Logik
    ├── targets.go                                Kandidaten für Systemplatte
    ├── execute.go  render.go                     Installation
    ├── unattended.go                             BNASCFG / answers.json
    └── tui/{model.go,steps.go,styles.go}         Bubble-Tea-Oberfläche
test/image/
├── e2e.sh                     QEMU: unbeaufsichtigte Installation + Boot-Prüfung
└── answers.json               Antworten für den Test
```

---

### Task 1: Root-Dateisystem des Zielsystems

**Files:**
- Create: `image/Dockerfile.builder`, `image/build-rootfs.sh`, `image/packages/rootfs.list`, `image/hooks/rootfs/10-identity.sh`, `20-locales.sh`, `30-services.sh`, `40-apt.sh`, `image/overlay/rootfs/usr/lib/betternas/banner.sh`, `image/overlay/rootfs/etc/systemd/system/betternas-banner.service`, `image/overlay/rootfs/etc/default/grub.d/betternas.cfg`, `image/tests/check-rootfs.sh`
- Modify: `Makefile` (Targets `builder`, `rootfs ARCH=amd64`)

**Interfaces:**
- Produces: `dist/rootfs-<arch>.squashfs` (zstd, Level 19) und `dist/rootfs-<arch>.tar`; `make rootfs` läuft in `betternas-builder`-Container (`docker run --privileged --platform linux/<arch>`), damit es auch auf macOS funktioniert.
  `rootfs.list` (eine Zeile je Paket): `linux-image-amd64` (über `ARCH` ersetzt), `systemd-sysv`, `systemd-resolved`, `systemd-timesyncd`, `systemd-zram-generator`, `udev`, `kmod`, `initramfs-tools`, `grub-efi-amd64`, `grub-efi-amd64-signed`, `shim-signed`, `grub-pc-bin`, `efibootmgr`, `openssh-server`, `sudo`, `ca-certificates`, `locales`, `console-setup`, `keyboard-configuration`, `tzdata`, `unattended-upgrades`, `nftables`, `avahi-daemon`, `libnss-mdns`, `samba`, `wsdd`, `mergerfs`, `smartmontools`, `gdisk`, `parted`, `e2fsprogs`, `xfsprogs`, `dosfstools`, `firmware-linux-free`, `firmware-misc-nonfree`, `firmware-realtek`, `intel-microcode`, `amd64-microcode`, `less`, `nano`, `curl`. (BetterNAS-Pakete kommen mit Fundament-Plan Task 9 hinzu.)
  Aufruf: `mmdebstrap --variant=minbase --architectures=$ARCH --components="main non-free-firmware" --include="$(paste -sd, rootfs.list)" --customize-hook='copy-in overlay/rootfs/. /' --customize-hook=hooks/rootfs/*.sh trixie rootfs.tar`.
  Hooks: `10-identity` leert `/etc/machine-id` (Datei bleibt, Inhalt leer), löscht `/var/lib/dbus/machine-id` und `/etc/ssh/ssh_host_*`, setzt Hostname `betternas`; `20-locales` generiert `de_DE.UTF-8` und `en_US.UTF-8`; `30-services` aktiviert `systemd-networkd`, `systemd-resolved`, `systemd-timesyncd`, `ssh`, `avahi-daemon`, `betternas-banner`, sperrt root (`passwd -l root`); `40-apt` schreibt `/etc/apt/sources.list.d/debian.sources` (trixie, trixie-updates, trixie-security; `main non-free-firmware`).
  `banner.sh` schreibt `/etc/issue.d/betternas.issue` mit der Banner-Zeile aus den Global Constraints (IPv4 via `ip -4 -o addr show scope global`); `betternas-banner.service` (oneshot) läuft nach `network-online.target`; `betternas-banner.timer` wiederholt ihn alle 30 s, damit IP-Änderungen per DHCP im Banner ankommen. Beide gehören zum Overlay.
  `grub.d/betternas.cfg`: `GRUB_CMDLINE_LINUX_DEFAULT="quiet console=tty0 console=ttyS0,115200"`, `GRUB_DISTRIBUTOR="BetterNAS"`, `GRUB_TIMEOUT=2`.

- [ ] **Step 1: Prüfskript schreiben** `check-rootfs.sh <rootfs.tar>` – schlägt fehl (Exit 1 mit Meldung), wenn: `etc/machine-id` nicht leer ist; `etc/ssh/ssh_host_*` existiert; `usr/bin/mergerfs`, `usr/sbin/smbd`, `usr/lib/shim/shimx64.efi.signed`, `usr/lib/grub/i386-pc/` fehlen; `usr/lib/betternas/banner.sh` nicht ausführbar ist; `etc/systemd/system/timers.target.wants/betternas-banner.timer` fehlt; `etc/systemd/system/multi-user.target.wants/systemd-networkd.service` fehlt; `etc/shadow` für root kein `!` enthält.
- [ ] **Step 2:** Run `image/tests/check-rootfs.sh /nonexistent.tar` → FAIL.
- [ ] **Step 3:** Builder-Image, Build-Skript, Paketliste, Hooks und Overlay implementieren.
- [ ] **Step 4:** Run `make builder rootfs ARCH=amd64 && image/tests/check-rootfs.sh dist/rootfs-amd64.tar` → PASS; `ls -lh dist/rootfs-amd64.squashfs` (Richtwert < 700 MB).
- [ ] **Step 5:** `shellcheck image/*.sh image/hooks/rootfs/*.sh image/overlay/rootfs/usr/lib/betternas/*.sh` → keine Befunde.
- [ ] **Step 6:** Commit `feat(image): target root filesystem built with mmdebstrap`.

---

### Task 2: Installer-Antworten und Validierung

**Files:**
- Create: `backend/internal/installer/answers.go`, `validate.go`, `answers_test.go`

**Interfaces:**
- Produces:
  ```go
  type NetConfig struct{ Interface string; DHCP bool; Address, Gateway string; DNS []string } // Address als CIDR, z. B. "192.168.1.20/24"
  type Answers struct{
      Language, Keymap, Timezone string
      TargetDevice string; RootSizeGiB int; CreateAppdata bool
      Hostname string; Net NetConfig
      AdminUser, AdminPassword string
      ConfirmWipe string `json:"confirmWipe,omitempty"` // nur unbeaufsichtigt
  }
  func Defaults() Answers   // de, de, Europe/Berlin, RootSizeGiB 32, CreateAppdata true, Hostname "betternas", DHCP true
  type FieldError struct{ Field, Message string }   // Message deutsch
  func (a Answers) Validate(knownTimezones func(string) bool) []FieldError
  var Languages = []string{"de","en"}; var Keymaps = []string{"de","de-nodeadkeys","ch","at","us"}
  ```
  JSON-Feldnamen lowerCamelCase (dieselbe Struktur wird als `answers.json` gelesen).
  Regeln: Hostname `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`; AdminUser `^[a-z_][a-z0-9_-]{0,31}$` und nicht in `root, daemon, bin, sys, nobody, betternas, admin`… (vollständige Liste: alle Namen aus Debians `/usr/share/base-passwd/passwd.master` + `betternas`); Passwort ≥ 8 Zeichen, nicht gleich dem Benutzernamen; RootSizeGiB ≥ 16; statische IP: gültiges CIDR, Gateway im selben Netz, mindestens ein DNS; Sprache/Tastatur aus den Listen; Zeitzone über `knownTimezones` (prüft `/usr/share/zoneinfo/<tz>`).

- [ ] **Step 1: Failing tests:** `TestDefaultsAreValidExceptAdmin` (Defaults + Admin `anna`/`sicheres-pw` → keine Fehler); Tabellen-Test `TestValidate` mit je einem Fall pro Regel, erwartet genau einen `FieldError` mit dem betroffenen `Field` (`hostname` `"Bet ter"`, `"-x"`; `adminUser` `"root"`, `"1abc"`; `adminPassword` `"kurz"`, gleich Benutzername; `rootSizeGiB` 15; `net.gateway` `10.0.0.1` bei Adresse `192.168.1.20/24`; `net.address` `"192.168.1.20"` ohne Präfix; `timezone` `"Mars/Base"`).
- [ ] **Step 2:** Run `go test ./internal/installer/` → FAIL. **Step 3:** Implementieren. **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `feat(installer): answers model and validation`.

---

### Task 3: Zielplatten und Partitionsplan

**Files:**
- Create: `backend/internal/installer/targets.go`, `layout.go`, `targets_test.go`, `layout_test.go`, `testdata/lsblk-live-*.json`

**Interfaces:**
- Consumes: `storage.ParseLsblk`, `storage.Disk` (Fundament-Plan Task 10).
- Produces:
  ```go
  type Target struct{ Device, Model, Serial string; SizeBytes uint64; Rotational bool; HasData bool; Warning string }
  func Targets(disks []storage.Disk, liveMediumDevice string) []Target
  func LiveMediumDevice(mountinfo []byte, lsblk []storage.Disk) string   // Disk, deren Partition /run/live/medium trägt
  type PartitionSpec struct{ Number int; Name, TypeCode, FSType, Label, Mount string; SizeMiB uint64 } // SizeMiB 0 = Rest
  type Layout struct{ Device string; Partitions []PartitionSpec }
  func PlanLayout(t Target, a Answers) (Layout, error)
  func PartitionDevice(disk string, n int) string
  ```
  Regeln `Targets`: Live-Medium ausschließen; Größe < 24 GiB ausschließen; Disks mit Partitionen/Dateisystemen → `HasData=true`; HDD → `Warning "HDD als Systemplatte: langsamer, Platte kann nicht schlafen"`; `Manageable` ist hier egal (virtuelle Platten ohne Serial sind gültige Ziele).
  `PlanLayout`: Nummern/Typen/Labels aus Global Constraints; Mounts `-`, `/boot/efi`, `/`, `/srv/appdata`; Appdata entfällt, wenn `!CreateAppdata` oder Rest < 8 GiB – dann erhält Root den Rest (`SizeMiB 0`); Fehler, wenn Root + 513 MiB > Disk.
  `PartitionDevice`: Suffix `p<n>`, wenn der Name auf eine Ziffer endet (`nvme0n1`, `mmcblk0`, `loop0`), sonst `<n>`.

- [ ] **Step 1: Failing tests:**
  - `TestTargetsExcludeLiveMedium`: Fixture (USB-Stick `sdc` trägt `/run/live/medium`, NVMe 500 GB leer, HDD 4 TB mit ext4) → 2 Targets, kein `/dev/sdc`; HDD `HasData && Warning != ""` (Review Focus 1).
  - `TestTargetsExcludeSmall`: 16-GiB-Disk fehlt.
  - `TestPlanLayoutDefault`: 500-GB-Disk → 4 Partitionen, Root `SizeMiB 32768`, Appdata `SizeMiB 0`.
  - `TestPlanLayoutNoAppdataWhenSmall`: 40-GiB-Disk, Root 32 → Rest < 8 GiB → 3 Partitionen, Root `SizeMiB 0`.
  - `TestPlanLayoutTooSmall`: Root 64 auf 40-GiB-Disk → Fehler.
  - `TestPartitionDevice`: `("/dev/sda",3)→"/dev/sda3"`, `("/dev/nvme0n1",2)→"/dev/nvme0n1p2"`, `("/dev/mmcblk0",1)→"/dev/mmcblk0p1"` (Review Focus 3).
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implementieren. **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `feat(installer): target disk selection and partition layout`.

---

### Task 4: Installation ausführen

**Files:**
- Create: `backend/internal/installer/execute.go`, `render.go`, `execute_test.go`, `render_test.go`, `testdata/golden/*`

**Interfaces:**
- Consumes: `sysexec.Runner`, `sysexec.FS`, `Layout`, `Answers`.
- Produces:
  ```go
  type Progress func(step string, percent int)
  type Env struct{ Runner sysexec.Runner; FS sysexec.FS; Squashfs, Target string; UEFI bool; Arch string } // Target = "/target"
  func Execute(ctx context.Context, env Env, l Layout, a Answers, p Progress) error
  func RenderFiles(l Layout, a Answers, uuids map[string]string) (map[string][]byte, error) // Pfad im Ziel → Inhalt; uuids: Label → UUID
  ```
  **Ablauf (exakte Befehle):** `wipefs --all <dev>` · `sgdisk --zap-all <dev>` · je Partition `sgdisk --new=<n>:0:<+SizeMiBM|0> --typecode=<n>:<code> --change-name=<n>:<name> <dev>` · `partprobe <dev>` · `udevadm settle` · `mkfs.vfat -F 32 -n EFI <p2>` · `mkfs.ext4 -F -L bnas-root <p3>` · ggf. `mkfs.ext4 -F -m 0 -L bnas-appdata <p4>` · `blkid -s UUID -o value <pN>` je Dateisystem · `mount <p3> /target` · `mkdir -p /target/boot/efi` + `mount <p2> /target/boot/efi` · `unsquashfs -f -d /target <squashfs>` (Fortschritt 10→70 %) · Dateien aus `RenderFiles` schreiben · Bind-Mounts `/dev`, `/dev/pts`, `/proc`, `/sys`, `/run` (+ `/sys/firmware/efi/efivars` bei UEFI) · in `chroot /target`: `ssh-keygen -A` (die leere `/etc/machine-id` aus dem Image bleibt unangetastet; systemd erzeugt sie beim ersten Boot) · `useradd -m -s /bin/bash -G betternas-admin,sudo <user>` (Gruppe `betternas-admin` vorher `groupadd -f -r betternas-admin`) · `chpasswd` mit Stdin `<user>:<pw>\n` · `smbpasswd -a -s <user>` mit Stdin `<pw>\n<pw>\n` · bei UEFI `grub-install --target=x86_64-efi --efi-directory=/boot/efi --bootloader-id=betternas --uefi-secure-boot` **und** `grub-install --target=x86_64-efi --efi-directory=/boot/efi --removable --uefi-secure-boot` · immer `grub-install --target=i386-pc <dev>` · `update-initramfs -u -k all` · `update-grub` · alle Mounts in umgekehrter Reihenfolge lösen · `sync`. Jeder ExitCode ≠ 0 → Fehler mit Befehl und Stderr; bereits gemountete Pfade werden dennoch ausgehängt. Passwörter erscheinen nie in Fehlermeldungen oder Fortschrittstexten. Target-Namen aus `Arch` ableiten (`amd64` → `x86_64-efi`/`i386-pc`; andere Architekturen → Fehler „nicht unterstützt“ bis zum ARM64-Port).
  **RenderFiles:** `/etc/fstab` (UUID-basiert: `/` ext4 `errors=remount-ro 0 1`, `/boot/efi` vfat `umask=0077 0 1`, `/srv/appdata` ext4 `defaults,noatime 0 2`), `/etc/hostname`, `/etc/hosts` (`127.0.1.1 <host>.local <host>`), `/etc/timezone` + Symlink-Ziel für `/etc/localtime` (als eigener Schritt), `/etc/default/locale` (`LANG=de_DE.UTF-8` bzw. `en_US.UTF-8`), `/etc/default/keyboard` (`XKBLAYOUT`, `XKBVARIANT` – `de-nodeadkeys` → `de`/`nodeadkeys`), `/etc/systemd/network/10-betternas.network` (`[Match] Name=<iface>`; DHCP: `DHCP=yes`; statisch: `Address=`, `Gateway=`, `DNS=` je Eintrag), `/etc/betternas/installed.json` (`{"version","installedAt"}`).

- [ ] **Step 1: Failing tests** (FakeRunner, RootFS in TempDir):
  - `TestExecuteCommandSequenceUEFI`: Defaults auf `/dev/nvme0n1` → `Calls` in obiger Reihenfolge, Partitionspfade `nvme0n1p1…p4`, beide `grub-install --target=x86_64-efi`-Aufrufe vorhanden, darunter einer mit `--removable` (Review Focus 4).
  - `TestExecuteBIOSOnly`: `UEFI=false` → kein `x86_64-efi`, kein efivars-Bind-Mount, `i386-pc` vorhanden.
  - `TestExecuteRegeneratesIdentity`: `ssh-keygen -A` im chroot vorhanden; geschriebene Dateien enthalten keine `/etc/machine-id` (Review Focus 5).
  - `TestExecuteNeverLeaksPassword`: Fake lässt `smbpasswd` mit ExitCode 1 scheitern → Fehlertext und alle Progress-Texte enthalten das Passwort nicht; Unmount-Aufrufe trotzdem in `Calls`.
  - `TestExecuteStopsAfterFailure`: `mkfs.ext4` scheitert → kein `unsquashfs` in `Calls`.
  - `TestRenderFilesGolden`: DHCP-Variante und statische Variante (`192.168.1.20/24`, GW `192.168.1.1`, DNS `192.168.1.1`, `9.9.9.9`) byte-gleich mit Golden-Files; ohne Appdata keine `/srv/appdata`-Zeile in `fstab`.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implementieren. **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `feat(installer): partition, unpack and configure target system`.

---

### Task 5: Installer-Oberfläche (Bubble Tea)

**Files:**
- Create: `backend/internal/installer/tui/model.go`, `steps.go`, `styles.go`, `i18n.go`, `model_test.go`
- Create: `backend/cmd/betternas-installer/main.go`

**Interfaces:**
- Consumes: `Answers`, `Validate`, `Targets`, `PlanLayout`, `Execute`.
- Produces:
  ```go
  type Step int // StepLanguage, StepKeymap, StepTimezone, StepDisk, StepLayout, StepHostname, StepNetwork, StepAdmin, StepSummary, StepInstalling, StepDone, StepFailed
  type Deps struct{ Env installer.Env; Targets []installer.Target; Interfaces []string; ApplyKeymap func(string) error; Reboot func() error; Timezones func(string) bool }
  func New(d Deps) Model   // tea.Model
  ```
  `main.go`: ermittelt Targets (`lsblk` + `/proc/self/mountinfo`), Netzwerkschnittstellen (`/sys/class/net`, ohne `lo`), UEFI (`/sys/firmware/efi` existiert); prüft zuerst `unattended.Find` (Task 6); sonst startet TUI. `ApplyKeymap` = `loadkeys <map>`. Flags `--squashfs /run/live/medium/betternas/rootfs.squashfs --target /target`.
  Verhalten: Schritte in obiger Reihenfolge, `Esc` = zurück, `Enter` = weiter; „weiter“ nur ohne `FieldError` für die Felder des aktuellen Schritts (Meldung unter dem Feld). Passwortfeld maskiert, mit Wiederholung. Zusammenfassung zeigt Zielplatte mit Modell und Größe, Partitionsplan, Hostname, Netzwerk, Benutzer; bei `HasData` Warnung in Rot „Alle Daten auf <Modell> (<Größe>) werden gelöscht“. Auswahl „Installieren“ / „Zurück“, **Fokus standardmäßig auf „Zurück“**. Fortschrittsbalken während `Execute` (läuft als `tea.Cmd`). Fehlerbildschirm mit Meldung und Option „Protokoll anzeigen“ (Pfad `/tmp/betternas-install.log`). Fertig-Bildschirm: „USB-Stick entfernen und Enter drücken“ → `Reboot`.

- [ ] **Step 1: Failing tests** (Model direkt mit `tea.KeyMsg` treiben, kein Terminal):
  - `TestKeymapAppliedBeforeAdminStep`: Tastatur `ch` wählen → `ApplyKeymap("ch")` aufgerufen, bevor `StepAdmin` erreicht wird (Review Focus 2).
  - `TestCannotAdvanceWithInvalidHostname`: Eingabe `"Bet ter"` + Enter → bleibt `StepHostname`, View enthält die Fehlermeldung.
  - `TestSummaryDefaultsToBack`: in `StepSummary` sofort Enter → zurück zu `StepAdmin`, `Execute` nicht aufgerufen (Review Focus 1).
  - `TestSummaryShowsWipeWarning`: Target mit `HasData` → View enthält „werden gelöscht“.
  - `TestPasswordMasked`: View in `StepAdmin` enthält das getippte Passwort nicht.
  - `TestEscGoesBack`: aus `StepTimezone` Esc → `StepKeymap`, Eingaben bleiben erhalten.
- [ ] **Step 2:** Run `go test ./internal/installer/tui/` → FAIL. **Step 3:** Implementieren. **Step 4:** Run → PASS.
- [ ] **Step 5:** Run `make build` (Installer-Binary für amd64 und arm64 baut).
- [ ] **Step 6:** Commit `feat(installer): interactive text UI`.

---

### Task 6: Unbeaufsichtigte Installation

**Files:**
- Create: `backend/internal/installer/unattended.go`, `unattended_test.go`
- Modify: `backend/cmd/betternas-installer/main.go`

**Interfaces:**
- Produces:
  ```go
  func Find(ctx context.Context, r sysexec.Runner, fs sysexec.FS) (path string, found bool, err error) // blkid -L BNASCFG → mount -o ro nach /run/bnascfg → answers.json
  func LoadAnswers(data []byte) (Answers, error)   // JSON, unbekannte Felder → Fehler; fehlende Felder → Defaults()
  func CheckUnattended(a Answers, targets []Target) error
  ```
  `CheckUnattended`: `TargetDevice` muss in `targets` enthalten sein, `ConfirmWipe == TargetDevice`, `Validate` ohne Fehler. Im unbeaufsichtigten Modus gibt `main` Fortschritt als Textzeilen auf stdout und `/dev/ttyS0` aus (`[BetterNAS] <schritt> <prozent>%`), am Ende `[BetterNAS] Installation abgeschlossen` und führt `systemctl reboot` aus; bei Fehler `[BetterNAS] Installation fehlgeschlagen: <msg>` und kein Reboot.

- [ ] **Step 1: Failing tests:** `TestLoadAnswersFillsDefaults` (nur Pflichtfelder → Hostname `betternas`); `TestLoadAnswersRejectsUnknownField` (`"hostnmae"` → Fehler); `TestCheckUnattendedRequiresConfirm` (fehlendes/abweichendes `confirmWipe` → Fehler; Ziel = Live-Medium, also nicht in Targets → Fehler) (Review Focus 1); `TestFindNoDisk` (Fake `blkid -L BNASCFG` ExitCode 2 → `found=false`, kein Fehler).
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implementieren. **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `feat(installer): unattended installation via BNASCFG answers file`.

---

### Task 7: Live-ISO

**Files:**
- Create: `image/build-iso.sh`, `image/packages/live.list`, `image/live/auto/config`, `image/live/config/includes.chroot/etc/systemd/system/getty@tty1.service.d/installer.conf`, `image/live/config/includes.chroot/usr/lib/betternas/installer-session.sh`, `image/live/config/bootloaders/grub-pc/…` und `grub-efi`-Menü (Branding), `image/live/config/bootloaders/syslinux/…`
- Modify: `Makefile` (Target `iso ARCH=amd64 VERSION=…`), `.github/workflows/ci.yml` (Job `image`: Builder-Image, `make rootfs iso`, Artefakt hochladen)

**Interfaces:**
- Consumes: `dist/rootfs-<arch>.squashfs` (Task 1), Installer-Binary (Task 5).
- Produces: `dist/betternas-<version>-<arch>.iso`.
  `auto/config`: `lb config --distribution trixie --architectures $ARCH --archive-areas "main non-free-firmware" --binary-images iso-hybrid --bootloaders "syslinux,grub-efi" --uefi-secure-boot enable --debian-installer none --iso-application BetterNAS --iso-volume BETTERNAS --memtest none --bootappend-live "boot=live components quiet console=tty0 console=ttyS0,115200"`.
  `live.list`: `linux-image-$ARCH`, `live-boot`, `systemd-sysv`, `udev`, `kbd`, `console-setup`, `gdisk`, `parted`, `dosfstools`, `e2fsprogs`, `squashfs-tools`, `util-linux`, `pciutils`, Firmware-Pakete wie in Task 1.
  `build-iso.sh` kopiert Squashfs nach `config/includes.binary/betternas/rootfs.squashfs` und Installer nach `config/includes.chroot/usr/sbin/betternas-installer`, dann `lb build`.
  tty1: Autologin root → `installer-session.sh` startet `betternas-installer`; nach Beenden ohne Reboot zurück in den Installer (Shell nur über tty2 erreichbar, Hinweis im Installer-Footer „Alt+F2: Konsole“). Bootmenü: Eintrag „BetterNAS installieren“ (Standard, Timeout 5 s) und „BetterNAS installieren (abgesicherter Grafikmodus)“ mit `nomodeset`.

- [ ] **Step 1:** Konfiguration und Skripte schreiben.
- [ ] **Step 2:** Run `make iso ARCH=amd64 VERSION=0.0.1-dev` → ISO entsteht; `xorriso -indev dist/betternas-0.0.1-dev-amd64.iso -find /betternas -name rootfs.squashfs` findet die Datei; `xorriso … -find /EFI/boot -name 'bootx64.efi'` findet den Bootloader.
- [ ] **Step 3 (manuell):** ISO in QEMU (UEFI mit OVMF) booten → Bootmenü „BetterNAS installieren“ → Installer-Sprachauswahl erscheint auf tty1.
- [ ] **Step 4:** Commit `feat(image): live ISO with installer autostart`.

---

### Task 8: Ende-zu-Ende-Test in QEMU (BIOS, UEFI, Secure Boot)

**Files:**
- Create: `test/image/e2e.sh`, `test/image/answers.json`, `test/image/README.md`
- Modify: `.github/workflows/ci.yml` (Job `image-e2e`, Matrix `bios`, `uefi`, `secureboot`), `Makefile` (Target `e2e MODE=uefi`)

**Interfaces:**
- Consumes: ISO aus Task 7, unbeaufsichtigter Modus aus Task 6, Banner aus Task 1.
- Produces: `e2e.sh <iso> <bios|uefi|secureboot>`:
  1. Zielplatte `qemu-img create -f qcow2 disk.qcow2 40G`; Konfig-Platte: vfat-Image Label `BNASCFG` mit `answers.json` (Ziel `/dev/vda`, `confirmWipe` `/dev/vda`, Admin `ittest`, DHCP, Sprache `de`).
  2. Installation: `qemu-system-x86_64 -m 2048 -smp 2 -nographic -no-reboot -serial file:install.log -drive file=disk.qcow2,if=virtio -drive file=cfg.img,if=virtio,format=raw -cdrom <iso> -boot d` + Firmware je Modus (`bios`: Standard-SeaBIOS; `uefi`: `OVMF_CODE_4M.fd`/Kopie `OVMF_VARS_4M.fd`; `secureboot`: `OVMF_CODE_4M.secboot.fd`/Kopie `OVMF_VARS_4M.ms.fd`, `-machine q35,smm=on`); Timeout 20 min; Erfolg = `install.log` enthält `[BetterNAS] Installation abgeschlossen`.
  3. Boot der installierten Platte ohne ISO und ohne Konfig-Platte, `-netdev user,id=n0,hostfwd=tcp::2222-:22 -device virtio-net,netdev=n0`, Serial nach `boot.log`; Erfolg = innerhalb 5 min `BetterNAS ist bereit:` in `boot.log`.
  4. `ssh -p 2222 ittest@localhost` per Passwort (via `sshpass`, nur Testumgebung) → `cat /etc/machine-id` nicht leer, `findmnt /srv/appdata` erfolgreich, `id ittest` enthält `betternas-admin`, `pdbedit -L` (via sudo) enthält `ittest`.
  Exit 0 nur, wenn alle Prüfungen bestehen; Logs als CI-Artefakt.

- [ ] **Step 1:** Skript und Antworten schreiben.
- [ ] **Step 2:** Run `make e2e MODE=uefi` → zunächst auftretende Fehler beheben (Fix-Commits gehören zur jeweiligen Komponente).
- [ ] **Step 3:** Run `make e2e MODE=bios` und `make e2e MODE=secureboot` → PASS.
- [ ] **Step 4 (manuell, im README dokumentiert):** ISO per `dd`/Etcher auf echten USB-Stick, Installation auf echter Hardware mit UEFI; danach Mainboard-Bootreihenfolge prüfen, Banner auf dem Monitor, Login per SSH.
- [ ] **Step 5:** Commit `test(image): automated QEMU install and boot test for BIOS, UEFI and Secure Boot`.

---

## Bewusst nicht in diesem Plan

ARM64-ISO (Build-Parameter vorbereitet, Bootloader-Ziele noch nicht), Browser-basierter Installer, Verschlüsselung der Systemplatte, Installation auf RAID1 für die Systemplatte, eigenes APT-Repository (Phase 2).
