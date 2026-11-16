# Phase 5a – KVM: Einrichtung, VM-Liste, Betrieb, Anlege-Assistent – Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (inline) oder superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** libvirt/KVM ist im Image/Paket enthalten und eingerichtet; in der GUI lassen sich virtuelle Maschinen auflisten, über einen Assistenten anlegen (Linux/Windows, UEFI/BIOS, TPM, qcow2-Disk, ISO, NAT-Netz), starten, herunterfahren, hart ausschalten, neu starten, auf Autostart stellen und löschen (mit/ohne Disks). **Etappe 5b** (eigener Plan): noVNC-Konsole, Snapshots, Klonen, ISO-Upload, Bridge `br0` (Opt-in, mit Rückfall).

**Architecture (Abweichung von Spec §4.5, bewusst):** Statt `go-libvirt` ruft der Agent die CLI `virsh -c qemu:///system` (und `qemu-img`) über `sysexec.Runner` auf – dasselbe Muster wie bei Docker (4a): keine neue Go-Abhängigkeit, mit `FakeRunner` testbar, die API bekommt keinen Zugriff auf `libvirt-sock` (Gruppe `libvirt` ist root-äquivalent). Die Domain-XML erzeugt das Paket `internal/vm` aus einer validierten `Spec` (nie Nutzer-XML) und übergibt sie per stdin an `virsh define /dev/stdin`. libvirt ist die Wahrheit (keine DB-Tabelle); Auflistung per `virsh list --all --name` + `dominfo`. Die GUI aktualisiert per Poll (5 s) und über das WebSocket-Ereignis `vm.changed`, das die API nach jeder ändernden Aktion sendet. Muster: `internal/docker`, `agent/docker*.go`, `api/docker.go`, `stores/docker.ts`, `views/docker/ContainersView.vue`.

**Tech Stack:** Go 1.24 (kein cgo), Debian-Pakete `libvirt-daemon-system`, `libvirt-clients`, `qemu-system-x86`, `qemu-utils`, `ovmf`, `swtpm`, `swtpm-tools`; Vue 3.5 + PrimeVue 4 + Pinia, Vitest.

**Spec:** `docs/specs/2026-11-15-betternas-design.md` §3, §4.5, §6.

## Global Constraints

- VM-Name `^[a-z0-9][a-z0-9_-]{0,39}$` (`vm.NameRe`); der Agent validiert jeden Namen erneut; Dateipfade entstehen nur aus `vm.DiskPath(name)` = `/srv/vms/disks/<name>.qcow2` und `vm.ISOPath(file)` = `/srv/vms/iso/<file>` mit `file` `^[A-Za-z0-9][A-Za-z0-9._-]{0,99}\.iso$` – nie aus freien Pfaden.
- Grenzen: vCPU 1–64, RAM 256–262144 MiB, Disk 1–65536 GiB (qcow2, sparse; `qemu-img create -f qcow2`), `os` ∈ `linux|windows`, `firmware` ∈ `uefi|bios`, Netz = Name eines aktiven libvirt-Netzes (Standard `default`, NAT).
- Aktionen `start|shutdown|destroy|reboot|autostart|noautostart`; Löschen nur im Zustand „aus“ (sonst `conflict`); Löschen mit Disks entfernt ausschließlich Dateien unter `/srv/vms/disks/`, die in der Domain-XML als `device='disk'` stehen – nie ISOs, nie Pfade außerhalb.
- Domain-Typ `kvm`, wenn `/dev/kvm` existiert, sonst `qemu` (Software-Emulation; GUI warnt „keine Hardware-Beschleunigung“). UEFI über `<os firmware='efi'>`, TPM 2.0 (`swtpm`, emulated) nur bei `tpm: true` (Windows-11-Standard in der GUI vorbelegt). Windows: Datenträger/NIC `sata`/`e1000e`, solange keine `driverIso` gewählt ist; mit `driverIso` (zweites CD-Laufwerk) `virtio`.
- Grafik: VNC nur `127.0.0.1`, Autoport (Konsole folgt in 5b). Keine Passthrough-/USB-Geräte.
- `/srv/vms/{disks,iso}` (0755, root) liegen bewusst nicht auf mergerfs (Spec §3); libvirt vergibt die Dateirechte beim Start selbst (`dynamic_ownership`).
- Fehlerformat `{"error":{"code","message","details"}}`, Codes aus `internal/apierr`; alle sichtbaren Texte deutsch; jede ändernde API-Aktion schreibt einen Audit-Eintrag (`audit.Record`) und sendet `vm.changed`.

