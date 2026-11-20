# appdata/dockerdata-Trennung Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Docker-Daten in zwei einstellbare Wurzeln (appdata, dockerdata) trennen; auf frischen Installationen bleibt Docker gesperrt, bis beide gesetzt sind.

**Architecture:** Die Wurzeln stehen in `/etc/betternas/paths.json`, nur der Agent liest und schreibt sie und hält sie im Speicher (`curPaths`). Alle pfadabhängigen Docker-Operationen laufen schon im Agent. Die API reicht nur `docker.paths.get/set` durch und kennt keine Wurzeln mehr (die Wurzelprüfung des Ordner-Pickers wandert vollständig in den Agent). Fehlt `paths.json`, sind alle Docker-RPCs mit `docker_not_configured` gesperrt, außer Status und Pfad-RPCs.

**Tech Stack:** Go (Agent/API, `internal/agent`, `internal/docker`, `internal/api`), Vue 3 + PrimeVue + Vitest (`frontend`), Shell/Debian-Paketierung (`packaging`).

**Spec:** `docs/superpowers/specs/2026-11-20-dockerdata-trennung-design.md`

## Global Constraints

- Datei: `/etc/betternas/paths.json` = `{"appdata":"…","dockerdata":"…"}`, Rechte 0644, Verzeichnis `/etc/betternas`.
- Unter appdata: `stacks`, `reverse-proxy`, `backups` (BetterNAS-eigen, reserviert). Unter dockerdata: `docker` (data-root, reserviert).
- Erlaubte Orte für Wurzeln: unterhalb von `/srv/pools`, `/srv/disks`, bei bestehenden Installationen auch `/srv/appdata`; nie `/`, nie `/srv/pools` oder `/srv/disks` selbst; appdata und dockerdata dürfen sich nicht verschachteln und nicht gleich sein (Ausnahme: der Legacy-Zustand `/srv/appdata` + `/srv/appdata`).
- dockerdata auf mergerfs: Fehler. appdata auf mergerfs: Warnung. nfs/cifs/fuse.sshfs: Fehler. Gleiches Gerät wie `/`: Warnung.
- Fehlercode `docker_not_configured` (HTTP 409). Alle Texte deutsch.
- Ein geänderter Pfad gilt nur für Neues; vorhandene Stacks/Daten werden nie verschoben oder umgeschrieben. `daemon.json` wird nur bei der Ersteinrichtung geschrieben (und nur, wenn keine eigene vorhanden ist).
- Bestehende Tests müssen grün bleiben: der Test-Helfer `dockerEnv` setzt den Legacy-Zustand.

## Review Focus

- `paths.json` ist kaputt (kein gültiges JSON): Docker bleibt **gesperrt** mit klarer Fehlermeldung, es wird nichts überschrieben und nicht stillschweigend „Legacy“ angenommen.
- Wurzel mit Leerzeichen, `..`, Schrägstrich am Ende, Symlink auf `/`: wird bereinigt oder abgelehnt, nie roh in Compose-Dateien oder `daemon.json` übernommen.
- Bestehende Installation mit eigener `daemon.json` (z. B. anderer `data-root`): Einrichtung überschreibt sie nicht, Docker bleibt aktiv.
- Update des Pakets auf einer bestehenden Installation: kein Moment, in dem Docker deaktiviert wird (postinst deaktiviert Docker nur, wenn weder `paths.json` noch Legacy-Merkmale vorliegen).
- Zweites `PUT` der Pfade, während ein Stack läuft: Laufende Stacks bleiben unberührt, Antwort nennt `staleStacks`.

---

### Task 1: Pfadmodell und Fehlercode

**Files:**
- Create: `backend/internal/docker/paths.go`, `backend/internal/docker/paths_test.go`
- Modify: `backend/internal/apierr/apierr.go` (Code + `HTTPStatus`)

