# Phase 3 – SnapRAID (optional pro Pool) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ein Pool kann optional durch SnapRAID (1–2 Paritätsplatten) geschützt werden: Paritätsplatten formatieren/übernehmen, SnapRAID pro Pool einrichten, Sync und Scrub per Zeitplan und von Hand, Schutz vor Massenlöschung, Status in der GUI.

**Architecture:** Paritätsplatten sind Datenträger mit Rolle `parity`, eingehängt unter `/srv/parity/<id>` (eigene Mount-Units, gleicher Apply-Weg wie Datenplatten). SnapRAID-Einstellungen liegen in SQLite; der Agent rendert daraus `/etc/betternas/snapraid/<pool>.conf` + `<pool>.json` und systemd-Timer. Sync/Scrub/Diff führt der Agent selbst als Kommandozeilenmodus aus (`betternas-agent snapraid-run <action> <pool> [--force]`), den sowohl die Timer (Template-Services) als auch die GUI (transiente Unit per `systemd-run`) starten; das Ergebnis steht in `/var/lib/betternas/snapraid/<pool>.state.json`, ein `flock` zeigt „läuft“ an. Die API ruft nur feste RPC-Methoden auf.

**Tech Stack:** Go 1.24 (ohne cgo), SQLite, Debian-Paket `snapraid`, systemd-Timer, Vue 3.5 + PrimeVue 4 + Pinia, Vitest.

**Spec:** `docs/specs/2026-11-15-betternas-design.md` (Abschnitte 3, 4.1 „SnapRAID“, 5, 6)

## Global Constraints

- Paritätsplatten: 1–2 pro Pool (`level` 1 oder 2), jede **≥ größte Datenplatte** des Pools (Vergleich über `size_bytes`); Mountpoint `/srv/parity/<disk-id>`, Parität-Datei `snapraid.parity` bzw. `snapraid.2-parity`.
- Eine Paritätsplatte gehört zu höchstens einem Pool und ist nie Pool-Branch.
- Konfiguration: `/etc/betternas/snapraid/<pool>.conf` (Kopfzeile `storage.ManagedHeader`) und `<pool>.json` (Sidecar: `deleteThreshold`, `scrubPercent`, `scrubOlderDays`); Content-Dateien auf **mindestens zwei Datenplatten** (bei nur einer Datenplatte: diese + erste Paritätsplatte) **und** der Systemplatte (`/var/lib/betternas/snapraid/<pool>.content`).
- Zeitpläne als systemd-Timer (`OnCalendar`): Sync Standard `*-*-* 03:00:00`, Scrub Standard `Sun *-*-* 05:00:00`, Scrub 8 %, älter als 10 Tage.
- Massenlöschung: Sync bricht ab, wenn mehr als `deleteThreshold` Dateien gelöscht wurden (Standard 500); die GUI fragt nach, „Trotzdem synchronisieren“ startet mit `--force`.
- Der Lauf bricht ab, wenn ein Datenträger des Pools (Daten oder Parität) nicht eingehängt ist – nie mit fehlender Platte synchronisieren.
- API läuft nie als root; der Agent validiert jede Operation erneut (Poolname `storage.PoolNameRe`, feste Aktionen `sync|scrub|diff`).
- Fehlerformat `{"error":{"code","message","details"}}`; vorhandene Codes aus `internal/apierr`. Alle sichtbaren Texte auf Deutsch.

## Review Focus

1. **Datenplatte aus einem SnapRAID-Pool entfernen:** wird mit `conflict` abgelehnt („SnapRAID zuerst deaktivieren“), sonst bricht der nächste Sync ab bzw. Parität wird ungültig. Test in Task 4 (`TestUpdatePoolRefusesRemovingBranchWithSnapraid`).
2. **Platte des Pools fehlt beim geplanten Sync:** Der Lauf endet mit Status `blocked` und synchronisiert nichts. Test in Task 3 (`TestRunSyncBlockedWhenDiskMissing`).
3. **Massenlöschung (z. B. Platte leer/neu formatiert):** Sync stoppt, Status `blocked` mit Anzahl; erst `--force` synchronisiert. Test in Task 3 (`TestRunSyncBlocksOnMassDelete`, `TestRunSyncForce`).
4. **Zwei Läufe gleichzeitig (Timer + GUI):** Der zweite meldet `conflict`/„läuft bereits“ statt parallel zu laufen. Test in Task 3 (`TestRunRefusesWhenLocked`).
5. **Paritätsplatte zu klein oder Paritätsplatte freigeben/löschen, solange sie benutzt wird:** Aktivieren schlägt mit `invalid_params` fehl, `Release` mit `disk_in_use`. Tests in Task 4 (`TestEnableSnapraidRejectsSmallParity`, `TestReleaseRefusesParityDiskInUse`).