## Review Focus

1. Namens-/Pfadinjektion: `..`, `/`, Großbuchstaben, Leerzeichen, `;`, XML-Sonderzeichen in Name und ISO-Dateiname werden abgelehnt (`TestSpecValidation`); die erzeugte XML enthält nie ungefilterte Nutzerwerte (`TestBuildXMLEscapes`).
2. Löschen darf nie fremde Dateien entfernen: Disk außerhalb `/srv/vms/disks/` oder ISO bleibt erhalten (`TestDeleteOnlyOwnDisks`).
3. Laufende VM löschen → `conflict`, nichts wird entfernt (`TestDeleteRefusesRunning`).
4. Anlegen mit bereits vorhandenem Namen oder Disk-Datei überschreibt nichts (`TestCreateRefusesExisting`); schlägt `virsh define` fehl, wird die frisch angelegte Disk wieder entfernt (`TestCreateCleansUpDisk`).
5. libvirt nicht installiert/aktiv: `vm.status` meldet es, `vm.list` liefert `[]` statt Fehler; GUI zeigt einen Hinweis statt einer leeren Seite (`vms-views.test.ts`).

---

### Task 1: libvirt installieren und einrichten

**Files:** Modify `image/packages/rootfs.list`, `packaging/build-deb.sh`, `packaging/scripts/postinst.sh`, `image/tests/check-rootfs.sh`.

- [ ] **Step 1:** Pakete (Tech Stack) in `rootfs.list` und in `Depends` von `build-deb.sh`; `postinst.sh`: `install -d -m 0755 /srv/vms/disks /srv/vms/iso`; Dienste `libvirtd.socket`/`libvirtd.service` aktivieren, `virsh net-autostart default` und `virsh net-start default` (jeweils `|| true`, nur wenn `virsh` vorhanden und Netz noch nicht aktiv).
- [ ] **Step 2:** `sh -n` auf die Skripte; `check-rootfs.sh` prüft zusätzlich `virsh`, `qemu-img`, `qemu-system-x86_64` und eine OVMF-Datei unter `/usr/share/OVMF`.
- [ ] **Step 3: Commit** – `feat(packaging): libvirt, qemu and ovmf`

### Task 2: Paket `internal/vm` – Namen, Spec, XML, Parser

**Files:** Create `backend/internal/vm/vm.go`, `xml.go`, `parse.go`; Test `vm_test.go`, `xml_test.go`, `parse_test.go`, `testdata/dominfo.txt`, `testdata/dumpxml.xml`, `testdata/net-list.txt`.

**Interfaces (Produces):**
- `NameRe`, `ISORe`, `DiskDir = "/srv/vms/disks"`, `ISODir = "/srv/vms/iso"`, Grenzen als Konstanten, `Actions` (Map); `func DiskPath(name string) string`, `func ISOPath(file string) string`.
- `type Spec struct { Name, OS, Firmware, Network, ISO, DriverISO string; VCPUs, MemoryMiB, DiskGiB int; TPM bool }` (JSON-Tags kleingeschrieben/camelCase: `name,os,firmware,network,iso,driverIso,vcpus,memoryMiB,diskGiB,tpm`); `func (s Spec) Validate() error` (liefert `apierr.InvalidParams` mit deutschem Text).
- `func BuildXML(s Spec, kvm bool) (string, error)` – validiert zuerst; escaped alle Werte (`encoding/xml`-Escape); UEFI → `<os firmware='efi'>`, TPM → `<tpm model='tpm-crb'><backend type='emulator' version='2.0'/></tpm>`.
- `type VM struct { Name, UUID, State string; VCPUs, MemoryMiB int; Autostart bool; Disks []string }` (`State`: `running|shutoff|paused|shuttingdown|crashed|other`).
- `func ParseDomInfo(out []byte) (VM, error)` (`virsh dominfo`: Felder `Name`, `UUID`, `State`, `CPU(s)`, `Max memory` in KiB, `Autostart`); `func ParseDisks(xml []byte) ([]string, error)` (nur `<disk device='disk'>` mit `<source file=…>`); `func ParseNetworks(out []byte) ([]string, error)` (`virsh net-list --name`, nur nicht-leere Zeilen).
- `type Status struct { Installed, Active, KVM bool }`.

