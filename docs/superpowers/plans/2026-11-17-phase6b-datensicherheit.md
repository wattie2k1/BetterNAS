# Phase 6b – Datensicherheit: Konfigurations-Backup/-Restore und SnapRAID-Plattenersatz – Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (inline). Steps use checkbox syntax.

**Goal:** (A) Die BetterNAS-Konfiguration lässt sich als Archiv sichern (automatisch jede Nacht und per Knopf, Download) und auf demselben oder einem frisch installierten NAS wiederherstellen. (B) Ein geführter Assistent ersetzt eine ausgefallene SnapRAID-Datenplatte durch eine neue und stellt deren Dateien per `snapraid fix` wieder her.

**Architecture (Entscheidungen, Spec §4.1 / §4.6 / §8):**
- **Backup = DB-Schnappschuss.** Alles unter `/etc/betternas/snapraid`, die Mount-Units, Samba-Dateien und SnapRAID-Timer werden beim Start aus der DB erzeugt (`Reconcile`); gesichert wird deshalb nur die SQLite-DB (konsistente Kopie per `VACUUM INTO`, Tabellen `sessions` und `jobs` geleert) plus `manifest.json` (`format`, `createdAt`, `version`, `schemaVersion`, `host`, Zähler). Archiv `betternas-backup-YYYYMMDD-HHMMSS.tar.gz` in `/var/lib/betternas/backups` (7 Stück bleiben). **Nicht enthalten:** TLS-Schlüssel (wird neu erzeugt), Benutzer/Gruppen/Samba-Passwörter (Systemkonten), Docker-Stacks und Nutzdaten (liegen auf den Platten bzw. in `/srv/appdata`).
- **Restore = „beim Start anwenden“.** Die laufende DB wird nie im Betrieb ersetzt: Upload/Auswahl → Prüfung und Vorschau → Bestätigung → Datei nach `/var/lib/betternas/restore/pending.db` (+ `pending.json`) → Neustart der API über den Agent (`system.restart-api`, verzögert per `systemd-run --on-active=2`) → `backup.ApplyPending` vor `db.Open` tauscht die DB (alte bleibt als `betternas.db.pre-restore-<zeit>`, 3 Stück) → `Reconcile` stellt Mounts, Freigaben und SnapRAID aus der DB wieder her. Alle Sitzungen sind danach ungültig.
- **Plattenersatz:** SnapRAID kennt Platten über ihren *Namen* in `.conf`/Content-Datei. Eine neue Platte bekommt eine neue ID, muss aber unter dem *alten Namen* geführt werden. Neue Tabelle `snapraid_alias(pool_name, disk_id, name)`; `PoolConfig.Names` (Platten-ID → SnapRAID-Name) steuert `data <name> <pfad>` in der `.conf`. Ablauf (Job): DB-Tausch der Branch-Position und Alias → `storage.apply` → `snapraid.apply` → Marker `<pool>.fixing` → `snapraid -c … -d <name> fix` → Marker weg → Sync. Solange der Marker liegt, verweigern Sync/Scrub (`blocked`, „Wiederherstellung offen“), damit ein Zeitplan nie eine leere Platte als Löschung einträgt. Fehlgeschlagene Wiederherstellung: wiederholbar (`fix`) oder abbrechbar (Marker löschen, mit Bestätigung).
- **Nicht in 6b:** Austausch von Paritätsplatten (neue Parität einrichten + `sync` genügt), Formatieren der Ersatzplatte im Assistenten (sie muss auf der Seite „Datenträger“ als Datenplatte formatiert sein), externe Backup-Ziele (Cloud/NFS), Verschlüsselung der Archive, Sicherung von Benutzerkonten.

**Tech Stack:** Go (`archive/tar`, `compress/gzip`, `VACUUM INTO`, SQLite-Migration), Vue 3 + PrimeVue, Vitest.

**Spec:** `docs/specs/2026-11-15-betternas-design.md` §4.1 (SnapRAID-Wiederherstellung), §4.6 (Konfigurations-Backup/-Restore); Abweichungen kommen nach Abschluss in §8.