---

### Task 1: Paritätsplatten – Rolle, Mount-Units, Formatieren/Übernehmen

**Files:**
- Modify: `backend/internal/storage/state.go`, `backend/internal/storage/render.go`, `backend/internal/storage/manager.go`, `backend/internal/agent/apply.go`, `backend/internal/agent/format.go`, `backend/internal/api/storage.go`
- Test: `backend/internal/storage/render_test.go`, `backend/internal/storage/manager_test.go`, `backend/internal/agent/storage_test.go`, `backend/internal/api/storage_api_test.go`

**Interfaces:**
- Produces:
  - `storage.DiskMount` bekommt `Role string \`json:"role"\`` (`"data"` | `"parity"`; leer gilt als `"data"`); `func ParityPath(id string) string` → `/srv/parity/<id>`; `func (d DiskMount) Path() string` (Datenplatte → `DiskPath`, Parität → `ParityPath`).
  - `State.Validate`: Rolle muss `data` oder `parity` sein; eine Paritätsplatte darf in keinem Pool-Branch stehen.
  - `storage.FormatRequest` bekommt `Role string \`json:"role,omitempty"\``; `Manager.Format` und `Manager.Import(ctx, actor, id, role string)` übernehmen die Rolle (Standard `data`, sonst `invalid_params`); der DB-Eintrag erhält `role`.
  - Mount-Unit-Name `srv-parity-<id>.mount`; Beschreibung „BetterNAS Paritätsplatte <id>“.
  - `agent.managedUnits` und `Apply` berücksichtigen `srv-parity-*.mount` (Ordnung: Datenplatten/Parität vor Pools; `isPoolUnit` unverändert).
  - API: `POST /api/v1/disks/{id}/format` und `/import` nehmen `role` im Body (Import bisher ohne Body → `role` optional).

- [ ] **Step 1: Tests schreiben** – `TestRenderParityUnit` (Golden `testdata/golden/srv-parity-<id>.mount`: `Where=/srv/parity/<id>`, gleiche Optionen wie Datenplatte, Beschreibung „Paritätsplatte“), `TestValidateRejectsParityInPool`, `TestFormatAsParityStoresRole` (fakeAgent-State enthält `Role:"parity"`, DB-Rolle `parity`), `TestImportWithRole`, in `agent/storage_test.go` `TestApplyWritesParityUnitAndProtectsMountpoint` (FakeRunner: `chattr +i /srv/parity/<id>`), in `storage_api_test.go` `TestFormatPassesRole`.
- [ ] **Step 2: Tests laufen lassen** – `cd backend && go test ./internal/storage ./internal/agent ./internal/api` → FAIL.
- [ ] **Step 3: Implementieren** gemäß Interfaces; `Manager.ListDisks` zeigt `Role` aus der DB (gilt schon), `Pool` bleibt für Paritätsplatten leer (wird in Task 4 durch den SnapRAID-Pool ergänzt). Neues Golden mit `-update`-Muster des vorhandenen Render-Tests.
- [ ] **Step 4: Tests laufen lassen** – `go test ./...` → PASS.
- [ ] **Step 5: Commit** – `feat(storage): parity disks with own role and mount units`

---

### Task 2: Paket `snapraid` – Konfiguration, Timer, Parser

**Files:**
- Create: `backend/internal/snapraid/snapraid.go`, `backend/internal/snapraid/render.go`, `backend/internal/snapraid/parse.go`
- Test: `backend/internal/snapraid/snapraid_test.go`, `backend/internal/snapraid/render_test.go`, `backend/internal/snapraid/parse_test.go`, Golden-Dateien unter `backend/internal/snapraid/testdata/golden/`

