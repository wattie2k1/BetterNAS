# Phase 5b – KVM: Konsole, Snapshots, Klonen, ISO-Upload, LAN-Netz – Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (inline). Steps use checkbox syntax.

**Goal:** Die VM-Seite bekommt eine noVNC-Konsole im Browser, Snapshots (anlegen, zurücksetzen, löschen), Klonen, ISO-/Image-Upload (und Löschen) sowie ein LAN-Netz ohne Eingriff in die Host-Netzkonfiguration.

**Architecture (Abweichungen von Spec §4.5, bewusst):**
- **Konsole:** VNC lauscht nur auf `127.0.0.1` (5a). Der Agent liefert per `virsh domdisplay` nur den Port (`vm.display`); die API (gleicher Host) verbindet sich selbst per TCP auf `127.0.0.1:<port>` und reicht die Bytes als binäre WebSocket-Frames weiter (`GET /api/v1/vms/{name}/console`, Subprotokoll `binary`, Sitzungs-Cookie wie alle WebSockets). Kein Agent-Stream nötig, kein VNC-Passwort (nur lokal erreichbar, Zugriff nur nach Login, Audit-Eintrag). Frontend: `@novnc/novnc` (lazy geladen).
- **Snapshots / Klonen:** `virsh snapshot-*` bzw. `virt-clone` (Paket `virtinst`) über `sysexec.Runner`; Klonen nur ausgeschaltet, nur Disks unter `/srv/vms/disks`.
- **ISO-Upload:** Die API schreibt den Upload (Streaming, kein JSON) direkt nach `/srv/vms/iso` (Verzeichnis `betternas:betternas 0775`, `ReadWritePaths` in der Unit), erst als `.upload-*`-Tempdatei, dann atomar umbenannt, nie überschreibend. Löschen läuft über den Agent.
- **LAN-Netz statt `br0`:** Statt die Host-Netzkonfiguration umzubauen (nur-SSH-Testgerät, kein sicherer Rückfall) nutzt der Assistent macvtap (`<interface type='direct'>` auf einer physischen Schnittstelle, Modus `bridge`): VMs erscheinen mit eigener IP im LAN, null Eingriff in den Host. Einschränkung: Host ↔ VM direkt nicht erreichbar (dafür das NAT-Netz). Netzwert `direct:<schnittstelle>`.

## Global Constraints
- Snapshot-Name `^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$` (`vm.SnapshotRe`), VM-Name `vm.DomainRe`; keine freien Pfade.
- Klon-Name `vm.NameRe`; Quelle muss „aus“ sein; Disks außerhalb `/srv/vms/disks` → `conflict`.
- Upload: Name `ISORe`/`ImageRe`, max. 50 GiB, existierende Datei → `conflict`, Abbruch löscht die Tempdatei.
- ISO löschen: nur Dateien passend zu `ISORe`/`ImageRe`; von einer VM (inaktive XML) referenziert → `conflict`.
- Konsole nur für laufende VMs; jede Sitzung wird im Audit-Log vermerkt.
- `direct:<if>`: Schnittstelle `^[a-z0-9][a-z0-9._-]{0,14}$` und in `/sys/class/net` als physisch gelistet.

## Tasks
- [x] 1 Paket `internal/vm`: `SnapshotRe`, `Snapshot`, `ParseSnapshots`, `ParseDisplayPort`, `direct:`-Netz in Spec/XML (Tests)
- [x] 2 Agent: `vm.snapshots|snapshot.create|revert|delete`, `vm.clone`, `vm.display`, `vm.iso.delete`, `vm.interfaces` (Tests)
- [x] 3 API/Manager: Routen + Audit, Konsolen-Proxy, ISO-Upload/-Löschen (Tests)
- [x] 4 Packaging: `virtinst`, Verzeichnisrechte, `ReadWritePaths`
- [x] 5 GUI: Konsole (noVNC), Snapshots, Klonen, Upload, LAN-Netz im Assistenten (Tests)
- [x] 6 Integrationstest, Ausrollen (0.4.0-rc4), Memory/Spec §8