## Global Constraints
- Alle Texte deutsch; Fehler als `apierr`; Audit-Eintrag (`audit.Record`) für Backup erstellen/löschen/herunterladen/wiederherstellen und Plattenersatz.
- Backup-Dateiname `^betternas-backup-\d{8}-\d{6}\.tar\.gz$`; nie freie Pfade aus Anfragen; Upload höchstens 256 MiB, Archiv-Einträge nur `manifest.json` und `betternas.db` (alles andere → `invalid`; keine absoluten/`..`-Pfade).
- Restore nur, wenn `manifest.format == 1`, `schemaVersion ≤` eigene Schema-Version und `PRAGMA integrity_check` der DB `ok` meldet.
- Der API-Prozess bleibt unprivilegiert; Neustart und Dateien außerhalb `/var/lib/betternas` nur über den Agent.
- Plattenersatz: nur Datenplatten, nur Pools mit SnapRAID, neue Platte Rolle `data`, in keinem Pool, `size ≥` alte; ein Pool-Vorgang gleichzeitig (bestehende Pool-Sperre).
- Bestehendes Muster beibehalten: Manager mit `db`/`agent`/`hub`/`jobs`; Routen `registerX` in `internal/api`; Agent-Methoden in eigener Datei mit `registerX(s, d)`.

## Review Focus
1. Archiv mit `../../etc/passwd`, absoluten Pfaden oder Zusatzdateien → abgelehnt, nichts geschrieben (Task 1).
2. Beschädigte/zu neue DB, falsches Format, abgeschnittenes `.tar.gz` → verständlicher Fehler, laufende DB unverändert (Task 1, 2).
3. Restore bei abgebrochenem Neustart: `pending.db` bleibt, alte DB bleibt, nächster Start wendet an; ein zweiter Start wendet nichts erneut an (Task 1).
4. Backup während laufender Schreibzugriffe liefert eine konsistente, öffnbare DB (Task 1).
5. Nächtliches Backup schlägt fehl (Platte voll) → Meldung über die Benachrichtigungen, alte Backups bleiben (Task 3).
6. Sync/Scrub während offener Wiederherstellung → `blocked`, nie eine Löschung (Task 6).
7. Neue Platte kleiner als die alte oder schon in einem Pool → abgelehnt, DB unverändert (Task 7).
8. `fix` meldet unwiederherstellbare Dateien → Lauf `errors`, Alarm, Marker bleibt (Task 6, 8).

---

## File Structure
- `backend/internal/backup/` – `backup.go` (Create, Inspect, Stage, ApplyPending, List, Delete, Prune), `backup_test.go`.
- `backend/internal/db/migrations/0006_snapraid_alias.sql`.
- `backend/internal/api/backup.go` – Routen; `backend/internal/agent/system.go` – `system.restart-api`.
- `backend/internal/snapraid/` (`Names`, Render), `backend/internal/agent/snapraid.go` (`fix`, Marker), `backend/internal/storage/replace.go` (Manager.ReplaceDisk u. a.), `backend/internal/api/snapraid.go` (Routen).
- `frontend/src/api/backup.ts`, `stores/backup.ts`, `views/system/BackupView.vue`, `components/storage/ReplaceDiskDialog.vue`; Änderungen in `PoolsView.vue`, `router.ts`, `AppLayout.vue`, `api/storage.ts`.

## Tasks
- [ ] 1 Paket `internal/backup`: Create, Inspect, Stage, ApplyPending, List/Delete/Prune
- [ ] 2 Backup-Manager, Agent `system.restart-api`, API-Routen
- [ ] 3 Verdrahtung `main.go` (ApplyPending, nächtlicher Lauf, Meldung bei Fehler)
- [ ] 4 GUI „Sicherung“
- [ ] 5 SnapRAID-Namen (Alias): Migration, `PoolConfig.Names`, Render
- [ ] 6 Agent: `fix`, Marker, Sperre für Sync/Scrub, Status
- [ ] 7 Storage-Manager: ReplaceDisk, RetryFix, CancelRecovery + Routen
- [ ] 8 Monitor: Befund „Wiederherstellung offen/fehlgeschlagen“
- [ ] 9 GUI: Plattenersatz-Assistent
- [ ] 10 Integrationstest (Loop-Geräte), Ausrollen (0.5.0-rc2), Spec §8, Memory

### Task 1: Paket `internal/backup`

**Files:** Create `backend/internal/backup/backup.go`, `backup_test.go`