**Interfaces:**
- Produces:
  - Konstanten `ConfDir = "/etc/betternas/snapraid"`, `StateDir = "/var/lib/betternas/snapraid"`, `LockDir = "/run/betternas"`, Standardwerte `DefaultSync`, `DefaultScrub`, `DefaultScrubPercent = 8`, `DefaultScrubOlderDays = 10`, `DefaultDeleteThreshold = 500`.
  - `type Settings struct { SyncSchedule, ScrubSchedule string; ScrubPercent, ScrubOlderDays, DeleteThreshold int }` mit `func (s Settings) WithDefaults() Settings` und `func (s Settings) Validate() error` (`OnCalendar` nur `[A-Za-z0-9 *:,./-]{1,64}`; Prozent 1–100; Tage 0–3650; Schwelle 0–1000000; `invalid_params`).
  - `type PoolConfig struct { Pool string; Data []string /*Disk-IDs*/; Parity []string /*Level 1,2 in Reihenfolge*/; Settings }`; `func (p PoolConfig) Validate() error` (Poolname, 1–2 Paritätsplatten, ≥1 Datenplatte, keine doppelten IDs).
  - `type State struct { Pools []PoolConfig }`, `func (s State) Validate() error`.
  - `func Render(s State) (map[string][]byte, error)` → Schlüssel sind **absolute Pfade**: `<ConfDir>/<pool>.conf`, `<ConfDir>/<pool>.json`, `/etc/systemd/system/betternas-snapraid-sync-<pool>.timer`, `/etc/systemd/system/betternas-snapraid-scrub-<pool>.timer`. Alle beginnen mit `storage.ManagedHeader` (JSON: Feld `"managedBy"` statt Kommentar; Timer: Kommentarzeile). `.conf`: `parity`/`2-parity`-Zeilen, `content`-Zeilen laut Global Constraints, `data <id> /srv/disks/<id>/`, `exclude /lost+found/`, `exclude *.unrecoverable`, `exclude /tmp/`. Timer: `[Timer] OnCalendar=…`, `Persistent=true`, `RandomizedDelaySec=300`, `Unit=betternas-snapraid-<action>@<pool>.service`, `[Install] WantedBy=timers.target`.
  - `type Diff struct { Added, Removed, Updated, Moved, Copied, Equal int }`; `func ParseDiff(out []byte) Diff` liest die Zusammenfassungszeilen (`   3 added` …); `func (d Diff) Unprotected() int` = Added+Removed+Updated+Moved+Copied.
  - `type RunState struct { Action, Result, Message string; At time.Time; Removed int }` (Result `ok|errors|failed|blocked`) und `type Status struct { Enabled, Running bool; LastSync, LastScrub *RunState; Diff *Diff; DiffAt *time.Time; Log string }`.

- [ ] **Step 1: Tests schreiben** – `TestSettingsDefaultsAndValidation` (leer → Defaults; `OnCalendar` `"daily; rm -rf /"` → Fehler; Prozent 0 und 101 → Fehler), `TestPoolConfigValidate` (0 und 3 Paritätsplatten, doppelte ID, ungültiger Poolname → Fehler), `TestRenderGolden` (Pool `media`, Daten `d1`,`d2`,`d3`, Parität `p1`,`p2` → vier Golden-Dateien), `TestRenderContentSingleDataDisk` (eine Datenplatte → Content auf ihr, auf der ersten Parität und auf der Systemplatte), `TestParseDiff` (Fixture `testdata/diff.txt` aus echter `snapraid diff`-Ausgabe: 12 equal, 3 added, 1 removed, 2 updated → `Unprotected()==6`).
- [ ] **Step 2: Test laufen lassen** – `cd backend && go test ./internal/snapraid` → FAIL (Paket fehlt).
- [ ] **Step 3: Implementieren** gemäß Interfaces; Dateipfade und Namen nur hier definieren (Agent und Manager importieren sie).
- [ ] **Step 4: Tests** – `go test ./internal/snapraid` → PASS.
- [ ] **Step 5: Commit** – `feat(snapraid): config, timer rendering and diff parsing`

---

### Task 3: Agent – Anwenden, Läufe, Status

**Files:**
- Create: `backend/internal/agent/snapraid.go`
- Modify: `backend/internal/agent/server.go` (RPC-Methoden), `backend/cmd/betternas-agent/main.go` (Unterbefehl), `packaging/systemd/betternas-snapraid-sync@.service`, `packaging/systemd/betternas-snapraid-scrub@.service` (neu), `packaging/build-deb.sh`, `packaging/scripts/postinst.sh` (Verzeichnisse `/etc/betternas/snapraid`, `/var/lib/betternas/snapraid`), `image/packages/rootfs.list` und `Depends` in `build-deb.sh` um `snapraid` erweitern
- Test: `backend/internal/agent/snapraid_test.go`