**Interfaces:**
- Produces:
  - `apierr.DockerNotConfigured = "docker_not_configured"` (HTTP 409)
  - `type Paths struct{ Appdata, Dockerdata string }` (JSON `appdata`, `dockerdata`)
  - `func (p Paths) StacksDir() string` (`<appdata>/stacks`), `ProxyDir()` (`<appdata>/reverse-proxy`), `BackupBase()` (`<appdata>/backups/stacks`), `DataRoot()` (`<dockerdata>/docker`), `StackDir(name string) string`
  - `var LegacyPaths = Paths{"/srv/appdata", "/srv/appdata"}`
  - `func CleanRoot(p string, allowLegacy bool) (string, error)`: bereinigt und prüft rein syntaktisch (absolut, ohne `..`/NUL, unter `/srv/pools/` oder `/srv/disks/`, bei `allowLegacy` auch `/srv/appdata`, nicht die Wurzeln `/srv/pools`/`/srv/disks` selbst, Länge ≤ 4096).
  - `func (p Paths) Validate(allowLegacy bool) error`: beide `CleanRoot`, Verschachtelung/Gleichheit verboten (außer `p == LegacyPaths`).
  - `func (p Paths) Reserved(root, first string) bool`: ist `first` ein reservierter Name unter der Wurzel (`stacks|reverse-proxy|backups` unter appdata, `docker` unter dockerdata; unter Legacy beide Mengen auf `/srv/appdata`).

- [ ] **Step 1: Tests** in `paths_test.go`: `TestCleanRoot` (Tabelle: `/srv/pools/tank/docker/` → `/srv/pools/tank/docker`; `/srv/pools` → Fehler; `/srv/pools/../etc` → Fehler; `/etc/x` → Fehler; `/srv/appdata` → Fehler ohne, ok mit `allowLegacy`; `/srv/disks/ab/dd` ok; Leerzeichen im Pfad ok), `TestPathsValidate` (gleich → Fehler; `/srv/pools/a` + `/srv/pools/a/b` → Fehler; `LegacyPaths` → ok; zwei getrennte Pools → ok), `TestPathsDirs` (`StacksDir`, `ProxyDir`, `BackupBase`, `DataRoot`, `StackDir("x")`), `TestPathsReserved`.
- [ ] **Step 2:** `go test ./internal/docker -run 'TestCleanRoot|TestPathsValidate|TestPathsDirs|TestPathsReserved'` → FAIL (Symbole fehlen).
- [ ] **Step 3:** Implementiere obige Signaturen in `docker/paths.go`; füge in `apierr.go` den Code und die Zuordnung zu 409 hinzu (`HTTPStatus` neben `Conflict`).
- [ ] **Step 4:** Dieselben Tests → PASS; `go vet ./internal/...`.
- [ ] **Step 5: Commit** `feat(docker): Pfadmodell (appdata/dockerdata) und Fehlercode docker_not_configured`

### Task 2: Agent-Zustand: Laden, Legacy-Erkennung, Sperre

**Files:**
- Create: `backend/internal/agent/docker_paths.go`, `backend/internal/agent/docker_paths_test.go`
- Modify: `backend/internal/agent/docker_test.go` (Helfer `dockerEnv` ruft `useLegacyPaths(t)`), `backend/internal/agent/server.go` (`initPaths(d)` in `Register`)

**Interfaces:**
- Consumes: `docker.Paths`, `docker.LegacyPaths`, `apierr.DockerNotConfigured`
- Produces:
  - `const pathsFile = "/etc/betternas/paths.json"`
  - `var curPaths atomic.Pointer[docker.Paths]`
  - `func initPaths(d Deps) error`: liest `pathsFile`. Gültig → `curPaths.Store`. Fehlt: Legacy, wenn `d.FS.Glob("/srv/appdata/stacks/*/compose.yaml")` nicht leer **oder** `data-root` der `daemon.json` gleich `/srv/appdata/docker`; dann `LegacyPaths` speichern **und in die Datei schreiben**. Sonst `curPaths.Store(nil)`. Ungültiges JSON oder fehlschlagende `Validate(true)` → Fehler `apierr.Conflict` „paths.json ist ungültig …“, `curPaths` bleibt nil.
  - `func requirePaths() (docker.Paths, error)`: `docker_not_configured` („Docker ist noch nicht eingerichtet. Lege unter Einstellungen → Docker die Speicherorte fest.“), solange `curPaths` nil.
  - `func guarded(h rpc.Handler) rpc.Handler`: ruft `requirePaths()` zuerst auf (Typ des Handlers aus `internal/rpc` übernehmen).
  - `func useLegacyPaths(t *testing.T)` (Testhelfer): setzt `curPaths` auf `LegacyPaths`, `t.Cleanup` setzt zurück.