**Interfaces:**
- Produces:
  ```go
  const Format = 1
  var NameRe = regexp.MustCompile(`^betternas-backup-\d{8}-\d{6}\.tar\.gz$`)
  type Manifest struct { Format int; CreatedAt time.Time; Version string; SchemaVersion int; Host string; Counts map[string]int } // JSON camelCase
  type Info struct { Name string; Size int64; Manifest Manifest }
  func Create(ctx context.Context, d *sql.DB, dir, version string) (Info, error)   // schreibt dir/<Name>; Temp-Datei + Rename
  func Inspect(r io.Reader, tmpDir string) (Manifest, dbPath string, err error)    // entpackt nach tmpDir, prüft Einträge/Format/Schema/integrity_check
  func Stage(dir string, dbPath string, m Manifest) error                            // dir/pending.db + pending.json (atomar), ersetzt vorhandenes Pending
  func ApplyPending(dir, dbPath string, keep int) (applied bool, err error)          // vor db.Open; tauscht DB, sichert alte als <dbPath>.pre-restore-<zeit>, löscht -wal/-shm, räumt auf
  func List(dir string) ([]Info, error)                                               // neueste zuerst
  func Delete(dir, name string) error                                                 // nur NameRe, sonst invalid
  func Prune(dir string, keep int) error
  func SchemaVersion(d *sql.DB) (int, error)                                          // MAX(version) aus schema_version
  ```
  `Counts`: `pools`, `shares`, `stacks`, `disks` (Zeilenzahlen der gleichnamigen Tabellen; fehlende Tabelle → 0).

- [ ] **Step 1: Tests** (Namen und Assertions)
  - `TestCreateIsConsistentAndOpenable`: DB mit Pools/Sessions/Jobs; während `Create` schreibt eine Goroutine weiter → Archiv enthält genau `manifest.json` + `betternas.db`; entpackte DB öffnet, `integrity_check`=ok, `sessions` und `jobs` leer, Pools vorhanden; `Manifest.Counts["pools"]` stimmt; Dateiname passt auf `NameRe`.
  - `TestInspectRejectsBadArchives` (Tabelle): `../../etc/passwd`, `/abs`, Zusatzdatei `extra.txt`, fehlendes Manifest, `format:2`, `schemaVersion` = eigene+1, abgeschnittenes gzip, DB-Datei mit Müll → je Fehler `invalid_params`; `tmpDir` enthält danach nichts Außerhalb.
  - `TestStageAndApplyPending`: Stage → `ApplyPending` tauscht (neue DB liegt am Ziel, alte als `.pre-restore-*`, `-wal`/`-shm` weg, `pending.*` entfernt) → zweiter Aufruf `applied=false`; `keep=3` räumt ältere `.pre-restore-*` auf.
  - `TestApplyPendingWithoutPendingIsNoop`, `TestApplyPendingKeepsOldDbOnFailure` (Ziel-Verzeichnis schreibgeschützt → Fehler, `pending.db` bleibt).
  - `TestListDeletePrune`: 9 Archive → `Prune(…, 7)` lässt die 7 neuesten; `Delete(dir, "../x")` und `Delete(dir, "foo.tar.gz")` → `invalid_params`.
- [ ] **Step 2:** `go test ./internal/backup/` → FAIL.
- [ ] **Step 3:** Implementieren. `Create`: `VACUUM INTO '<tmp>'` (Pfad quoten), Kopie öffnen, `DELETE FROM sessions; DELETE FROM jobs; VACUUM`, tar.gz schreiben (Reihenfolge Manifest, DB; Modus 0600), Rename. `Inspect`: nur reguläre Dateien mit exakt den zwei Namen; Größenbegrenzung per `io.LimitReader` (DB ≤ 1 GiB).
- [ ] **Step 4:** Tests → PASS.
- [ ] **Step 5: Commit** `feat(backup): config archive create, inspect, stage and apply`

### Task 2: Backup-Manager, Agent-Neustart, Routen

**Files:** Create `backend/internal/backup/manager.go`, `manager_test.go`, `backend/internal/api/backup.go`, `backup_api_test.go`, `backend/internal/agent/system.go`, `system_test.go`; Modify `backend/internal/agent/server.go` (`registerSystem`), `backend/internal/api/server.go` (`Deps.Backup`)