**Interfaces:**
- Consumes: `snapraid.State`, `snapraid.Render`, `snapraid.ParseDiff`, `snapraid.RunState`, `snapraid.Status`, `storage.PoolPath`, `storage.DiskPath`, `storage.ParityPath`.
- Produces:
  - `func ApplySnapraid(ctx, d Deps, st snapraid.State) (ApplyResult, error)` – prüft, dass Daten- und Paritätsplatten eingehängt sind (`findmnt --mountpoint`, sonst `invalid_params`), schreibt Dateien atomar (nur eigene mit Kopfzeile überschreiben, Rollback wie `ApplyShares`), entfernt veraltete verwaltete Dateien/Timer (`systemctl disable --now` vorher), `systemctl daemon-reload`, `systemctl enable --now <timer>` für neue/geänderte Timer.
  - `func StartSnapraid(ctx, d Deps, pool, action string, force bool) (unit string, err error)` – Aktion `sync|scrub|diff`, Pool muss `<ConfDir>/<pool>.conf` haben; Fehler `conflict`, wenn der Lauf-Lock belegt ist; startet `systemd-run --unit=betternas-snapraid-<pool> --collect /usr/sbin/betternas-agent snapraid-run <action> <pool> [--force]`.
  - `func SnapraidStatus(ctx, d Deps, pool string) (snapraid.Status, error)` – liest `<StateDir>/<pool>.state.json`, `Running` über nicht-blockierenden `flock` auf `<LockDir>/snapraid-<pool>.lock`, `Log` = letzte 40 Zeilen aus `<StateDir>/<pool>.log`.
  - `func RunSnapraid(ctx, d Deps, pool, action string, force bool) error` – Ablauf: Lock nehmen (sonst `conflict`) → Mounts prüfen (fehlt etwas: `RunState{Result:"blocked", Message:"Datenträger <id> fehlt"}`, Fehler) → bei `sync`: `snapraid -c <conf> diff`, `Removed > deleteThreshold && !force` → `blocked` mit `Removed` → sonst `snapraid -c <conf> sync`; bei `scrub`: `scrub -p <percent> -o <days>`; bei `diff`: nur Diff speichern. Ergebnis (`ok` bei Exit 0, `errors` bei Scrub-Exit ≠ 0, `failed` sonst) mit Zeit in `state.json` (`lastSync`, `lastScrub`, `diff`, `diffAt` bleiben erhalten), Ausgabe in `<pool>.log`.
  - RPC: `snapraid.apply`, `snapraid.start` `{pool, action, force}` → `{unit}`, `snapraid.status` `{pool}`; ändernde unter `mutate`-Sperre (`snapraid.start` nicht – es ändert nur über den Lauf-Lock).
  - Kommandozeile: `betternas-agent snapraid-run <sync|scrub|diff> <pool> [--force]` (Exit 0/1, Meldung auf stderr); die Template-Services rufen `snapraid-run sync %i` bzw. `scrub %i`.

- [ ] **Step 1: Tests schreiben** (FakeRunner + Test-FS wie `shares_test.go`; Lock-Datei über temporäres Verzeichnis, `LockDir` als Paket-Variable überschreibbar): `TestApplySnapraidWritesFilesAndEnablesTimers`, `TestApplySnapraidRemovesStaleTimers`, `TestApplySnapraidRefusesUnmountedDisk`, `TestApplySnapraidLeavesForeignFiles`, `TestRunSyncBlockedWhenDiskMissing`, `TestRunSyncBlocksOnMassDelete` (Fake-`snapraid diff` meldet 501 removed bei Schwelle 500 → kein `snapraid sync`-Aufruf, State `blocked`, `Removed==501`), `TestRunSyncForce`, `TestRunSyncOKRecordsState`, `TestRunScrubErrorsRecorded` (Exit 2 → `errors`), `TestRunRefusesWhenLocked`, `TestStartSnapraidRejectsBadInput` (Aktion `rm`, Pool `../x` → `invalid_params`), `TestStartSnapraidUsesTransientUnit` (`systemd-run`, `--unit=betternas-snapraid-media`), `TestSnapraidStatusReadsState`.
- [ ] **Step 2: Tests laufen lassen** – `cd backend && go test ./internal/agent -run Snapraid` → FAIL.
- [ ] **Step 3: Implementieren** gemäß Interfaces; Lock mit `unix.Flock(LOCK_EX|LOCK_NB)` (Datei bleibt offen bis Laufende); Aufruf der Programme ausschließlich über `d.Runner` (`snapraid`, `findmnt`, `systemctl`, `systemd-run`), Dateien über `d.FS`. Unit-Templates:

```ini
[Unit]
Description=BetterNAS SnapRAID %i (Sync)
[Service]
Type=oneshot
ExecStart=/usr/sbin/betternas-agent snapraid-run sync %i
Nice=10
IOSchedulingClass=idle
```
  (Scrub analog mit `scrub`.) `postinst.sh` legt die beiden Verzeichnisse mit `0755` an.
- [ ] **Step 4: Tests laufen lassen** – `go test ./...` → PASS; `go vet ./...` sauber.
- [ ] **Step 5: Commit** – `feat(agent): snapraid apply, runs, status and timers`

---

### Task 4: Manager und API – Einrichten, Starten, Status

**Files:**
- Create: `backend/internal/db/migrations/0003_snapraid.sql`, `backend/internal/storage/snapraid.go`, `backend/internal/api/snapraid.go`
- Modify: `backend/internal/storage/manager.go` (UpdatePool-Sperre, Release-Sperre, ListDisks `Pool`, Reconcile), `backend/internal/api/server.go` (Routen registrieren), `backend/cmd/betternas-api/main.go` (Reconcile-Eintrag „snapraid“)
- Test: `backend/internal/storage/snapraid_test.go`, `backend/internal/api/snapraid_api_test.go`

**Interfaces:**
- Consumes: Agent-RPC `snapraid.apply|start|status`, `snapraid.Settings/State/PoolConfig/Status` (Task 2/3), `Manager.run`, `Manager.currentState`.
- Produces:
  - Migration: `CREATE TABLE snapraid(pool_name TEXT PRIMARY KEY REFERENCES pools(name) ON DELETE CASCADE, sync_schedule TEXT NOT NULL, scrub_schedule TEXT NOT NULL, scrub_percent INTEGER NOT NULL, scrub_older_days INTEGER NOT NULL, delete_threshold INTEGER NOT NULL)` und `CREATE TABLE snapraid_parity(pool_name TEXT NOT NULL REFERENCES pools(name) ON DELETE CASCADE, disk_id TEXT NOT NULL UNIQUE REFERENCES disks(id), level INTEGER NOT NULL CHECK(level IN (1,2)), PRIMARY KEY(pool_name, level))`; `pools.snapraid_enabled` wird mitgepflegt.
  - `type SnapraidInput struct { Parity []string \`json:"parity"\`; snapraid.Settings }` (JSON flach: `parity`, `syncSchedule`, `scrubSchedule`, `scrubPercent`, `scrubOlderDays`, `deleteThreshold`).
  - `func (m *Manager) EnableSnapraid(ctx, actor, pool string, in SnapraidInput) (jobID string, err error)` (auch zum Ändern): prüft Pool existiert, Paritätsplatten `role=parity` und nicht anderweitig belegt, **jede ≥ größte Datenplatte** (`invalid_params` mit Plattennamen), validiert `Settings`; Job `snapraid.enable`: DB schreiben → `snapraid.apply` mit gesamtem SnapRAID-Zustand aller Pools (bei Fehler DB-Transaktion nicht committen).
  - `func (m *Manager) DisableSnapraid(ctx, actor, pool string) (string, error)` – Job `snapraid.disable`; Paritätsplatten bleiben als Rolle `parity` erhalten (Parität-Dateien werden nicht gelöscht).
  - `func (m *Manager) SnapraidStatus(ctx, pool string) (snapraid.Status, error)` (`Enabled` aus DB, Rest vom Agent); `func (m *Manager) RunSnapraid(ctx, actor, pool, action string, force bool) (string, error)` – Job `snapraid.<action>`: `snapraid.start`, dann alle 2 s `snapraid.status` bis `Running==false`, Job-Log übernimmt neue Logzeilen, Job schlägt fehl bei `blocked|failed`, bei `errors` mit Meldung „Scrub hat Fehler gefunden“.
  - `UpdatePool` lehnt Entfernen eines Branches ab, wenn SnapRAID aktiv ist (`conflict`, „SnapRAID zuerst deaktivieren“); Hinzufügen erlaubt und löst `snapraid.apply` aus. `Release` lehnt Paritätsplatten ab, die in `snapraid_parity` stehen (`disk_in_use`). `DeletePool` blockiert nicht, deaktiviert aber SnapRAID vorher im selben Job. `ListDisks` setzt `Pool` bei Paritätsplatten auf den SnapRAID-Pool. `PoolView` bekommt `Snapraid *SnapraidInput \`json:"snapraid,omitempty"\``.
  - API: `PUT /api/v1/pools/{name}/snapraid` (Body `SnapraidInput`), `DELETE /api/v1/pools/{name}/snapraid`, `GET /api/v1/pools/{name}/snapraid/status`, `POST /api/v1/pools/{name}/snapraid/sync` (Body `{"force":bool}`), `POST …/scrub`, `POST …/diff`; alle Änderungen antworten `202 {"jobId"}` über `accepted`.

