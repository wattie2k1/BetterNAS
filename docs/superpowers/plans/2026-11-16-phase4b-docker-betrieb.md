# Phase 4b – Docker: Logs, Shell, Stats, Update-Prüfung, Images/Netzwerke/Volumes – Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (inline). Steps use checkbox (`- [ ]`) syntax.

**Goal:** Im Container-Bereich lassen sich Logs live ansehen, eine Shell im Container öffnen (xterm.js), CPU/RAM anzeigen; Stacks zeigen „Update verfügbar“ (Digest-Vergleich); Images, Netzwerke und Volumes sind einsehbar, entfernbar und per Prune aufräumbar.

**Architecture:** Wie 4a: alles über typisierte Agent-RPCs (`docker`-CLI), die API bleibt ohne Docker-Rechte. Neu ist ein **bidirektionaler RPC-Kanal** (`rpc.Session`: Client sendet nach der Anfrage ebenfalls Zeilen), den Logs (nur Ausgabe) und Shell (PTY, Eingabe + Größe) nutzen. Die API reicht beides per WebSocket an den Browser (`/api/v1/containers/{id}/logs|shell`, Sitzungs-Cookie, Origin-Prüfung von `coder/websocket`). Stats sind ein einfacher Aufruf (`docker stats --no-stream`), den die GUI alle 5 s abfragt. Update-Prüfung: Job, der je Stack die Images (`docker compose config --images`) lokal (`image inspect` → RepoDigests) und remote (`docker buildx imagetools inspect`) vergleicht; Ergebnis in `settings` (`docker.updates`). Neue Abhängigkeit: `github.com/creack/pty` (reines Go, kein cgo).

**Spec:** `docs/specs/2026-11-15-betternas-design.md` §4.4.

## Global Constraints

- Container-ID `docker.ContainerIDRe`; Image-Referenz `^[A-Za-z0-9][A-Za-z0-9._/:@-]{0,254}$`; Netzwerk-/Image-ID `^(sha256:)?[a-f0-9]{12,64}$`; Volume-Name `^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`. Der Agent validiert jede Eingabe erneut; Argumente werden nie über eine Shell zusammengesetzt (Ausnahme: der feste Shell-Starter unten).
- Shell: `docker exec -it -e TERM=xterm-256color <id> sh -c 'command -v bash >/dev/null 2>&1 && exec bash || exec sh'` (fester String); Größe 1–500 Zeilen/Spalten; Sitzung endet, wenn der WebSocket schließt (Prozess wird beendet). Jeder Shell-Start wird auditiert (`container.shell`).
- Logs: `docker logs -f --tail <N> <id>` mit N ∈ [0, 5000] (Standard 200); Ausgabe stdout+stderr.
- Prune/Entfernen immer mit Bestätigungsdialog in der GUI; `volume prune` warnt ausdrücklich vor Datenverlust. Eingebaute Netzwerke (`bridge`, `host`, `none`) sind nicht entfernbar.
- Alle Texte deutsch; Fehlerformat und Audit wie 4a.

## Tasks

### Task 1: RPC-Sitzungen und Streaming-Ausführung
**Files:** Modify `internal/rpc/server.go`, `client.go`; Create `internal/sysexec/stream.go`, `stream_fake.go`; Test `rpc/rpc_test.go`, `sysexec/stream_test.go`.
- `Server.HandleSession(method, func(ctx, params, in <-chan Event, emit func(Event) error) error)`; der Server liest nach der Anfrage Zeilen als `Event` in `in` (schließt bei EOF und bricht ctx ab). `Client.OpenSession(ctx, method, params) (*Session, error)` mit `Events() <-chan Event`, `Send(Event) error`, `Close()`.
- `sysexec.Streamer` { `Start(ctx, name, args...) (io.ReadCloser, error)` (stdout+stderr, Close beendet den Prozess), `StartPTY(ctx, rows, cols, name, args...) (PTY, error)` } mit `PTY` { `io.ReadWriteCloser`, `Resize(rows, cols uint16) error` }; echte Implementierung mit `creack/pty`, `FakeStreamer` für Tests.
- [ ] Tests (`TestSessionBidirectional`, `TestSessionClosesOnClientClose`, `TestStreamStartAndClose`) → Implementierung → `go test ./internal/rpc ./internal/sysexec`; Commit `feat(rpc): bidirectional sessions and streaming exec`.

### Task 2: Logs, Shell, Stats (Agent + API)
**Files:** Modify `internal/docker/docker.go` (Typen, Regexe), `internal/agent/server.go` (`Deps.Stream`), `cmd/betternas-agent/main.go`; Create `internal/agent/docker_ops.go`, `internal/api/docker_stream.go`; Test dazu.
- Agent: Session `container.logs {id, tail}` (Events `data` mit `[]byte`), Session `container.shell {id, rows, cols}` (Events `data` aus/ein, `resize {rows, cols}` ein, `exit` aus), RPC `container.stats` → `[]docker.ContainerStats{ID, Name, CPU float64, MemUsage, MemLimit uint64, MemPercent float64}` (`docker stats --no-stream --no-trunc --format '{{json .}}'`, Parser in `internal/docker`).
- API: `GET /api/v1/containers/{id}/logs?tail=` (WS, Text), `GET /api/v1/containers/{id}/shell?rows=&cols=` (WS: Binär = Eingabe/Ausgabe, Text-JSON `{"type":"resize","rows":..,"cols":..}`), `GET /api/v1/containers/stats`.
- [ ] Tests: Agent mit FakeStreamer (Argumentvektor, ID-/Tail-/Größen-Validierung, Resize, Prozessende → `exit`), Parser (`TestParseStats` inkl. `1.5GiB / 7.7GiB`), API (`TestShellRequiresSession`, WebSocket-Rundlauf Logs mit Stub-Agent) → Implementierung → Commit `feat: container logs, shell and stats`.