- [ ] **Step 1: Tests** – `TestSpecValidation` (gültig: Linux/UEFI/NAT; ungültig: Namen `""`, `../x`, `A`, `a b`, `a;b`, 41 Zeichen; vCPU 0 und 65; RAM 255; Disk 0 und 65537; `os: bsd`; ISO `../x.iso`, `x.img`; Netz mit Leerzeichen), `TestBuildXMLEscapes` (`<`/`&`/`'` in Netzname wird verworfen oder escaped – nie roh in der Ausgabe), `TestBuildXMLVariants` (Windows ohne Treiber-ISO → `bus='sata'` + `e1000e`; mit `driverIso` → `virtio`, zwei `cdrom`-Geräte; BIOS → kein `firmware='efi'`; `kvm=false` → `type='qemu'`; TPM nur bei `tpm:true`; VNC `listen='127.0.0.1'`), `TestParseDomInfo`, `TestParseDisks` (CD-ROM mit ISO wird ausgelassen), `TestParseNetworks`.
- [ ] **Step 2:** `cd backend && go test ./internal/vm` → FAIL. **Step 3:** implementieren (Spec-Werte aus den Global Constraints). **Step 4:** PASS.
- [ ] **Step 5: Commit** – `feat(vm): names, spec validation, domain xml and virsh parsers`

### Task 3: Agent – VM-Operationen

**Files:** Create `backend/internal/agent/vm.go`; Modify `agent/server.go` (Registrierung); Test `agent/vm_test.go` (`FakeRunner` + `FakeFS`).

**Interfaces (Consumes):** `vm.*` aus Task 2. **Produces (RPC):**
- `vm.status` → `vm.Status` (`virsh --version` erfolgreich → `Installed`; `virsh -c qemu:///system list --name` Exit 0 → `Active`; `KVM` = `/dev/kvm` existiert).
- `vm.list` → `[]vm.VM` (Namen aus `virsh list --all --name`, je Name `dominfo`; libvirt nicht erreichbar → `[]`).
- `vm.networks` → `[]string`; `vm.isos` → `[]string` (Glob `/srv/vms/iso/*.iso`, nur Dateinamen passend zu `ISORe`, sortiert).
- `vm.create` (`vm.Spec` + `start bool`) → `{}`: validieren; Name nicht vorhanden (`dominfo` Exit ≠ 0) und Disk-Datei nicht vorhanden, sonst `conflict`; ISO/Treiber-ISO müssen existieren (`not_found`); Netz muss in `vm.networks` stehen; `qemu-img create -f qcow2 <disk> <N>G`; `virsh define /dev/stdin` mit `BuildXML`; schlägt `define` fehl → Disk wieder entfernen (Review 4); optional `virsh start`. Mit `mutate`-Sperre.
- `vm.action` (`{name, action}`) → `{}` (`autostart` → `virsh autostart <name>`, `noautostart` → `virsh autostart --disable <name>`; `shutdown`/`destroy`/`reboot`/`start` direkt).
- `vm.delete` (`{name, deleteDisks bool}`) → `{}`: nicht gefunden → `not_found`; Zustand ≠ „aus“ → `conflict`; vor dem Undefine `virsh dumpxml` lesen; `virsh undefine <name> --nvram --tpm`; danach bei `deleteDisks` jede Disk entfernen, deren Pfad mit `/srv/vms/disks/` beginnt (Review 2; `path.Clean`, kein `..`).