- [ ] **Step 1: Tests:** `TestInitPathsFile` (Datei mit zwei Pools → geladen), `TestInitPathsFreshIsNil` (nichts vorhanden → `requirePaths` liefert Code `docker_not_configured`), `TestInitPathsLegacyByStack` und `…ByDataRoot` (jeweils `curPaths == LegacyPaths` und Datei geschrieben), `TestInitPathsCorruptStaysLocked` (Datei `{x` → Fehler, weiterhin `docker_not_configured`, Datei unverändert).
- [ ] **Step 2:** `go test ./internal/agent -run TestInitPaths` → FAIL.
- [ ] **Step 3:** Implementiere in `docker_paths.go`. `Register` ruft `initPaths(d)` und loggt einen Fehler, ohne abzubrechen.
- [ ] **Step 4:** Tests → PASS.
- [ ] **Step 5: Commit** `feat(agent): Pfadzustand aus paths.json mit Legacy-Erkennung und Sperre`

### Task 3: Stacks, Rollback und Reverse-Proxy auf Paths umstellen

**Files:**
- Modify: `backend/internal/agent/docker.go` (`composePath`, `envPath`, `composeArgs`, `ListStacks`, Stack-Anlage), `docker_rollback.go:29` (`rollbackPath`), `docker_proxy.go` (`proxyConf`, `ProxyWrite`), `backend/internal/docker/docker.go` (`StacksDir`, `StackDir` entfernen), `backend/internal/docker/proxy.go` (`ProxyDir` entfernen, `RenderProxyCompose(c ProxyConfig, proxyDir string)`), `backend/internal/docker/proxy_test.go`
- Modify (Registrierung): `docker.go registerDocker`, `docker_ops.go`, `docker_updates.go`, `docker_resources.go`, `docker_devices.go`, `docker_proxy.go`: alle Handler außer `docker.status` mit `guarded(...)` umhüllen. `folder.list` und `folder.create` (`docker_folders.go`) bleiben ungesperrt (siehe Task 4).

**Interfaces:**
- Consumes: `requirePaths`, `curPaths`, `docker.Paths`.
- Produces: `func stackDir(name string) string` im Agent (liest `curPaths`; Legacy-Fallback nur nicht nötig, weil gesperrt); `composePath/envPath/composeArgs/rollbackPath` nutzen es. `proxyConf()` statt Konstante.

- [ ] **Step 1: Tests:** In `docker_paths_test.go` `TestStackPathsFollowPaths` (curPaths = zwei Pools → `composePath("x")` = `<appdata>/stacks/x/compose.yaml`) und `TestDockerRPCsLockedWhenNotConfigured` (Server registrieren, `curPaths` nil; `stack.list`, `container.list`, `proxy.write`, `image.list` liefern `docker_not_configured`, `docker.status` nicht). In `proxy_test.go` `TestRenderProxyCompose` erwartet die übergebene Wurzel in den drei Volume-Zeilen.
- [ ] **Step 2:** `go test ./internal/agent ./internal/docker` → FAIL/Build-Fehler.
- [ ] **Step 3:** Stelle alle Aufrufer um; `go build ./...` muss ohne Verweis auf `docker.StacksDir`, `docker.StackDir`, `docker.ProxyDir` laufen.
- [ ] **Step 4:** `go test ./...` im Backend → PASS (bestehende Tests über `useLegacyPaths`).
- [ ] **Step 5: Commit** `refactor(docker): Stack-, Rollback- und Proxy-Pfade aus Paths, Docker-RPCs gesperrt ohne Einrichtung`

### Task 4: Ordner-Picker mit eingerichteten Wurzeln

**Files:**
- Modify: `backend/internal/docker/folders.go` (`FolderRoots`, `IsFolderRoot`, `UnderFolderRoot` entfernen; `CleanFolder` prüft nur noch Syntax: absolut, kein `..`, kein NUL, ≤ 4096, `path.Clean`), `backend/internal/docker/manager.go` (Kommentar), `backend/internal/agent/docker_folders.go`, `docker_folders_test.go`, `docker/folders_test.go`

**Interfaces:**
- Produces (Agent): `func folderRoots() []string` = `/srv/pools`, `/srv/disks`, dazu `Appdata` und `Dockerdata`, wenn sie nicht darunter liegen (Legacy: `/srv/appdata`); `func underFolderRoot(p string) bool`; `func isFolderRoot(p string) bool`. `reservedFolder(p)` nutzt `Paths.Reserved`. `hiddenAppdata` entfällt, `ListFolders` blendet die reservierten Namen unter beiden Wurzeln aus. `CreateFolder` erlaubt Anlegen unter appdata/dockerdata/Pools/Disks, nicht direkt in `/srv/pools` oder `/srv/disks`.
- Hinweis: `ListFolders` und `CreateFolder` dürfen auch im Zustand „nicht eingerichtet“ laufen (für die Einrichtung), deshalb `folder.list`/`folder.create` **nicht** mit `guarded` umhüllen (Task 3 entsprechend ausnehmen); `folderRoots()` kennt dann nur `/srv/pools` und `/srv/disks`.