**Interfaces:**
- Consumes: Task 1.
- Produces:
  ```go
  type Agent interface{ Call(ctx context.Context, method string, params, result any) error }
  type Manager struct{ … }
  func NewManager(d *sql.DB, agent Agent, dir, version string, hub *events.Hub) *Manager
  func (m *Manager) Create(ctx context.Context, actor string) (Info, error)
  func (m *Manager) List(ctx context.Context) ([]Info, error)
  func (m *Manager) Open(name string) (*os.File, error)                      // für Download; NameRe-geprüft
  func (m *Manager) Delete(ctx context.Context, actor, name string) error
  type Preview struct { Token string; Manifest Manifest; Expires time.Time; Warnings []string }
  func (m *Manager) PreviewUpload(ctx context.Context, r io.Reader) (Preview, error)       // Inspect nach tmp, Token merken (15 min)
  func (m *Manager) PreviewStored(ctx context.Context, name string) (Preview, error)
  func (m *Manager) Restore(ctx context.Context, actor, token string) error               // Stage + agent "system.restart-api"; Audit
  ```
  Warnungen: `version` des Archivs neuer als die laufende; Archiv älter als 90 Tage; weitere aus `Counts`.
  Agent `system.restart-api` (kein Parameter): `systemd-run --on-active=2 --unit=betternas-api-restart systemctl restart betternas-api.service`.
  Routen (Admin-Sitzung, CSRF): `GET /api/v1/backups`, `POST /api/v1/backups` (201), `GET /api/v1/backups/{name}` (Download, `Content-Disposition: attachment`, Audit), `DELETE /api/v1/backups/{name}` (204), `POST /api/v1/backups/restore/preview` (Body: Archiv roh, `MaxBytesReader` 256 MiB → `Preview`), `POST /api/v1/backups/{name}/restore/preview`, `POST /api/v1/backups/restore` (Body `{"token"}` → 202).

- [ ] **Step 1: Tests**
  - Manager: `TestCreateListDeleteAudit`; `TestRestoreNeedsValidToken` (unbekannt/abgelaufen → `not_found`/`conflict`, Agent nie aufgerufen); `TestRestoreStagesThenRestarts` (Reihenfolge: `pending.db` existiert, dann `system.restart-api`; Fehler des Agents → `pending.*` wird wieder entfernt); `TestPreviewUploadRejectsGarbage` (Review Focus 1, 2: DB unverändert, tmp aufgeräumt).
  - Agent: `TestRestartApiRunsSystemdRun` (genau `systemd-run --on-active=2 --unit=betternas-api-restart systemctl restart betternas-api.service`).
  - API: `TestBackupRoutes` (Anlegen → Liste → Download-Bytes = Archiv, `attachment`; Löschen; ungültiger Name 400; ohne Sitzung 401), `TestRestoreRoutesFlow` (Upload-Preview → Restore 202 → Agent-Stub bekam `system.restart-api`; zu großer Upload → 413/400).
- [ ] **Step 2:** Tests → FAIL.
- [ ] **Step 3:** Implementieren; Tokens im Speicher (wie `formatToken` im Storage-Manager), temporär entpackte DB bleibt bis Restore/Ablauf unter `<dir>/restore-tmp/<token>/`, Aufräumen beim nächsten Preview.
- [ ] **Step 4:** `go test ./...` → PASS.
- [ ] **Step 5: Commit** `feat(backup): manager, restart via agent and api routes`

### Task 3: Verdrahtung `main.go`, nächtlicher Lauf

**Files:** Modify `backend/cmd/betternas-api/main.go`; Create `backend/internal/backup/schedule.go`, `schedule_test.go`

**Interfaces:**
- Produces: `func (m *Manager) Run(ctx context.Context, n Notifier)`; `type Notifier interface{ Send(ctx context.Context, msg notify.Message) []notify.Result }` (Bestandsschnittstelle, wie `monitor.Notifier`); `func nextRun(now time.Time) time.Time` (nächstes 02:30 lokal).
- `main.go`: **vor** `db.Open`: `backup.ApplyPending(filepath.Join(stateDir,"restore"), cfg.DBPath, 3)` (Fehler → Log + normal weiter starten); danach `bm := backup.NewManager(d, agent, stateDir+"/backups", version.Version, hub)`; `go bm.Run(ctx, ns)`; `Deps.Backup: bm`. `stateDir = filepath.Dir(cfg.DBPath)`.