- [ ] **Step 1: Tests schreiben** (fakeAgent erweitern um `snapraid.*`): `TestEnableSnapraidStoresAndApplies`, `TestEnableSnapraidRejectsSmallParity`, `TestEnableSnapraidRejectsNonParityDisk`, `TestEnableSnapraidRejectsParityUsedElsewhere`, `TestDisableSnapraidApplies`, `TestUpdatePoolRefusesRemovingBranchWithSnapraid`, `TestUpdatePoolAddingBranchReappliesSnapraid`, `TestReleaseRefusesParityDiskInUse`, `TestRunSnapraidFollowsStatusUntilDone` (Status erst `Running`, dann fertig → Job `succeeded`), `TestRunSnapraidBlockedFailsJob`; API: `TestSnapraidEndpointsRequireAuthAndCSRF` und Routing-/Statuscode-Test je Endpunkt.
- [ ] **Step 2: Tests laufen lassen** – `go test ./internal/storage ./internal/api` → FAIL.
- [ ] **Step 3: Implementieren**; `Reconcile` ruft zusätzlich `snapraid.apply` (Agent-Start nach Update erneuert Dateien); Fehler im Log, kein Abbruch des API-Starts.
- [ ] **Step 4: Tests laufen lassen** – `go test ./...` → PASS.
- [ ] **Step 5: Commit** – `feat(storage): snapraid setup, runs and status`

---

### Task 5: Integrationstest auf dem Testgerät

**Files:**
- Create: `backend/internal/agent/snapraid_integration_test.go` (`//go:build integration`)
- Modify: `Makefile` (Testname `TestSnapraidLifecycle` in `make integration` aufnehmen)

**Interfaces:**
- Consumes: `Format`, `Apply`, `ApplySnapraid`, `StartSnapraid`, `RunSnapraid`, `SnapraidStatus`.

- [ ] **Step 1: Test schreiben** – `TestSnapraidLifecycle`: 3 Loop-Geräte à 1 GiB (Muster aus `TestStorageLifecycle`; überspringen ohne root, ohne `snapraid`-Binary oder wenn bereits verwaltete Units/Pools existieren), zwei als Daten, eine als Parität formatieren (`Format` mit Rolle), `Apply`, Pool anlegen, `ApplySnapraid`; 20 Dateien in `/srv/pools/<pool>` schreiben → `RunSnapraid sync` → Status `ok`, `snapraid.parity` existiert, Diff 0; 6 Dateien löschen bei `deleteThreshold=5` → `sync` ergibt `blocked` mit `Removed==6`, `sync --force` ergibt `ok`; Scrub → `ok`. Cleanup entfernt Units/Dateien/Loops wie im vorhandenen Test.
- [ ] **Step 2: Lokal kompilieren** – `cd backend && GOOS=linux GOARCH=amd64 go vet -tags integration ./internal/agent` → sauber.
- [ ] **Step 3: Auf dem Testgerät ausführen** – `make integration` → `TestSnapraidLifecycle` PASS (auf `betternas-dev` ist der echte Pool `test` vorhanden: der Test überspringt dort laut Schutz – dann vorher temporär auf einer sauberen Umgebung/QEMU laufen lassen und das Ergebnis im Ledger festhalten).
- [ ] **Step 4: Commit** – `test(agent): snapraid integration scenario`