- [ ] **Step 1: Tests:** `TestListFoldersRootsIncludeDisksAndConfigured` (Wurzelliste `/srv/pools`, `/srv/disks`, plus bei Config außerhalb `/srv/pools/a/dd`), `TestListFoldersHidesReservedUnderBothRoots`, `TestCreateFolderInDisks`, `TestCreateFolderRefusesRootsThemselves`, `TestFolderRPCsWorkWhenNotConfigured`; `CleanFolder` akzeptiert `/etc` syntaktisch (Wurzelprüfung ist Agent-Sache) und lehnt `..` ab.
- [ ] **Step 2:** `go test ./internal/agent ./internal/docker -run 'Folder'` → FAIL.
- [ ] **Step 3:** Implementiere. Die Fehlertexte nennen die Bereiche „Pools, Platten und die Docker-Speicherorte“.
- [ ] **Step 4:** Tests → PASS.
- [ ] **Step 5: Commit** `feat(docker): Ordnerauswahl nutzt eingerichtete Wurzeln und /srv/disks`

### Task 5: Datensicherung mit beiden Wurzeln

**Files:**
- Modify: `backend/internal/agent/docker_backup.go` (`appdataRoot`, `backupBase`, `bindPathRe`, `reservedAppdata`, `noDataReason`, `df`-Aufruf), `docker_backup_test.go`

**Interfaces:**
- Produces: `func safeBindPath(p string) bool` bleibt, prüft jetzt: bereinigt, kein `..`, Zeichenmenge wie bisher (`[A-Za-z0-9_.+@ -]` und `/`), liegt **unter** `Appdata` oder `Dockerdata` (nicht die Wurzel selbst), erster Namensteil nicht reserviert (`Paths.Reserved`). `backupDir(name)` nutzt `curPaths.BackupBase()`. `noDataReason` wird Funktion `noDataReason() string` und nennt beide Wurzeln. Platzprüfung `df` auf der Wurzel, in der das Backup liegt (`Appdata`).

- [ ] **Step 1: Tests:** `TestSafeBindPathBothRoots` (unter appdata ok, unter dockerdata ok, `<appdata>/stacks/x` nein, `<dockerdata>/docker` nein, `/srv/pools/…` nein, `..` und Leerzeichen-Sonderfälle wie bisher), `TestBackupPlanIncludesDockerdataBinds`, `TestBackupDirUsesAppdata`; die vorhandenen Backup-Tests bleiben im Legacy-Zustand grün.
- [ ] **Step 2:** `go test ./internal/agent -run 'Backup|SafeBind'` → FAIL.
- [ ] **Step 3:** Implementiere. Regex darf nicht mehr aus der Wurzel gebaut werden: Zeichenmenge einmal prüfen, Wurzelbezug über `strings.HasPrefix` mit `root + "/"`.
- [ ] **Step 4:** Tests → PASS.
- [ ] **Step 5: Commit** `feat(docker): Update-Datensicherung deckt appdata und dockerdata ab`

### Task 6: Pfade setzen: Prüfung und Ersteinrichtung

**Files:**
- Modify: `backend/internal/agent/docker_paths.go`, `docker_paths_test.go`, `services.go` (nur falls `DataRoot`-Anzeige aus `curPaths` statt `/var/lib/docker` sinnvoll; sonst unverändert)