- [ ] **Step 1: Tests:** `TestNextRun` (vor/nach 02:30, Monatswechsel, Sommerzeit-Wechsel liefert nie in der Vergangenheit); `TestRunCreatesBackupAndNotifiesOnFailure` (Nachsteuerbare Uhr/Intervall per Feld; Erfolg → Archiv + `Prune`; Create-Fehler (Verzeichnis nicht schreibbar) → ein `notify.Message{Level:"crit", Title:"Konfigurations-Backup fehlgeschlagen"}`, bestehende Archive unangetastet); `TestRunStopsOnContext`.
- [ ] **Step 2:** FAIL → **Step 3:** implementieren → **Step 4:** `go vet ./... && go test ./...` PASS.
- [ ] **Step 5: Commit** `feat(backup): nightly run, apply pending restore at start`

### Task 4: GUI „Sicherung“

**Files:** Create `frontend/src/api/backup.ts`, `stores/backup.ts`, `views/system/BackupView.vue`, `__tests__/backup-view.test.ts`; Modify `router.ts` (`system/sicherung`, Titel „Sicherung“), `AppLayout.vue` (Dock `pi pi-history`, `['#14b8a6','#2dd4bf']`)

**Verhalten:** Liste (Datum, Größe, Version, Pools/Freigaben) mit Herunterladen (`<a href="/api/v1/backups/<name>" download>`), Löschen (Bestätigung), „Jetzt sichern“; Abschnitt „Wiederherstellen“: Datei wählen (oder aus der Liste „Wiederherstellen…“) → Vorschau-Dialog (Zähler, Warnungen, Hinweis „Benutzerkonten, Docker-Daten und TLS-Schlüssel sind nicht enthalten; alle werden abgemeldet; BetterNAS startet neu“) → Bestätigung durch Tippen von `WIEDERHERSTELLEN` → danach Banner „BetterNAS startet neu …“, Abfrage von `/api/v1/system/info` bis erreichbar, dann Weiterleitung zur Anmeldung (Muster `stores/updates.ts`).

- [ ] **Step 1: Tests (Vitest):** Liste rendert Name/Größe; „Jetzt sichern“ → `POST /api/v1/backups` und Liste neu; Upload (Datei) → Preview-Dialog zeigt Zähler und Warnung; Bestätigungsknopf bleibt gesperrt, bis exakt `WIEDERHERSTELLEN` getippt ist; danach `POST /api/v1/backups/restore` mit Token; API-Fehler der Vorschau wird als Text gezeigt (kein Dialog).
- [ ] **Step 2:** `npx vitest run` → FAIL; **Step 3:** umsetzen; **Step 4:** `npx vitest run` und `npx vue-tsc --noEmit` PASS.
- [ ] **Step 5: Commit** `feat(web): backup and restore page`

### Task 5: SnapRAID-Namen (Alias)

**Files:** Create `backend/internal/db/migrations/0006_snapraid_alias.sql`; Modify `backend/internal/snapraid/snapraid.go` (`PoolConfig.Names`, `Name(id)`), `render.go`, `backend/internal/storage/snapraid.go` (`loadSnapraid` liest Alias), `backend/internal/db/db_test.go` (Version 6), Tests dazu

**Interfaces:**
- Migration: `snapraid_alias(pool_name TEXT NOT NULL REFERENCES pools(name) ON DELETE CASCADE, disk_id TEXT NOT NULL REFERENCES disks(id), name TEXT NOT NULL, PRIMARY KEY(pool_name, disk_id))`.
- `PoolConfig.Names map[string]string` (`json:"names,omitempty"`): Platten-ID → SnapRAID-Name; fehlt ein Eintrag, ist der Name die ID. `func (p PoolConfig) Name(id string) string`. `Validate`: Namen müssen `diskIDRe` erfüllen und je Pool eindeutig sein (auch gegen nicht umbenannte IDs).
- Render: `data <Name(id)> <Pfad(id)>/`; Content-Datei-Zeilen und Parität unverändert.

