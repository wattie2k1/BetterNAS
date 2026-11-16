# Phase 4a – Docker: Einrichtung, Stacks, Container – Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (inline) oder superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Docker ist im Image/Paket enthalten und eingerichtet; in der GUI lassen sich Compose-Stacks anlegen, bearbeiten (Monaco), validieren, starten/stoppen/aktualisieren/löschen und Container auflisten und steuern. (Etappe B – Logs-Stream, Shell, Stats, Update-Prüfung per Digest, Images/Netzwerke/Volumes – folgt in `phase4b`.)

**Architecture (Abweichung von Spec §2/§4.4, bewusst):** Die API greift **nicht** auf `docker.sock` zu (Gruppe `docker` = root-äquivalent). Alle Docker-Operationen sind typisierte Agent-RPCs, die die `docker`-CLI über `sysexec.Runner` aufrufen. Stacks sind **nur Dateien** unter `/srv/appdata/stacks/<name>/{compose.yaml,.env}` (root, 0600/0700; keine DB-Tabelle – die Dateien sind die Wahrheit, per CLI weiter nutzbar). Deploy/Pull/Down laufen wie Updates und SnapRAID in einer transienten systemd-Unit (`betternas-stack-<name>-<ts>`, übersteht Neustart von API/Agent, Log aus dem Journal); die API begleitet sie als Job und pollt `stack.runstatus`. Muster: `agent/updates.go`, `agent/snapraid.go`, `system/updates.go`.

**Tech Stack:** Go 1.24 (kein cgo), Debian-Pakete `docker.io` + `docker-compose` (Compose-v2-Plugin, Trixie), Vue 3.5 + PrimeVue 4 + Pinia, `monaco-editor` (nur YAML-Grundmodus, lazy geladen), Vitest.

**Spec:** `docs/specs/2026-11-15-betternas-design.md` §3, §4.4, §6.

## Global Constraints

- Stackname `^[a-z0-9][a-z0-9_-]{0,39}$` (`docker.StackNameRe`); der Agent validiert jeden Namen erneut und löst Pfade nur über `docker.StackDir(name)` auf (nie Pfade aus Eingaben).
- Container-ID `^[a-f0-9]{12,64}$`; erlaubte Container-Aktionen `start|stop|restart|remove`; erlaubte Stack-Aktionen `up|down|stop|start|restart|pull|update` (`update` = `pull` + `up -d`).
- `compose.yaml` ≤ 256 KiB, `.env` ≤ 64 KiB. Speichern ist ohne Validierung möglich (Entwurf); **Deploy (`up`/`update`) läuft nur, wenn `docker compose config -q` erfolgreich ist**, sonst `invalid_params` mit Compose-Fehlertext.
- Löschen eines Stacks: erst `down` (ohne `-v`; Volumes bleiben, GUI sagt das), dann Verzeichnis entfernen; läuft ein Lauf desselben Stacks → `conflict`. Pro Stack höchstens ein Lauf (`systemctl list-units betternas-stack-<name>-*`).
- Docker `data-root` = `/srv/appdata/docker` (nicht mergerfs), `log-driver json-file` mit `max-size 10m`, `max-file 3` (`/etc/docker/daemon.json`, im Paket).
- Fehlerformat `{"error":{"code","message","details"}}`, Codes aus `internal/apierr`; alle sichtbaren Texte deutsch; jede ändernde API-Aktion schreibt einen Audit-Eintrag (`audit.Record`).

## Review Focus

1. Pfad-/Namensinjektion: `..`, `/`, Großbuchstaben, Leerzeichen im Stack- und Containernamen werden vom Agent abgelehnt (`TestStackNameValidation`).
2. Ungültige Compose-Datei darf nie deployt werden; gespeicherter Entwurf bleibt erhalten (`TestDeployRefusesInvalidCompose`).
3. Zwei Läufe desselben Stacks gleichzeitig → `conflict` (`TestRunStackRefusesWhenRunning`).
4. Löschen entfernt nur `/srv/appdata/stacks/<name>`, nie ein anderes Verzeichnis (`TestDeleteStackOnlyOwnDir`).
5. `.env` wird nie an die Stack-Liste ausgeliefert (nur über `stack.get`, nur Admins) und mit Mode 0600 geschrieben.