### Task 3: Update-Prüfung per Digest
**Files:** Create `internal/agent/docker_updates.go`; Modify `internal/docker/manager.go`, `internal/api/docker.go`; Test dazu.
- Agent `stack.images {name}` → `[]string`; `image.digests {images}` → je Image `{image, local, remote, outdated, error}` (lokal: `docker image inspect --format '{{json .RepoDigests}}'`; remote: `docker buildx imagetools inspect <ref> --format '{{json .Manifest.Digest}}'`; fehlt local → `outdated:false, error:"nicht lokal vorhanden"`; Remote-Fehler → `error`, nie `outdated`).
- Manager: `CheckUpdates(ctx, actor)` → Job, ruft je Stack `stack.images` + `image.digests`, speichert `docker.updates` (`{checkedAt, stacks:{name:{outdated:[…], errors:[…]}}}`); `Updates(ctx)` liest. Routen `GET /api/v1/docker/updates`, `POST /api/v1/docker/updates/check` (202 `{jobId}`).
- [ ] Tests (Parser, outdated-Logik, Job speichert Ergebnis, Fehler eines Images bricht die übrigen nicht ab) → Commit `feat: docker image update check`.

### Task 4: Images, Netzwerke, Volumes
**Files:** Create `internal/agent/docker_resources.go`; Modify `internal/docker/parse.go`, `manager.go`, `internal/api/docker.go`; Test dazu.
- Agent RPCs: `image.list`, `network.list`, `volume.list` (`docker … ls --format '{{json .}}'`; Volumes mit `dangling`-Markierung; Netzwerke mit `builtin`), `resource.remove {kind: image|network|volume, ref}`, `resource.prune {kind, all}` (`image prune -f [-a]`, `network prune -f`, `volume prune -f`) → `{reclaimed string}`.
- API: `GET /api/v1/docker/images|networks|volumes`, `DELETE /api/v1/docker/{kind}/{ref}`, `POST /api/v1/docker/{kind}/prune` (Body `{"all":bool}`), Audit + `docker.changed`.
- [ ] Tests (Parser, Regeln je Art, eingebaute Netzwerke nicht entfernbar, Prune-Argumente) → Commit `feat: docker images, networks and volumes`.

### Task 5: Frontend – Container-Betrieb
**Files:** Modify `api/docker.ts`, `stores/docker.ts`, `views/docker/ContainersView.vue`, `package.json` (`@xterm/xterm`, `@xterm/addon-fit` fest gepinnt); Create `components/docker/LogsDialog.vue`, `ShellDialog.vue`, `utils/terminal.ts` (xterm lazy); Test `__tests__/docker-ops.test.ts`.
- Container-Tabelle: CPU %/RAM-Spalten (Stats alle 5 s, nur solange die Seite sichtbar ist), Aktionen „Logs“ und „Shell“ (nur für laufende). Logs-Dialog: WebSocket, Autoscroll, Pause, Löschen, Zeilenlimit 5000. Shell-Dialog: xterm, Größenanpassung per Fit-Addon → `resize`.
- [ ] Tests (Stats-Polling startet/stoppt, Logs-Dialog hängt Text an und begrenzt, Shell sendet Eingabe/Größe — WebSocket und xterm gestubbt) → Commit `feat(web): container logs, shell and stats`.

### Task 6: Frontend – Updates und Ressourcen
**Files:** Modify `views/docker/StacksView.vue`, `components/AppLayout.vue`, `router.ts`; Create `views/docker/ResourcesView.vue`; Test `__tests__/docker-resources.test.ts`.
- Stacks: Tag „Update verfügbar“ (Tooltip mit Images), Button „Auf Updates prüfen“ (Job). Ressourcen-Seite `/docker/ressourcen` mit Reitern Images/Netzwerke/Volumes: Tabellen, Entfernen und „Aufräumen“ mit ConfirmDialog (Volumes: Datenverlust-Warnung).
- [ ] Tests → Commit `feat(web): docker updates and resources`.

### Task 7: Abschluss
- [ ] `make test`; `make build web deb VERSION=0.3.0-rc2`; `make deploy`; Integrationstest `TestDockerOps` (Logs eines laufenden busybox-Containers, Shell-PTY `echo`, Stats, Image-List, Update-Prüfung) in `make integration`; Doku (Spec §8, Memory), danach „In main mergen“ auf Nachfrage.

## Nicht in 4b
Compose-Schema-Validierung im Editor (`monaco-yaml`), Container-Erstellung außerhalb von Stacks, Registry-Anmeldung, Dateien im Container kopieren.