---

### Task 6: Frontend – Paritätsplatten, SnapRAID-Panel, Dashboard

**Files:**
- Create: `frontend/src/components/storage/SnapraidPanel.vue`, `frontend/src/components/storage/SnapraidDialog.vue`
- Modify: `frontend/src/api/storage.ts`, `frontend/src/components/storage/FormatDialog.vue` (Rollen-Auswahl „Datenplatte | Paritätsplatte“), Pool-Ansicht unter `frontend/src/views/storage/` (Panel je Pool einbinden), `frontend/src/views/DashboardView.vue` (SnapRAID-Kachel)
- Test: `frontend/src/__tests__/snapraid.test.ts`, `frontend/src/__tests__/storage.test.ts` (Rolle), `frontend/src/__tests__/fixtures.ts`

**Interfaces:**
- Consumes: API aus Task 4.
- Produces: Typen und Funktionen in `api/storage.ts`: `SnapraidInput`, `SnapraidStatus` (`enabled`, `running`, `lastSync`, `lastScrub`, `diff`, `diffAt`, `log`), `RunState`; `enableSnapraid(pool, input)`, `disableSnapraid(pool)`, `snapraidStatus(pool)`, `runSnapraid(pool, 'sync'|'scrub'|'diff', force?)`; `formatDisk`/`importDisk` nehmen `role`.

- [ ] **Step 1: Tests schreiben** – `FormatDialog` sendet `role: 'parity'`, wenn „Paritätsplatte“ gewählt; `SnapraidDialog`: nur Platten mit Rolle `parity` und freiem Status auswählbar, mehr als 2 nicht wählbar, zu kleine Platte zeigt Warnung (Größe < größte Datenplatte), „Zeitplan“-Felder mit Standardwerten (`*-*-* 03:00:00`, 8 %, 10 Tage, 500); `SnapraidPanel`: zeigt „Letzter Sync“, „Ungeschützte Änderungen“, bei `blocked` mit `removed` den Text „Sync gestoppt: N Dateien gelöscht“ und die Schaltfläche „Trotzdem synchronisieren“ (sendet `force:true`), Schaltflächen „Jetzt synchronisieren/prüfen“ deaktiviert, solange `running`; Status wird alle 5 s abgefragt, solange `running`.
- [ ] **Step 2: Tests laufen lassen** – `cd frontend && npx vitest run` → FAIL.
- [ ] **Step 3: Implementieren** (alle Texte deutsch; bestehende PrimeVue-Muster der Pool-Ansicht verwenden; Entfernen von Branches im `PoolDialog` bei aktivem SnapRAID deaktivieren mit Hinweis „SnapRAID zuerst deaktivieren“).
- [ ] **Step 4: Tests, Typprüfung, Build** – `npx vitest run && npx vue-tsc --noEmit && npm run build` → PASS.
- [ ] **Step 5: Commit** – `feat(web): parity disks and snapraid panel`

---

### Task 7: Abschluss – Doku, Release-Stand, Gesamttests

**Files:**
- Modify: `README.md`/`docs/` (Abschnitt SnapRAID: Voraussetzungen, Zeitpläne, Massenlöschungsschutz, Wiederherstellung folgt in Phase 6), `docs/specs/2026-11-15-betternas-design.md` (Phase 3 als umgesetzt markieren, Abweichung „Läufe über Unit + Lock statt Job-RPC“ festhalten), Versionsdatei auf 0.2.0

- [ ] **Step 1: Alles testen** – `make test` (Go + Web) → PASS; `make build` → Binaries für amd64 und arm64.
- [ ] **Step 2: Auf dem Testgerät** per `make remote`/Paket-Update einspielen und `make integration` laufen lassen; manueller Rundgang mit zweiter Platte, falls vorhanden (sonst im Ledger vermerken, dass nur der Loop-Test lief).
- [ ] **Step 3: Doku und Version** aktualisieren; Commit – `docs: snapraid (phase 3), version 0.2.0`
- [ ] **Step 4: Gedächtnis** `betternas-status.md` aktualisieren; Branch mit „In main mergen“ abschließen (finishing-a-development-branch).