---

### Task 1: Docker installieren und einrichten

**Files:** Modify `image/packages/rootfs.list`, `packaging/build-deb.sh`, `packaging/scripts/postinst.sh`; Create `packaging/docker/daemon.json`.

- [ ] **Step 1:** `docker.io` und `docker-compose` in `rootfs.list`; in `Depends` von `build-deb.sh` ergänzen; `daemon.json` per `install -D -m 0644` nach `/etc/docker/daemon.json` (Inhalt siehe Constraints); `postinst.sh`: `install -d -m 0755 /srv/appdata/stacks /srv/appdata/docker`.
- [ ] **Step 2:** `sh -n` auf die Skripte, `python3 -m json.tool packaging/docker/daemon.json`; `image/tests/check-rootfs.sh` prüft zusätzlich `docker` und `/usr/libexec/docker/cli-plugins/docker-compose` (bzw. Pfad des Pakets).
- [ ] **Step 3: Commit** – `feat(packaging): docker engine, compose and daemon config`

### Task 2: Paket `internal/docker` – Namen, Pfade, Parser

**Files:** Create `backend/internal/docker/docker.go`, `parse.go`; Test `docker_test.go`, `parse_test.go`, `testdata/ps.jsonl`, `testdata/compose-ls.json`.

**Interfaces (Produces):**
- `const StacksDir = "/srv/appdata/stacks"`; `StackNameRe`, `ContainerIDRe`; `func StackDir(name string) string`; `ComposeFile = "compose.yaml"`, `EnvFile = ".env"`, Größenlimits, `StackActions`, `ContainerActions` (Maps).
- `type Container struct { ID, Name, Image, State, Status, Stack string; Ports []string; Created int64 }` (`Stack` aus Label `com.docker.compose.project`, nur wenn der Stack unter `StacksDir` existiert/gelistet ist).
- `func ParseContainers(out []byte) ([]Container, error)` für `docker ps -a --no-trunc --format '{{json .}}'` (je Zeile ein JSON-Objekt; `Labels` ist ein Komma-String).
- `type StackInfo struct { Name string; Running, Total int; Updated int64 }`; `func ParseComposeLs(out []byte) (map[string]struct{ Running, Total int }, error)` für `docker compose ls -a --format json` (`Status` z. B. `running(2), exited(1)`).
- `type RunState struct { State string; Log string }` (`running|succeeded|failed`).

- [ ] **Step 1: Tests** – `TestStackNameValidation` (gültig `jellyfin`, `a_b-1`; ungültig `""`, `../x`, `A`, `a b`, 41 Zeichen), `TestStackDir`, `TestParseContainers` (Fixture mit Compose- und Nicht-Compose-Container, Ports `0.0.0.0:8096->8096/tcp, :::8096->8096/tcp`), `TestParseComposeLs`.
- [ ] **Step 2:** `cd backend && go test ./internal/docker` → FAIL. **Step 3:** implementieren. **Step 4:** PASS.
- [ ] **Step 5: Commit** – `feat(docker): names, paths and output parsers`

### Task 3: Agent – Stack-Dateien (list/get/save/validate/delete)

**Files:** Create `backend/internal/agent/docker.go`; Modify `agent/server.go` (Registrierung); Test `agent/docker_test.go` (FakeRunner + FakeFS aus `sysexec`).

**Interfaces:** RPC `stack.list` → `[]docker.StackInfo` (Verzeichnisse mit `compose.yaml` + Laufzustand aus `docker compose ls -a`; Docker nicht erreichbar → Liste trotzdem, Zähler 0); `stack.get {name}` → `{name, compose, env}`; `stack.save {name, compose, env}` (Verzeichnis 0700, Dateien 0600, atomar via Temp+Rename; `mutate`-Sperre); `stack.validate {name}` → `{ok, message}` (`docker compose --project-directory <dir> config -q`); `stack.delete {name}`.