- [ ] **Step 1: Tests** – `TestVMStatus`, `TestVMListEmptyWhenLibvirtDown`, `TestCreateBuildsDiskAndDomain` (Reihenfolge `qemu-img create` → `virsh define`, XML via stdin enthält Namen), `TestCreateRefusesExisting`, `TestCreateCleansUpDisk`, `TestCreateRequiresExistingISO`, `TestActionWhitelist` (unbekannte Aktion/ungültiger Name → `invalid_params`, es wird nichts ausgeführt), `TestDeleteRefusesRunning`, `TestDeleteOnlyOwnDisks` (XML mit Disk in `/srv/vms/disks/x.qcow2` und `/etc/passwd` → nur erste wird entfernt).
- [ ] **Step 2:** `go test ./internal/agent -run 'VM|Create|Delete|Action'` → FAIL. **Step 3:** implementieren; Fehler von `virsh` als `apierr.Internal` mit `details.detail` (stderr), nicht erreichbares libvirt als „libvirt ist nicht verfügbar“. **Step 4:** PASS. **Step 5: Commit** – `feat(agent): vm status, list, create, actions, delete`

### Task 4: API – Manager und Routen

**Files:** Create `backend/internal/vm/manager.go`, `backend/internal/api/vms.go`; Modify `api/server.go` (`Deps.VMs *vm.Manager`, `registerVMs`), `cmd/betternas-api/main.go`; Test `vm/manager_test.go`, `api/vms_api_test.go` (Muster `docker_api_test.go`).

**Interfaces:** `vm.NewManager(db *sql.DB, agent Agent, hub *events.Hub) *Manager` (`Agent` wie in `docker.Manager`); Methoden `Status`, `List`, `Networks`, `ISOs`, `Create(ctx, actor, Spec, start bool)`, `Action(ctx, actor, name, action)`, `Delete(ctx, actor, name, deleteDisks bool)`; jede ändernde Methode: Audit (`vm.create|vm.<action>|vm.delete`, Target = Name) + Event `vm.changed`. Routen: `GET /api/v1/vms/status`, `GET /api/v1/vms`, `GET /api/v1/vms/networks`, `GET /api/v1/vms/isos`, `POST /api/v1/vms` (Body `Spec` + `start`), `POST /api/v1/vms/{name}/{action}`, `DELETE /api/v1/vms/{name}?disks=1`; Body-Limit 64 KiB.

- [ ] **Step 1: Tests** – `TestManagerAudits` (Erfolg/Fehler schreiben Audit; nur Erfolg sendet `vm.changed`), `TestVMRoutesRequireAuth`, `TestCreateRoute` (Spec wird 1:1 an `vm.create` gereicht, ungültiger Name → 422-Fehlerformat), `TestDeleteRouteDisksFlag`.
- [ ] **Step 2:** FAIL → **Step 3:** implementieren → **Step 4:** `cd backend && go vet ./... && go test ./...` PASS. **Step 5: Commit** – `feat(api): vm routes and manager`

### Task 5: GUI – Store, Liste, Aktionen

**Files:** Create `frontend/src/api/vms.ts`, `stores/vms.ts`, `views/vms/VmsView.vue`; Modify `router.ts` (`/vms`, Titel „Virtuelle Maschinen“), `components/AppLayout.vue` (Dock-Eintrag `pi pi-desktop`, Farbe `['#f97316', '#fb923c']`, nach „Docker-Ressourcen“), `stores`-Ereignisbehandlung in `AppLayout.vue` (`vmsStore.handleEvent`); Test `__tests__/vms-store.test.ts`, `__tests__/vms-views.test.ts`.

**Interfaces:** `vmsApi` (`status/list/networks/isos/create/action/remove`) mit Typen `VmInfo`, `VmSpec`, `VmStatus`, `VmAction`; Store `useVmsStore` (`vms`, `status`, `loading`, `error`, `refresh()`, `act(name, action)`, `remove(name, disks)`, `handleEvent(msg)` → `refresh` bei `vm.changed`). View: Kartenraster wie Container-Seite (Name, Zustandspunkt grün/grau/gelb, vCPU · RAM, Autostart-Schalter, Menü: Starten / Herunterfahren / Hart ausschalten / Neu starten / Löschen); Bestätigungsdialog Löschen mit Checkbox „Datenträger ebenfalls löschen“; Hinweisbanner, wenn libvirt nicht aktiv ist bzw. `kvm` fehlt („keine Hardware-Beschleunigung“); Poll alle 5 s nur solange die Seite offen ist; Leerzustand „Noch keine virtuelle Maschine“.