- [ ] **Step 1: Tests:** `TestRenderUsesAliasName` (`Names{"new1":"old1"}` → Zeile `data old1 /srv/disks/new1/`, keine Zeile `data new1`); `TestValidateAliasCollision` (Alias = ID einer anderen Platte → `invalid_params`); `TestLoadSnapraidReadsAlias` (Manager-Test mit eingefügter Alias-Zeile; `Names` gefüllt); Migration-Test aktualisiert; `consistency_test.go` bleibt grün.
- [ ] **Step 2:** FAIL → **Step 3:** umsetzen → **Step 4:** `go test ./...` PASS.
- [ ] **Step 5: Commit** `feat(snapraid): disk name aliases for replaced disks`

### Task 6: Agent – `fix`, Marker, Sperre

**Files:** Modify `backend/internal/agent/snapraid.go`, `server.go` (`snapraid.start` bekommt `disk`), `backend/internal/snapraid/snapraid.go` (`FixingPath`, `Status.Recovery`, `RunState.Unrecoverable`), `backend/cmd/betternas-agent/main.go` (CLI `snapraid-run fix <pool> --disk <name>`); Tests in `snapraid_test.go`

**Interfaces:**
- `snapraid.FixingPath(pool) = StateDir + "/" + pool + ".fixing"` (Inhalt: SnapRAID-Name der Platte, Zeile 2: Startzeit).
- `type Recovery struct { Disk string `json:"disk"`; State string `json:"state"` /* open */; Since time.Time `json:"since"` }`; `Status.Recovery *Recovery`; `spStateFile.LastFix *RunState`; `Status.LastFix`.
- Agent: Aktion `fix` (erlaubt neben sync/scrub/diff; **braucht** `disk`, Name `^[A-Za-z0-9_][A-Za-z0-9._-]*$` und in der `.conf` als `data <name> …` vorhanden, sonst `invalid_params`). Ablauf `RunSnapraid(ctx, d, pool, "fix", force=false, disk)`: Sperre, Mount-Prüfung wie bisher, **Marker schreiben**, `snapraid -c <conf> -d <name> fix`; Exit 0 und kein „unrecoverable“ in der Ausgabe → Marker löschen, `LastFix=ok`; sonst `LastFix` `errors`/`failed` mit Meldung (Anzahl nicht wiederherstellbarer Dateien aus „unrecoverable“-Zeilen, `Unrecoverable`), **Marker bleibt**. Danach (nur bei ok) kein automatischer Sync – den löst der Manager (Task 7) aus.
- Sync/Scrub: existiert der Marker → sofort `blocked` mit Meldung „Wiederherstellung von Platte <name> ist offen – erst abschließen oder abbrechen.“ (kein snapraid-Aufruf).
- Neue Agent-Methode `snapraid.recovery.cancel {pool}`: löscht den Marker (nur wenn vorhanden, sonst `not_found`).
- `SnapraidStatus` füllt `Recovery` aus dem Marker; `StartSnapraid(ctx, d, pool, action, force, disk)` hängt `--disk <name>` an die `systemd-run`-Argumente; `parseSnapraidArgs` kennt `--disk`.

- [ ] **Step 1: Tests (FakeRunner, `FakeFS` wie vorhandene Tests):** `TestFixWritesMarkerAndRunsFix` (Aufrufreihenfolge: Marker existiert **vor** `snapraid … -d old1 fix`; bei Erfolg Marker weg, `LastFix.Result=="ok"`); `TestFixUnrecoverableKeepsMarker` (Ausgabe mit `unrecoverable`-Zeilen → `Result=="errors"`, `Unrecoverable==2`, Marker bleibt); `TestFixFailedKeepsMarker`; `TestFixRejectsUnknownOrBadDisk` (Name nicht in `.conf`, `../x`, leer → `invalid_params`, kein snapraid-Aufruf); `TestSyncAndScrubBlockedWhileMarker` (Review Focus 6: Ergebnis `blocked`, `snapraid` nicht aufgerufen); `TestStatusShowsRecovery`; `TestRecoveryCancelRemovesMarker`; `TestParseArgsDisk`.
- [ ] **Step 2:** FAIL → **Step 3:** umsetzen → **Step 4:** `go test ./internal/agent/ ./internal/snapraid/` PASS.
- [ ] **Step 5: Commit** `feat(agent): snapraid fix with recovery marker`

### Task 7: Storage-Manager – ReplaceDisk, RetryFix, CancelRecovery, Routen