- [ ] **Step 1: Tests** – `TestSaveAndGetStack`, `TestStackFilesAreMode0600` (FakeFS merkt Modi), `TestStackSizeLimits`, `TestValidateStackReportsComposeError` (Runner liefert ExitCode 1 + stderr → `ok:false`, Meldung enthalten), `TestDeleteStackOnlyOwnDir` (nur `StackDir(name)` wird entfernt; Runner sah `compose down`), `TestListStacksIgnoresDirsWithoutCompose`.
- [ ] **Step 2–4:** FAIL → implementieren → `go test ./internal/agent` PASS. **Step 5: Commit** – `feat(agent): compose stack files and validation`

### Task 4: Agent – Läufe, Container, Docker-Status

**Files:** Modify `agent/docker.go`, `agent/server.go`; Test `agent/docker_test.go`.

**Interfaces:** `stack.run {name, action}` → `{unit}` (validiert, bei `up`/`update` zuerst `config -q`; `systemd-run --unit=betternas-stack-<name>-<ts> --no-block --property=RemainAfterExit=yes --description=… docker compose --project-directory <dir> <args>`; `update` = Shell-freie Kette über zwei Aufrufe: `pull`, dann `up -d --remove-orphans` in einer Unit per `sh -c` **ohne** Nutzereingaben im String – der Stackname ist durch die Regex gesichert; Aufräumen alter Units wie in `InstallUpdates`); `stack.runstatus {unit}` → `docker.RunState` (Unit-Regex `^betternas-stack-[a-z0-9_-]+-[0-9]+$`, Log: `journalctl -u … -n 200`); `container.list` → `[]docker.Container`; `container.action {id, action}`; `docker.status` → `{installed, active, version}` (`docker version --format {{.Server.Version}}`, `systemctl is-active docker`).

- [ ] **Step 1: Tests** – `TestRunStackBuildsSystemdRun` (genauer Argumentvektor), `TestRunStackRefusesWhenRunning`, `TestDeployRefusesInvalidCompose`, `TestRunStackRejectsUnknownAction`, `TestRunStatusUnitValidation`, `TestContainerActionValidation` (ID `../x`, Aktion `exec` → `invalid_params`), `TestContainerList` (Runner-Fixture), `TestDockerStatusNotInstalled`.
- [ ] **Step 2–4:** FAIL → implementieren → PASS. **Step 5: Commit** – `feat(agent): stack runs, containers and docker status`

### Task 5: API – Manager und Routen

**Files:** Create `backend/internal/docker/manager.go`, `backend/internal/api/docker.go`; Modify `api/server.go` (`Deps.Docker *docker.Manager`, `registerDocker`), `cmd/betternas-api/main.go` (Verdrahtung); Test `docker/manager_test.go`, `api/docker_api_test.go` (Muster `snapraid_api_test.go`).

**Routen (Admin-Session, CSRF wie übrige):** `GET /api/v1/docker/status`; `GET /api/v1/stacks`; `POST /api/v1/stacks` `{name, compose, env}` (409 bei vorhandenem Namen); `GET|PUT|DELETE /api/v1/stacks/{name}`; `POST /api/v1/stacks/{name}/validate`; `POST /api/v1/stacks/{name}/{action}` (`up|down|stop|start|restart|pull|update`) → `202 {jobId}`; `GET /api/v1/containers`; `POST /api/v1/containers/{id}/{action}` → `204`. Manager: Audit (`stack.save|delete|run`, `container.action`), Job pro Lauf (`jobs.Start("stack."+action, name, actor, …)` pollt alle 2 s `stack.runstatus`, spiegelt das Log, endet mit `failed` bei Unit-Fehler), Event `docker.changed` über den Hub nach jeder Änderung.

- [ ] **Step 1: Tests** – Manager mit Fake-Agent: `TestRunStackTracksJobToSuccess`, `TestRunStackFailureMarksJobFailed`, `TestCreateStackConflict`, `TestAuditRecorded`; API: `TestStacksRequireSession`, `TestRunReturns202WithJobId`, `TestInvalidNameIs400`.
- [ ] **Step 2–4:** FAIL → implementieren → `go vet ./... && go test ./...` PASS. **Step 5: Commit** – `feat(api): docker stacks and containers`