- [ ] **Step 1: Tests** – Store: `refresh` füllt Liste, `handleEvent` löst Refresh nur bei `vm.changed` aus, `act` ruft die API und lädt neu; View: Karten je VM, Aktionsmenü ruft `act` mit richtiger Aktion, Löschdialog sendet `disks=true` nur bei gesetzter Checkbox, Banner bei `active:false`.
- [ ] **Step 2:** `cd frontend && npx vitest run` → FAIL. **Step 3:** implementieren (ResizeObserver-Stub ist in `src/test/setup.ts`). **Step 4:** PASS; `npx vue-tsc --noEmit` sauber. **Step 5: Commit** – `feat(web): vm list and actions`

### Task 6: GUI – Anlege-Assistent

**Files:** Create `frontend/src/components/vms/VmWizard.vue`; Modify `views/vms/VmsView.vue` (Button „Neue VM“); Test `__tests__/vms-wizard.test.ts`.

**Interfaces:** Dialog in drei Schritten (PrimeVue `Stepper`): ① Name, Betriebssystem (Linux/Windows), Firmware (UEFI vorbelegt; bei Windows zusätzlich TPM-Schalter, vorbelegt an); ② vCPU (Standard 2), RAM in GiB (Standard 4), Disk in GiB (Standard 40 Linux/80 Windows); ③ ISO (Auswahl aus `isos`; Hinweis „ISO-Dateien unter /srv/vms/iso ablegen – Upload folgt“, wenn die Liste leer ist), bei Windows optional Treiber-ISO, Netzwerk (Auswahl aus `networks`, Standard `default`), Schalter „Nach dem Anlegen starten“. Clientseitige Validierung spiegelt `Spec.Validate` (gleiche Grenzen, gleiche Namensregel); Fehler der API werden im Dialog angezeigt; bei Erfolg schließt er und die Liste lädt neu.

- [ ] **Step 1: Tests** – Standardwerte je OS, ungültiger Name sperrt „Weiter“, Absenden ruft `vmsApi.create` mit exakt der erwarteten `VmSpec` (+ `start`), API-Fehler bleibt sichtbar, Windows zeigt TPM/Treiber-ISO, Linux nicht.
- [ ] **Step 2:** FAIL → **Step 3:** implementieren → **Step 4:** PASS (alle Vitest + `vue-tsc`). **Step 5: Commit** – `feat(web): vm creation wizard`

### Task 7: Integrationstest, Ausrollen, Doku

**Files:** Create `backend/internal/agent/vm_integration_test.go` (Build-Tag/Skip-Muster wie `docker_integration_test.go`); Modify `Makefile` (`integration`-Liste um `TestVMLifecycle`), `docs/specs/2026-11-15-betternas-design.md` (§4.5/§8: Phase-5a-Abweichung virsh-CLI statt go-libvirt, Tabellenzeile 5 → 5a/5b).

- [ ] **Step 1:** `TestVMLifecycle` (überspringt ohne root, `virsh` oder `qemu-img`): Spec Linux/BIOS, 1 vCPU, 256 MiB, 1 GiB, ohne ISO → `vm.create` mit `start:true` → `vm.list` zeigt `running` → `vm.action destroy` → `shutoff` → `vm.delete` mit Disks → VM und `/srv/vms/disks/<name>.qcow2` weg; Testname-Präfix `bnas-it-`, Aufräumen per `t.Cleanup`.
- [ ] **Step 2:** `make test` (Go + Vitest) grün; `make build web deb VERSION=0.4.0-rc1`; `make deploy VERSION=0.4.0-rc1` (installiert libvirt-Pakete auf dem Testgerät); `make integration` – `TestVMLifecycle` läuft durch; `virsh net-list` zeigt `default` aktiv.
- [ ] **Step 3:** GUI-Prüfung im Browser macht der Nutzer (Login-Passwort); Mock-Server-Vorschau nur, wenn gewünscht.
- [ ] **Step 4: Commit** – `test(agent): vm lifecycle integration test; docs: kvm phase 5a`