**Files:** Create `backend/internal/storage/replace.go`, `replace_test.go`; Modify `backend/internal/api/snapraid.go` (+ `snapraid_api_test.go`), `backend/internal/agent/server.go` (`snapraid.recovery.cancel`)

**Interfaces:**
- Consumes: Task 5 (`Names`), Task 6 (`fix`, Status.Recovery).
  ```go
  func (m *Manager) ReplaceDisk(ctx context.Context, actor, pool, oldID, newID string) (jobID string, err error)
  func (m *Manager) RetryFix(ctx context.Context, actor, pool string) (jobID string, err error)       // liest Disk-Namen aus Status.Recovery
  func (m *Manager) CancelRecovery(ctx context.Context, actor, pool string) error
  ```
- Voraussetzungen (sonst Fehler **vor** jeder Änderung): Pool existiert und hat SnapRAID (`conflict` sonst); `oldID` ist Branch des Pools; `newID` hat Rolle `data`, ist in keinem Pool/keiner Parität, online, `size ≥ old.size` (`invalid_params` mit Größen im Text); kein Marker offen (`conflict`). Alte Platte darf online sein (dann „Austausch einer noch vorhandenen Platte“ erlaubt) oder fehlen.
- Job `snapraid.replace` (Ziel Pool): (1) DB-Transaktion: Branch `oldID`→`newID` an gleicher `position`, `snapraid_alias(pool, newID, Name(oldID))` (war `oldID` selbst Alias, wird dessen Name übernommen), alte `disks`-Zeile löschen; (2) `storage.apply`; (3) `snapraid.apply` mit `Names`; (4) `snapraid.start` Aktion `fix` (`disk=<Name>`) und Warten wie in `RunSnapraidJob` (Status-Poll); (5) bei Erfolg `snapraid.start` `sync`; Ergebnis je Schritt ins Job-Log; Audit `snapraid.replace` (Details: alt, neu, Name). Schlägt (2)/(3) fehl, wird die DB-Transaktion **nicht** gehalten – sie wird zuerst vorbereitet, aber erst nach erfolgreichem `storage.apply`/`snapraid.apply` committet (Muster `applyAndCommit` in `manager.go`). Schlägt (4)/(5) fehl: DB bleibt neu, Marker bleibt, Job `failed` mit Hinweis auf „Wiederholen“.
- Routen: `POST /api/v1/pools/{name}/snapraid/replace` Body `{"oldDisk","newDisk"}` → 202 `{jobId}`; `POST /api/v1/pools/{name}/snapraid/recovery/retry` → 202; `DELETE /api/v1/pools/{name}/snapraid/recovery` → 204 (Abbrechen).

- [ ] **Step 1: Tests:** `TestReplaceDiskHappyPath` (fakeAgent: Reihenfolge `storage.apply` → `snapraid.apply` (mit `Names{new:old}`) → `snapraid.start fix disk=old` → `snapraid.start sync`; DB: Branch an gleicher Position, Alias vorhanden, alte Disk-Zeile weg; Audit); `TestReplaceDiskRejects` (Tabelle: kleiner, unzugeordnet/Rolle `parity`, schon in Pool, Pool ohne SnapRAID, Marker offen → je Fehler, **DB und Agent unverändert**: Review Focus 7); `TestReplaceDiskApplyFailureLeavesDbUntouched`; `TestReplaceDiskFixFailureKeepsNewState` (Job `failed`, Alias und Branch bleiben); `TestReplaceAliasChain` (zweiter Austausch derselben Position behält den ursprünglichen Namen); `TestRetryFixUsesRecoveryDisk`; `TestCancelRecoveryCallsAgent`. API: `TestReplaceRoutes` (202/400/409, Audit-Eintrag, ohne Sitzung 401).
- [ ] **Step 2:** FAIL → **Step 3:** umsetzen → **Step 4:** `go vet ./... && go test ./...` PASS.
- [ ] **Step 5: Commit** `feat(storage): replace failed snapraid data disk with fix`

### Task 8: Monitor – Wiederherstellung offen

**Files:** Modify `backend/internal/monitor/monitor.go`, `monitor_test.go`