**Interfaces:**
- Consumes: `docker.Paths.Validate/CleanRoot`, `sysexec.Runner`, `sysexec.FS`.
- Produces:
  - `type PathsInfo struct { Configured bool; Appdata, Dockerdata string; Warnings []string; StaleStacks int }` (JSON `configured`, `appdata`, `dockerdata`, `warnings`, `staleStacks`)
  - `func GetPaths(d Deps) (PathsInfo, error)`: `StaleStacks` = Anzahl Stacks, deren `compose.yaml` Bind-Pfade der Form `/srv/…` enthält, die weder unter dem aktuellen appdata noch unter dockerdata, noch unter `/srv/pools` liegen.
  - `func SetPaths(ctx context.Context, d Deps, in docker.Paths) (PathsInfo, error)`: unter `mutate`: `Validate(legacyActive)`; je Wurzel `realpath -e` (Ordner muss existieren, Link-Ziel muss wieder unter erlaubter Wurzel liegen); Dateisystem per `stat -f -c %T -- <p>` (`fuseblk`/`ext2/ext3` ok; `fuse.mergerfs` → Fehler für dockerdata/Warnung für appdata; `nfs|nfs4|cifs|smb3|fuse.sshfs` → Fehler); gleiches Gerät wie `/` per `stat -c %d` → Warnung. Dann `MkdirAll` der Unterordner (`stacks` 0700, `reverse-proxy`, `backups`, `docker` 0710), `paths.json` schreiben (`0644`), `curPaths.Store`. **Ersteinrichtung** (vorher `curPaths == nil`): `daemon.json` nur schreiben, wenn sie fehlt (`data-root`, `log-driver`, `log-opts` wie `packaging/docker/daemon.json`, `data-root` = `DataRoot()`); danach `systemctl enable --now docker.socket docker.service`; Fehler dabei sind harte Fehler, `paths.json` wird dann wieder entfernt, `curPaths` zurückgesetzt.
  - RPC `docker.paths.get` / `docker.paths.set` (nicht `guarded`).

- [ ] **Step 1: Tests (FakeRunner/Hook wie `folderEnv`):** `TestSetPathsFirstSetupWritesFilesAndStartsDocker` (prüft `paths.json`, `daemon.json` mit `data-root`, Aufrufe `systemctl enable --now docker.socket docker.service`, angelegte Unterordner), `TestSetPathsKeepsExistingDaemonJson`, `TestSetPathsRejectsMergerfsForDockerdata`, `TestSetPathsWarnsMergerfsForAppdata`, `TestSetPathsRejectsNetworkFS`, `TestSetPathsWarnsSameDeviceAsRoot`, `TestSetPathsRejectsNested`, `TestSetPathsRejectsSymlinkOutsideRoots`, `TestSetPathsRollbackWhenSystemctlFails`, `TestSetPathsChangeLaterDoesNotTouchDaemonJson`, `TestGetPathsStaleStacks`.
- [ ] **Step 2:** `go test ./internal/agent -run 'SetPaths|GetPaths'` → FAIL.
- [ ] **Step 3:** Implementiere. Der Wert, der in `daemon.json` landet, ist ausschließlich das bereinigte Ergebnis von `DataRoot()` (kein Rohtext) und wird als JSON-Marshal erzeugt.
- [ ] **Step 4:** Tests → PASS; `go test ./...` komplett.
- [ ] **Step 5: Commit** `feat(agent): Docker-Speicherorte setzen mit Prüfung und Ersteinrichtung`

### Task 7: API und Status

**Files:**
- Modify: `backend/internal/docker/docker.go` (`Status` bekommt `Configured bool` `json:"configured"`, `PathsInfo`-Typ hierher verlegen oder spiegeln), `docker/manager.go` (`Paths(ctx)`, `SetPaths(ctx, actor, docker.Paths)`), `backend/internal/api/docker.go` (Routen), `backend/internal/agent/docker.go` (`DockerStatus` füllt `Configured`), Tests in `api/docker_api_test.go` (Dateiname des vorhandenen Docker-API-Tests übernehmen) und `docker/manager_test.go`

**Interfaces:**
- Produces: `GET /api/v1/docker/paths` → `PathsInfo`; `PUT /api/v1/docker/paths` mit `{"appdata","dockerdata"}` → `PathsInfo` (Audit-Aktion `docker.paths.set`, Ziel `appdata=…,dockerdata=…`); beide mit denselben Rechten wie `/api/v1/docker/proxy`. `GET /api/v1/docker/status` enthält `configured`.

- [ ] **Step 1: Tests:** API: GET liefert `configured:false` ohne Einrichtung; PUT mit gültigen Pfaden leitet `docker.paths.set` an den Agent und schreibt Audit; ungültige Eingabe → 400; ohne Berechtigung → 403; `docker_not_configured` wird als 409 an den Client gegeben.
- [ ] **Step 2:** `go test ./internal/api ./internal/docker` → FAIL.
- [ ] **Step 3:** Implementiere analog zu `ProxyConfig`-Handlern.
- [ ] **Step 4:** `go vet ./... && go test ./...` → PASS.
- [ ] **Step 5: Commit** `feat(api): /docker/paths und configured im Docker-Status`