### Task 6: Frontend – API-Client und Store

**Files:** Create `frontend/src/api/docker.ts`, `frontend/src/stores/docker.ts`; Modify `components/AppLayout.vue` (`docker.changed`-Event → `docker.handleEvent`); Test `frontend/src/__tests__/docker-store.test.ts`.

- [ ] **Step 1: Tests** – Store lädt Stacks/Container, `run(name, action)` verfolgt den Job über `useJobsStore().track` und lädt neu; `handleEvent` lädt bei `docker.changed`; Fehler → `error`.
- [ ] **Step 2–4:** FAIL → implementieren → `npm test` PASS. **Step 5: Commit** – `feat(web): docker api client and store`

### Task 7: Frontend – Ansichten

**Files:** Create `frontend/src/views/docker/StacksView.vue`, `ContainersView.vue`, `frontend/src/components/docker/StackDialog.vue`, `YamlEditor.vue` (Monaco, per `defineAsyncComponent` geladen, nur `yaml`-Sprache, Worker per Vite `?worker`), `StackDeleteDialog.vue`; Modify `router.ts` (`/docker/stacks`, `/docker/container`), `AppLayout.vue` (zwei Dock-Einträge, Symbol `pi-box`/`pi-server`, Verläufe passend zum Design), `package.json` (`monaco-editor` fest gepinnt); Test `__tests__/StacksView.test.ts`, `StackDialog.test.ts`.

Verhalten: Stacks-Karten/Tabelle (Name, Laufzustand „2/3 laufen“, Aktionen Start/Stopp/Neustart/Aktualisieren/Bearbeiten/Löschen); Dialog „Stack“ mit Namensfeld (nur beim Anlegen), YAML-Editor (Vorlage mit `services:` bei neu), `.env`-Reiter, Button „Prüfen“ (zeigt Compose-Fehler), „Speichern“ und „Speichern & starten“; Löschen-Dialog nennt: „Volumes bleiben erhalten“; leere Zustände und Docker-nicht-aktiv-Hinweis deutsch; Container-Tabelle (Name, Image, Status, Ports, Stack) mit Start/Stopp/Neustart/Entfernen (ConfirmDialog). Texte nur in den Views, Logik im Store.

- [ ] **Step 1: Tests** (Komponenten mit gemocktem Store, Editor gestubbt): Liste rendert, Deploy ruft `run`, Löschen verlangt Bestätigung, Prüfen zeigt Fehlertext, Docker inaktiv zeigt Hinweis.
- [ ] **Step 2–4:** FAIL → implementieren → `npm test && npm run build` PASS (Chunk-Größe des Editors notieren). **Step 5: Commit** – `feat(web): docker stacks and containers views`

### Task 8: Abschluss und Test auf dem Testgerät

- [ ] **Step 1:** `make test`; `make build web deb VERSION=0.3.0-rc1`; `make deploy VERSION=0.3.0-rc1` auf `betternas-dev` (zieht `docker.io`/`docker-compose` per apt).
- [ ] **Step 2: Integrationstest** (`//go:build integration`, `agent/docker_integration_test.go`, in `make integration` aufnehmen, überspringt ohne Docker): Stack anlegen → `up` (kleines `busybox`-Image, `sleep`) → `container.list` zeigt ihn → `down` → `delete`.
- [ ] **Step 3:** Manuell in der GUI: Stack mit ungültigem YAML (Fehlertext), gültiger Stack deployen, Container stoppen, Stack löschen. Ergebnis im Ledger festhalten.
- [ ] **Step 4:** Doku (`docs/specs` §8 Phase 4a, Abweichungen), Memory aktualisieren; **Commit** – `docs: docker stacks (phase 4a)`. Danach „In main mergen“ auf Nachfrage.

## Offen für Etappe B

Logs-Stream (`HandleStream`), Shell (xterm.js über WebSocket + PTY im Agent), Stats, Update-Prüfung per Digest, Images/Netzwerke/Volumes mit Prune, Compose-JSON-Schema-Validierung im Editor (`monaco-yaml`).