- Neuer Befund je Pool mit `Status.Recovery != nil`: Schlüssel `snapraid-recovery:<pool>`, Stufe `crit`, Titel „SnapRAID-Wiederherstellung offen: <pool>“, Text mit Platte und Meldung aus `LastFix` (z. B. „2 Dateien nicht wiederherstellbar“). Verschwindet mit dem Marker → „Behoben“-Meldung (bestehende Logik). Quellfehler → Präfix `snapraid-recovery:` unbeurteilt wie die übrigen.
- [ ] **Step 1: Tests:** `TestFindingRecoveryOpen` (Marker → Befund mit Text aus `LastFix.Message`), `TestRecoveryClearedRecovers` (einmal „Behoben“), `TestRecoveryStatusErrorKeepsAlert`.
- [ ] **Step 2–4:** FAIL → umsetzen → `go test ./internal/monitor/` PASS. **Step 5: Commit** `feat(monitor): alert for open snapraid recovery`

### Task 9: GUI – Plattenersatz-Assistent

**Files:** Create `frontend/src/components/storage/ReplaceDiskDialog.vue`, `frontend/src/__tests__/replace-disk.test.ts`; Modify `frontend/src/api/storage.ts` (`SnapraidStatus.recovery`, `lastFix`; `replaceDisk`, `retryRecovery`, `cancelRecovery`), `stores/storage.ts`, `views/storage/PoolsView.vue`

**Verhalten:** In der SnapRAID-Zeile eines Pools: Knopf „Platte ersetzen …“ (hervorgehoben, wenn eine Platte des Pools `missing` ist). Dialog in drei Schritten: ① Welche Platte ersetzen? (Datenplatten des Pools; fehlende vorausgewählt) ② Ersatzplatte (nur Datenplatten ohne Pool, online, `size ≥` alte; sonst Hinweis „Formatiere eine neue Platte zuerst unter Datenträger → Datenplatte“) ③ Zusammenfassung mit Warnung („Die Dateien der alten Platte werden aus der Parität wiederhergestellt; das kann viele Stunden dauern. Synchronisierung ist währenddessen gesperrt.“) und Bestätigung durch Anhaken. Bei offener Wiederherstellung zeigt der Pool ein Banner (Platte, Meldung aus `lastFix`) mit „Wiederholen“ und „Abbrechen“ (Bestätigung). Fortschritt über den bestehenden Job-Panel.

- [ ] **Step 1: Tests:** Schritt ② listet nur zulässige Platten (zu kleine, Pool-Mitglieder, Paritätsplatten fehlen); Weiter ohne Auswahl gesperrt; Bestätigung sendet `POST …/snapraid/replace` mit `{oldDisk,newDisk}`; Banner bei `recovery` mit „Wiederholen“ (`POST …/recovery/retry`) und „Abbrechen“ (`DELETE …/recovery` erst nach Bestätigung); fehlende Platte hebt den Knopf hervor; Hinweistext, wenn keine Ersatzplatte existiert.
- [ ] **Step 2–4:** FAIL → umsetzen → `npx vitest run`, `npx vue-tsc --noEmit` PASS. **Step 5: Commit** `feat(web): replace disk wizard and recovery banner`

### Task 10: Integrationstest, Ausrollen, Doku

**Files:** Modify `backend/internal/agent/snapraid_integration_test.go` (neuer Test `TestSnapraidReplaceFix` im selben Stil: Loop-Geräte), `Makefile` (Testliste), `docs/specs/2026-11-15-betternas-design.md` (§8 „Abweichung Phase 6b“), Memory

- [ ] **Step 1:** `TestSnapraidReplaceFix`: zwei Datenplatten + Parität auf Loop-Geräten, Dateien schreiben, `sync`; Datenplatte 1 löschen/neu formatieren (leere „Ersatzplatte“ unter neuer ID), Konfiguration mit `Names{neu:alt}` anwenden, `fix` über `RunSnapraid` → Dateien wieder da (Prüfsummen gleich), Marker weg; vor dem `fix` liefert `sync` `blocked`.
- [ ] **Step 2:** `make build web deb VERSION=0.5.0-rc2 && make deploy VERSION=0.5.0-rc2`; `make integration` → PASS (Backup-/Restore-Weg auf dem Testgerät: `POST /api/v1/backups` per GUI-Prüfung des Nutzers; ein Restore **nicht** automatisch auslösen, er ersetzt die DB des Testgeräts).
- [ ] **Step 3:** Spec §8, Memory, Go + Vitest abschließend grün; Commit `docs: phase 6b notes`.