### Task 8: Paketierung

**Files:**
- Modify: `packaging/scripts/postinst.sh`, `packaging/build-deb.sh` (Verweis auf `docker-daemon.json` entfernen), `packaging/docker/daemon.json` (löschen, falls nirgends mehr gebraucht), betroffene Skripte unter `image/` und `tools/` (per `grep -rn "daemon.json\|srv/appdata" packaging image tools` prüfen)

**Interfaces:**
- Produces: `postinst` legt `/srv/appdata`, `/srv/appdata/stacks`, `/srv/appdata/docker` **nicht** mehr an. Es legt nur `/srv/disks /srv/parity /srv/pools /srv/vms /srv/vms/disks` an. Docker-Block: wenn `/etc/betternas/paths.json` fehlt **und** keine Legacy-Merkmale (`/srv/appdata/docker` vorhanden oder `/srv/appdata/stacks/*/compose.yaml` vorhanden oder `/etc/docker/daemon.json` vorhanden), dann (nur mit systemd) `systemctl disable --now docker.socket docker.service || true`; im chroot (Image-Bau) `systemctl disable docker.socket docker.service` ohne `--now`, falls `systemctl` dort verfügbar ist, sonst überspringen und im Kommentar vermerken. Die Entscheidung „Legacy“ im Skript entspricht der in `initPaths`.

- [ ] **Step 1:** Ergänze einen Shell-Test in `tools/tests/` (Muster der vorhandenen Tests übernehmen), der `postinst.sh` mit gefälschten `install`/`systemctl` in einem Temp-Wurzelverzeichnis ausführt: frisch → `systemctl disable` aufgerufen; mit `paths.json` → nicht; mit `/srv/appdata/stacks/x/compose.yaml` → nicht.
- [ ] **Step 2:** `tools/tests/run.sh` → FAIL.
- [ ] **Step 3:** Passe `postinst.sh` und `build-deb.sh` an.
- [ ] **Step 4:** `tools/tests/run.sh` → PASS; `make deb VERSION=0.0.1-test` baut (falls Docker für den Builder verfügbar ist, sonst im Bericht als nicht geprüft vermerken).
- [ ] **Step 5: Commit** `feat(packaging): Docker bis zur Einrichtung der Speicherorte deaktiviert`

### Task 9: Frontend – API, Store, Vorlagen

**Files:**
- Modify: `frontend/src/api/docker.ts` (`DockerStatus.configured`, `DockerPaths`, `dockerApi.paths()`, `dockerApi.setPaths(p)`), `frontend/src/stores/docker.ts` (`configured` computed, `paths` ref, `loadPaths()`; `refresh()` lädt Stacks/Container nur wenn `status.configured`), `frontend/src/utils/appTemplates.ts`, Test `frontend/src/utils/appTemplates.test.ts` (Name des vorhandenen Tests übernehmen)

**Interfaces:**
- Produces: `export const roots = { appdata: '/srv/appdata', dockerdata: '/srv/dockerdata' }` (veränderbar über `setRoots({appdata, dockerdata})`, vom Store nach `loadPaths()` aufgerufen); `appdata(sub)` und `dockerdata(sub)` liefern `(stack) => \`${roots.<x>}/${stack}/${sub}\``. Zuordnung: appdata für `CONFIG_DIR`, `CONF_DIR`, `STATE_DIR` und die Ordner `etc-pihole`, `letsencrypt`, `configs`; dockerdata für alle übrigen bisherigen `appdata(...)`-Aufrufe (Einzelprüfung je Vorlage, Vorgabe im Zweifel dockerdata, wenn der Ordner groß werden kann).

- [ ] **Step 1: Tests:** `TestTemplatesSplitRoots`: nach `setRoots({appdata:'/srv/pools/a/cfg', dockerdata:'/srv/pools/b/dd'})` liefert für jede Vorlage jedes Feld mit `kind: 'path'` und Vorgabe unter einer der beiden Wurzeln den Pfad unter der **richtigen** Wurzel (Jellyfin/Emby `CONFIG_DIR` → appdata, `DATA_DIR`, `DB_DIR`, `TRANSCODE_DIR` → dockerdata) und es gibt kein Feld mehr mit hartem `/srv/appdata/`. Store-Test: `configured:false` → `refresh()` ruft `dockerApi.stacks` nicht auf.
- [ ] **Step 2:** `cd frontend && npm test` → FAIL.
- [ ] **Step 3:** Implementiere.
- [ ] **Step 4:** `npm test` und `npm run build` → PASS.
- [ ] **Step 5: Commit** `feat(gui): Vorlagen und Store nutzen appdata/dockerdata`

### Task 10: Frontend – Einrichtung und Sperransicht

**Files:**
- Create: `frontend/src/components/docker/DockerPathsDialog.vue`, `frontend/src/components/docker/DockerNotConfigured.vue`, Tests daneben (`*.test.ts`)
- Modify: `frontend/src/views/docker/{StacksView,ContainersView,ProxyView,ResourcesView}.vue` (zeigen `DockerNotConfigured`, wenn `!store.configured`), `frontend/src/views/settings/DockerSettings.vue` (Karte „Speicherorte“), `AppLayout.vue` nur, falls dort Docker-Menüeinträge den Status brauchen

**Interfaces:**
- Consumes: `dockerApi.paths/setPaths`, vorhandene Ordnerauswahl-Komponente (in `frontend/src/components/docker` per `grep -rn "folders" frontend/src` ermitteln und wiederverwenden).
- Produces: `DockerNotConfigured` (Text „Docker ist noch nicht eingerichtet“, Knopf „Speicherorte festlegen“ → Route `settings-docker`, Hinweis „Bereite zuerst unter Speicher einen Pool oder eine Platte vor.“ mit Link, wenn keine Pools/Platten existieren); `DockerPathsDialog` mit zwei Ordnerfeldern, Erklärungen (appdata: klein, Einstellungen; dockerdata: groß), Warnungen aus der Antwort als `Message severity="warn"`, Fehlern als `error`, Hinweis „Gilt nur für neue Stacks; vorhandene Daten werden nicht verschoben“ und der Anzahl `staleStacks`. In `DockerSettings.vue` öffnet sich der Dialog beim Laden automatisch, solange `!configured`.

- [ ] **Step 1: Tests:** Komponententests: `DockerNotConfigured` zeigt Knopf und Link-Text; `DockerPathsDialog` sperrt „Speichern“ ohne beide Pfade, ruft `setPaths` mit beiden Pfaden, zeigt Warnungen und `staleStacks`; `StacksView` zeigt bei `configured:false` die Sperransicht statt der Stackliste.
- [ ] **Step 2:** `npm test` → FAIL.
- [ ] **Step 3:** Implementiere.
- [ ] **Step 4:** `npm test` und `npm run build` → PASS; zusätzlich im Browser (Mock-Server wie bei der btrfs-GUI-Prüfung) Sperransicht, Dialog und Fehlerfall ansehen.
- [ ] **Step 5: Commit** `feat(gui): Docker-Speicherorte einrichten, Sperransicht ohne Einrichtung`

### Task 11: Ausrollen und Prüfen auf dem Testgerät

**Files:** keine Codeänderung, außer Versionsnummer wie im Projekt-Workflow (Gedächtnis „Immer ausrollen“: Version hochzählen, auf `192.168.1.65` deployen).

- [ ] **Step 1:** Vollständiger Lauf: `make test` → PASS.
- [ ] **Step 2:** Deployen (`make deploy` bzw. `tools/dev-repo.sh`/`tools/remote-build.sh`, wie in früheren Etappen).
- [ ] **Step 3:** Prüfen am Testgerät: Docker bleibt aktiv, Stacks laufen, `GET /api/v1/docker/status` meldet `configured:true`, `/etc/betternas/paths.json` enthält zweimal `/srv/appdata`, neue Stack-Vorlage zeigt für Einstellungen und Daten dieselbe Wurzel.
- [ ] **Step 4:** Frischinstallation prüfen, soweit möglich (VM aus dem Image oder Testkopie des Pakets ohne `/srv/appdata`): Docker ist deaktiviert, Docker-Ansichten zeigen die Sperre, Einrichtung mit zwei Ordnern auf einem Pool schaltet Docker frei. Wenn keine solche Umgebung verfügbar ist, im Bericht als nicht geprüft nennen.
- [ ] **Step 5:** Gedächtnisdatei `betternas-status.md` aktualisieren (Stand, Version, offener Punkt: Installer-Restpartition).
