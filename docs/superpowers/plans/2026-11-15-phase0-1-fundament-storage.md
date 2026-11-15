# BetterNAS Phase 0 + 1 (Fundament & Storage) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ein auf Debian 13 installierbares `.deb`, das eine Web-GUI mit Login bereitstellt, über die Datenplatten erkannt, formatiert/importiert und zu mergerfs-Pools zusammengefasst, bearbeitet und gelöscht werden können.

**Architecture:** Zwei Go-Binaries: `betternas-api` (unprivilegiert, HTTP/WS, SQLite, Jobs, Auth) und `betternas-agent` (root, typisierte JSON-RPC-Operationen über Unix-Socket). Die API hält den Soll-Zustand in SQLite, berechnet daraus einen `storage.State` und lässt den Agent diesen per `storage.apply` als systemd-Mount-Units idempotent anwenden. Die Vue-SPA wird von der API statisch ausgeliefert.

**Tech Stack:** Go 1.24 (`CGO_ENABLED=0`), chi v5, modernc.org/sqlite, github.com/coder/websocket, github.com/pilebones/go-udev, golang.org/x/sys/unix · Vue 3.5 + TypeScript 5 + Vite 6, Pinia 3, Vue Router 4, PrimeVue 4, Vitest 3 · nfpm für `.deb`.

**Spec:** `docs/specs/2026-11-15-betternas-design.md`

**Ausführungsreihenfolge:** Task 1, 2 und 10 dieses Plans → Plan `2026-11-15-phase0ab-distribution-installer.md` (Image + Installer) → restliche Tasks dieses Plans. Ab dann wird jede Komponente im eigenen ISO ausgeliefert und in VMs getestet, die aus diesem ISO installiert wurden.

## Global Constraints

- Zielsystem ist das BetterNAS-Image (Debian-13-„Trixie“-Basis); Binaries für `amd64` **und** `arm64` müssen in CI bauen; immer `CGO_ENABLED=0`.
- Go-Modulpfad: `github.com/betternas/betternas` (Platzhalter, später per `go mod edit -module` änderbar).
- `betternas-api` läuft als Systembenutzer `betternas`; `betternas-agent` als root.
- Agent-Socket: `/run/betternas/agent.sock`, Modus `0660`, Gruppe `betternas`; Agent prüft Peer-UID per `SO_PEERCRED` (erlaubt: root und UID von `betternas`).
- Pfade: Datenplatten `/srv/disks/<disk-id>`, Pools `/srv/pools/<pool>`, DB `/var/lib/betternas/betternas.db`, TLS `/etc/betternas/tls/{cert,key}.pem`, Web-UI `/usr/share/betternas/web`.
- Jede generierte Datei beginnt mit der Zeile `# Verwaltet von BetterNAS – nicht manuell bearbeiten`. Nur Dateien mit diesem Kopf werden geändert oder gelöscht.
- Defaults: Dateisystem `ext4` (Auswahl `ext4`, `xfs`), Create-Policy `epmfs` (Auswahl `epmfs`, `mfs`, `lfs`, `pfrd`), `minfreespace` `20G`.
- Admin = Mitglied der Gruppe `betternas-admin`.
- Fehlerformat überall: `{"error":{"code":"...","message":"...","details":{...}}}`; `message` auf Deutsch.
- GUI nur aus privaten Netzen erreichbar (127.0.0.0/8, ::1, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16, fc00::/7, fe80::/10), sonst 403.
- UI-Texte auf Deutsch.
- **Bewusste Abweichungen von der Spec (Phase 0/1):** (a) Ein einziges Paket `betternas` statt getrennter Pakete – Aufteilung bei Phase 6. (b) Passwortprüfung über `/usr/sbin/unix_chkpwd` (Helper von `pam_unix`, gleiche Passwortdatenbank), weil libpam cgo erfordert. (c) Rolle `parity` existiert im Datenmodell, wird aber erst in Phase 3 nutzbar.

## Review Focus

1. **USB-Gehäuse mit identischer oder fehlender Seriennummer** – zwei Platten dürfen nie dieselbe ID bekommen; beide werden „nicht verwaltbar“ mit Begründung angezeigt (Test in Task 10).
2. **Platte eines Pools fehlt beim Booten / wird abgezogen** – Pool muss mit den übrigen Platten trotzdem einhängen (`Wants=` statt `Requires=`), UI zeigt die Platte als `missing` (Tests in Task 11 und 14).
3. **Pool wird bearbeitet, während eine Datei darauf geöffnet ist** – Agent meldet `pool_busy`, alte Unit-Dateien werden wiederhergestellt, DB bleibt unverändert (Tests in Task 12 und 14).
4. **Platte ändert sich zwischen Vorschau und Formatieren** (anderes Gerät eingesteckt, inzwischen gemountet, andere Größe) – Formatieren wird mit `conflict` abgelehnt, nichts wird gelöscht (Tests in Task 12 und 14).
5. **Sonderzeichen in Pool-Namen oder Disk-IDs** (`../`, `:`, `,`, Leerzeichen) – dürfen weder Pfade verlassen noch mergerfs-Branch-Listen bzw. Mount-Optionen aufbrechen (Tests in Task 10 und 14).

---

## Dateistruktur

```
BetterNAS/
├── .github/workflows/ci.yml
├── Makefile  .gitignore  README.md  nfpm.yaml
├── backend/
│   ├── go.mod
│   ├── cmd/betternas-api/main.go
│   ├── cmd/betternas-agent/main.go
│   └── internal/
│       ├── version/version.go
│       ├── apierr/apierr.go              Fehlercodes ↔ HTTP-Status
│       ├── sysexec/{runner.go,fs.go,fake.go}
│       ├── rpc/{protocol.go,server.go,client.go,peercred_linux.go,peercred_other.go}
│       ├── db/{db.go,migrations/0001_init.sql}
│       ├── audit/audit.go
│       ├── auth/{sessions.go,ratelimit.go,lan.go,handlers.go}
│       ├── jobs/jobs.go
│       ├── events/hub.go                  WS-Hub
│       ├── storage/{lsblk.go,diskid.go,state.go,escape.go,render.go}   reine Logik
│       ├── storage/manager.go             API-seitige Orchestrierung
│       ├── agent/{server.go,auth.go,disks.go,format.go,apply.go,smart.go,udev.go,sysinfo.go}
│       └── api/{server.go,tls.go,static.go,system.go,jobs.go,storage.go,ws.go}
├── frontend/   (Vite-Projekt, Details in Task 8/15)
├── packaging/
│   ├── systemd/{betternas-api.service,betternas-agent.service}
│   └── scripts/{postinst.sh,prerm.sh}
└── test/integration/{README.md,storage_test.go}
```

---

### Task 1: Repository-Grundgerüst und CI

**Files:**
- Create: `.gitignore`, `README.md`, `Makefile`, `.github/workflows/ci.yml`, `backend/go.mod`, `backend/internal/version/version.go`, `backend/internal/version/version_test.go`

**Interfaces:**
- Produces: `version.Version string` (per `-ldflags "-X github.com/betternas/betternas/internal/version.Version=..."`, Default `"dev"`); Make-Targets `test`, `build`, `build-arm64`, `web`, `deb`, `deploy VM=<ssh-host>`.

- [ ] **Step 1:** `git init`, `.gitignore` (`bin/`, `dist/`, `frontend/node_modules/`, `frontend/dist/`, `*.db`).
- [ ] **Step 2: Failing test** `TestVersionDefault`: `if Version != "dev" { t.Fatal(Version) }`.
- [ ] **Step 3:** Run `cd backend && go test ./...` → FAIL (Paket fehlt).
- [ ] **Step 4:** `go mod init github.com/betternas/betternas`, `go 1.24`; `version.go` mit `var Version = "dev"`.
- [ ] **Step 5:** Makefile: `build` = `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../bin/amd64/ ./cmd/...`, `build-arm64` analog, `test` = `go vet ./... && go test ./...` plus `npm --prefix frontend test` (sobald vorhanden), `deploy` = `scp dist/*.deb $(VM):/tmp/ && ssh $(VM) sudo apt install -y /tmp/betternas_*.deb`. README: Entwicklung auf macOS, Ausführung in Debian-13-VM (UTM), Vite-Dev-Server proxyt auf die VM.
- [ ] **Step 6:** CI-Workflow: Job `backend` (Go 1.24: vet, test, build amd64 + arm64), Job `frontend` (Node 22: `npm ci`, `npm test`, `npm run build`), Job `package` (nfpm, abhängig von beiden; ab Task 9 aktiv).
- [ ] **Step 7:** Run `make test` → PASS.
- [ ] **Step 8:** Commit `chore: repository skeleton and CI`.

---

### Task 2: sysexec – testbare Befehle und Dateizugriffe

**Files:**
- Create: `backend/internal/sysexec/runner.go`, `fs.go`, `fake.go`, `sysexec_test.go`

**Interfaces:**
- Produces:
  ```go
  type Result struct{ Stdout, Stderr []byte; ExitCode int }
  type Runner interface { Run(ctx context.Context, stdin []byte, name string, args ...string) (Result, error) } // error nur bei Start-Fehler, nicht bei ExitCode≠0
  func NewRunner() Runner
  type FS interface {
      ReadFile(path string) ([]byte, error); WriteFile(path string, data []byte, perm os.FileMode) error
      Remove(path string) error; MkdirAll(path string, perm os.FileMode) error; Glob(pattern string) ([]string, error)
  }
  func NewRootFS(root string) FS        // alle Pfade relativ zu root; Pfade mit ".." nach Clean → Fehler
  type FakeRunner struct{ Calls []string; Responses map[string]Result } // Key = "name arg1 arg2"; unbekannter Key → ExitCode 0, leere Ausgabe
  ```

- [ ] **Step 1: Failing tests:**
  - `TestRootFSConfinesPaths`: `NewRootFS(tmp).WriteFile("/etc/x", …)` landet in `tmp/etc/x`; `WriteFile("/../../etc/passwd")` → Fehler.
  - `TestRootFSWriteIsAtomic`: nach `WriteFile` existiert keine `*.tmp`-Datei im Verzeichnis (Schreiben über Temp-Datei + `os.Rename`).
  - `TestFakeRunnerRecordsCalls`: zwei Aufrufe → `Calls == []string{"wipefs --all /dev/sdb", "udevadm settle"}`; Antwort aus `Responses` wird zurückgegeben.
  - `TestRunnerExitCode`: `NewRunner().Run(ctx, nil, "sh", "-c", "exit 3")` → `ExitCode == 3`, `err == nil`.
- [ ] **Step 2:** Run `go test ./internal/sysexec/` → FAIL.
- [ ] **Step 3:** Implementieren (Runner via `exec.CommandContext`, `Env` = `LC_ALL=C` + `PATH`).
- [ ] **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `feat(sysexec): command runner and rooted filesystem`.

---

### Task 3: Agent-RPC über Unix-Socket

**Files:**
- Create: `backend/internal/rpc/protocol.go`, `server.go`, `client.go`, `peercred_linux.go`, `peercred_other.go`, `rpc_test.go`
- Create: `backend/internal/apierr/apierr.go`, `apierr_test.go`

**Interfaces:**
- Produces:
  ```go
  // apierr
  type Error struct{ Code, Message string; Details map[string]any }   // implements error
  func New(code, message string) *Error
  const (InvalidParams="invalid_params"; Unauthorized="unauthorized"; Forbidden="forbidden"; NotFound="not_found";
         Conflict="conflict"; PoolBusy="pool_busy"; DiskInUse="disk_in_use"; SystemDisk="system_disk";
         RateLimited="rate_limited"; Internal="internal")
  func HTTPStatus(code string) int   // 400,401,403,404,409,409,409,409,429,500
  func Write(w http.ResponseWriter, err error) // *Error → Status+JSON; sonst 500 "internal"
  // rpc – Zeilenbasiertes JSON, eine Verbindung pro Call
  type Request  struct{ ID uint64; Method string; Params json.RawMessage }
  type Response struct{ ID uint64; Result json.RawMessage; Error *apierr.Error }
  type Event    struct{ Event string; Data json.RawMessage }
  type Handler func(ctx context.Context, params json.RawMessage) (any, error)
  type StreamHandler func(ctx context.Context, emit func(Event) error) error
  func NewServer(allowedUIDs []uint32) *Server
  func (s *Server) Handle(method string, h Handler)
  func (s *Server) HandleStream(method string, h StreamHandler)
  func (s *Server) Serve(l net.Listener) error
  func NewClient(socketPath string) *Client
  func (c *Client) Call(ctx context.Context, method string, params, result any) error // Fehler als *apierr.Error
  func (c *Client) Subscribe(ctx context.Context, method string) (<-chan Event, error)
  ```
  JSON-Feldnamen in lowerCamelCase (`id`, `method`, `params`, `result`, `error`, `event`, `data`).

- [ ] **Step 1: Failing tests** (Socket in `t.TempDir()`):
  - `TestCallRoundTrip`: Handler `echo` gibt Params zurück → `Call` liefert `{"a":1}`.
  - `TestUnknownMethod`: Fehlercode `not_found`.
  - `TestHandlerErrorPropagates`: Handler gibt `apierr.New("pool_busy","…")` zurück → Client erhält `*apierr.Error` mit Code `pool_busy`; nicht-apierr-Fehler → Code `internal`, Message ohne interne Details.
  - `TestPeerUIDRejected` (nur Linux, `//go:build linux`): `NewServer([]uint32{4242})` → Verbindung des Testprozesses wird ohne Antwort geschlossen, `Call` liefert Fehler.
  - `TestSubscribeReceivesEvents`: StreamHandler emittiert zwei Events → Kanal liefert beide, Abbruch per ctx schließt Kanal.
  - `TestHTTPStatus`: `pool_busy`→409, `rate_limited`→429, unbekannt→500.
- [ ] **Step 2:** Run `go test ./internal/rpc/ ./internal/apierr/` → FAIL.
- [ ] **Step 3:** Implementieren. Peer-Cred via `unix.GetsockoptUcred` (Linux); `peercred_other.go` erlaubt alles (nur für Entwicklung auf macOS). Ein Request pro Verbindung, max. Zeilengröße 1 MiB.
- [ ] **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `feat(rpc): agent JSON-RPC over unix socket`.

---

### Task 4: Datenbank, Migrationen, Audit-Log

**Files:**
- Create: `backend/internal/db/db.go`, `backend/internal/db/migrations/0001_init.sql`, `db_test.go`, `backend/internal/audit/audit.go`, `audit_test.go`

**Interfaces:**
- Produces:
  ```go
  func Open(path string) (*sql.DB, error)   // modernc sqlite, PRAGMA journal_mode=WAL, foreign_keys=ON, busy_timeout=5000; führt Migrationen aus
  func Record(ctx context.Context, db *sql.DB, e Entry) error
  type Entry struct{ Actor, Action, Target, Result string; Details map[string]any } // Result: "ok" | "error"
  func List(ctx context.Context, db *sql.DB, limit int) ([]StoredEntry, error)  // neueste zuerst
  ```
  Schema `0001_init.sql`:
  ```sql
  CREATE TABLE schema_version(version INTEGER NOT NULL);
  CREATE TABLE disks(id TEXT PRIMARY KEY, role TEXT NOT NULL CHECK(role IN ('data','parity')),
    fs_type TEXT NOT NULL, fs_uuid TEXT NOT NULL, label TEXT NOT NULL, model TEXT, serial TEXT,
    size_bytes INTEGER NOT NULL, created_at TEXT NOT NULL);
  CREATE TABLE pools(name TEXT PRIMARY KEY, create_policy TEXT NOT NULL, min_free TEXT NOT NULL,
    snapraid_enabled INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL);
  CREATE TABLE pool_branches(pool_name TEXT NOT NULL REFERENCES pools(name) ON DELETE CASCADE,
    disk_id TEXT NOT NULL UNIQUE REFERENCES disks(id), position INTEGER NOT NULL, PRIMARY KEY(pool_name, disk_id));
  CREATE TABLE jobs(id TEXT PRIMARY KEY, kind TEXT, target TEXT, status TEXT, progress INTEGER, log TEXT,
    started_by TEXT, started_at TEXT, finished_at TEXT);
  CREATE TABLE audit(id INTEGER PRIMARY KEY AUTOINCREMENT, ts TEXT, actor TEXT, action TEXT, target TEXT, result TEXT, details_json TEXT);
  CREATE TABLE sessions(token_hash TEXT PRIMARY KEY, username TEXT NOT NULL, csrf TEXT NOT NULL, expires_at TEXT NOT NULL);
  CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);
  ```
  Nicht verwaltete Platten (`unassigned`, `system`) stehen nicht in `disks`. `UNIQUE(disk_id)` in `pool_branches` erzwingt „eine Platte in höchstens einem Pool“.

- [ ] **Step 1: Failing tests:** `TestOpenRunsMigrationsOnce` (zweimal `Open` auf dieselbe Datei → `schema_version` = 1, kein Fehler); `TestDiskInTwoPoolsRejected` (zweiter Insert in `pool_branches` mit gleicher `disk_id` → Constraint-Fehler); `TestAuditRecordAndList` (zwei Einträge → `List` liefert neuesten zuerst, `Details` round-trip).
- [ ] **Step 2:** Run `go test ./internal/db/ ./internal/audit/` → FAIL.
- [ ] **Step 3:** Implementieren; Migrationen per `embed.FS`, in Dateinamen-Reihenfolge, jede in eigener Transaktion.
- [ ] **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `feat(db): sqlite schema, migrations and audit log`.

---

### Task 5: Authentifizierung

**Files:**
- Create: `backend/internal/agent/auth.go`, `auth_test.go`
- Create: `backend/internal/auth/sessions.go`, `ratelimit.go`, `lan.go`, `handlers.go`, `auth_test.go`

**Interfaces:**
- Consumes: `sysexec.Runner`, `rpc.Server.Handle`, `rpc.Client`, `apierr`, `audit.Record`.
- Produces:
  ```go
  // agent: Methode "auth.verify", Params {"username","password"} → Result {"ok":bool}
  func VerifyPassword(ctx context.Context, r sysexec.Runner, username, password string) (bool, error)
  // auth (API)
  type Store struct{...}; func NewStore(db *sql.DB, ttl time.Duration) *Store   // ttl = 12h, gleitend
  func (s *Store) Create(ctx, username string) (token, csrf string, err error)    // token: 32 Byte crypto/rand, base64url; DB speichert sha256
  func (s *Store) Lookup(ctx, token string) (Session, error)                      // verlängert expires_at
  func (s *Store) Delete(ctx, token string) error
  type Session struct{ Username, CSRF string }
  type Verifier interface{ Verify(ctx context.Context, username, password string) (bool, error) }
  type GroupChecker func(username, group string) (bool, error)   // Default: os/user (ohne cgo liest /etc/group)
  func LANOnly(next http.Handler) http.Handler
  func (h *Handlers) Login, Logout, Me http.HandlerFunc
  func (h *Handlers) RequireSession(next http.Handler) http.Handler  // 401 ohne Cookie; 403 bei Nicht-GET ohne passenden X-CSRF-Token
  func SessionFrom(ctx context.Context) Session
  ```
  Cookie `bnas_session`: `HttpOnly`, `SameSite=Strict`, `Secure` außer im `--dev`-Modus, `Path=/`.

- [ ] **Step 1: Failing tests (Agent):** `TestVerifyPasswordInvokesUnixChkpwd`: FakeRunner → Call `"/usr/sbin/unix_chkpwd alice nonull"`, stdin genau `"geheim\x00"`; ExitCode 0 → `true`; ExitCode 7 → `false`, kein Fehler. `TestVerifyRejectsBadUsername`: `"../x"`, `""`, `"-rf"` → `false` ohne Aufruf (Regex `^[a-z_][a-z0-9_-]{0,31}$`).
- [ ] **Step 2: Failing tests (API):**
  - `TestLoginSuccessSetsCookie`: Verifier ok + in `betternas-admin` → 200, Body `{"username":"alice","csrfToken":…}`, Cookie gesetzt; Audit-Eintrag `auth.login`/`ok`.
  - `TestLoginNonAdminIsGeneric401`: Passwort korrekt, aber nicht in Gruppe → 401 mit identischer Message wie bei falschem Passwort (`"Benutzername oder Passwort falsch"`).
  - `TestLoginRateLimited`: 5 Fehlversuche von `192.168.1.10` in 5 min → 6. Versuch 429 `rate_limited` (auch mit korrektem Passwort); andere IP nicht betroffen.
  - `TestCSRFRequiredForMutations`: gültige Session, `POST` ohne/mit falschem `X-CSRF-Token` → 403; mit korrektem → durchgereicht; `GET` ohne Token → durchgereicht.
  - `TestSessionExpires`: Store mit ttl 1ms → `Lookup` nach Ablauf → Fehler; Request → 401.
  - `TestLANOnly`: RemoteAddr `192.168.1.5:1234`, `[fd00::1]:1`, `127.0.0.1:1` → durch; `8.8.8.8:1234`, `[2001:db8::1]:1` → 403. `X-Forwarded-For` wird ignoriert.
- [ ] **Step 3:** Run `go test ./internal/auth/ ./internal/agent/` → FAIL.
- [ ] **Step 4:** Implementieren.
- [ ] **Step 5:** Run → PASS.
- [ ] **Step 6:** Commit `feat(auth): unix_chkpwd verification, sessions, CSRF, rate limit, LAN guard`.

---

### Task 6: Jobs und Event-Hub

**Files:**
- Create: `backend/internal/jobs/jobs.go`, `jobs_test.go`, `backend/internal/events/hub.go`, `hub_test.go`

**Interfaces:**
- Produces:
  ```go
  type Status string // "queued" | "running" | "succeeded" | "failed"
  type Job struct{ ID, Kind, Target string; Status Status; Progress int; Log string; StartedBy string; StartedAt time.Time; FinishedAt *time.Time } // JSON lowerCamelCase
  type Logger interface{ Printf(format string, a ...any); SetProgress(p int) }
  func NewManager(db *sql.DB, hub *events.Hub) *Manager
  func (m *Manager) Start(kind, target, actor string, fn func(ctx context.Context, log Logger) error) string // gibt Job-ID zurück, läuft in Goroutine
  func (m *Manager) Get(ctx, id string) (Job, error)          // apierr NotFound
  func (m *Manager) List(ctx, limit int) ([]Job, error)
  func (m *Manager) MarkInterrupted(ctx) error                // beim Start: running/queued → failed, Log "Durch Neustart unterbrochen"
  type Message struct{ Type string; Data any }                // Typen: "job.updated", "disks.changed", "pools.changed"
  func NewHub() *Hub; func (h *Hub) Publish(m Message); func (h *Hub) Subscribe() (<-chan Message, func())
  ```

- [ ] **Step 1: Failing tests:** `TestJobSucceeds` (fn loggt „a“, setzt Progress 50 → Endstatus `succeeded`, Progress 100, Log enthält „a“, persistiert); `TestJobFails` (fn gibt Fehler → `failed`, Fehlertext im Log); `TestJobPublishesUpdates` (Hub-Subscriber erhält mind. `running` und `succeeded`); `TestMarkInterrupted`; `TestHubSlowSubscriberDoesNotBlock` (Subscriber liest nie; 1000 Publishes kehren zurück; Puffer 64, überzählige Nachrichten werden für diesen Subscriber verworfen).
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implementieren. **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `feat(jobs): background jobs with persistence and event hub`.

---

### Task 7: API-Server, TLS, Systeminfo, WebSocket, Agent-Binary

**Files:**
- Create: `backend/internal/api/server.go`, `tls.go`, `static.go`, `system.go`, `jobs.go`, `ws.go`, `api_test.go`
- Create: `backend/internal/agent/server.go`, `sysinfo.go`, `sysinfo_test.go`
- Create: `backend/cmd/betternas-api/main.go`, `backend/cmd/betternas-agent/main.go`

**Interfaces:**
- Consumes: alles aus Task 2–6.
- Produces:
  ```go
  // agent
  type Deps struct{ Runner sysexec.Runner; FS sysexec.FS; AllowLoop bool }
  func Register(s *rpc.Server, d Deps)        // registriert alle Agent-Methoden; Mutationen (disk.format, storage.apply) über einen gemeinsamen Mutex serialisiert
  // Methode "system.info" → {"hostname","kernel","os","uptimeSeconds","cpuCount","memTotalBytes","memAvailableBytes"}  (aus /proc/uptime, /proc/meminfo, /etc/os-release, uname)
  // api
  type Config struct{ Listen, DBPath, AgentSocket, TLSDir, WebDir string; Dev bool }
  type Deps struct{ DB *sql.DB; Agent *rpc.Client; Jobs *jobs.Manager; Hub *events.Hub; Auth *auth.Handlers; Storage *storage.Manager /* nil bis Task 14 */ }
  func NewServer(cfg Config, deps Deps) http.Handler
  func EnsureSelfSignedCert(dir, hostname string) (certFile, keyFile string, err error) // ECDSA P-256, 10 Jahre, SAN: hostname, hostname.local, localhost
  ```
  Routen: `POST /api/v1/auth/login|logout`, `GET /api/v1/auth/me`, `GET /api/v1/system/info` (Agent-Result + `"version"`), `GET /api/v1/jobs?limit=50`, `GET /api/v1/jobs/{id}`, `GET /api/v1/ws` (sendet Hub-Nachrichten als JSON-Text-Frames; Session-Pflicht, Origin muss dem Host entsprechen), alles andere → statische Dateien aus `WebDir` mit SPA-Fallback auf `index.html`. Alle Routen hinter `LANOnly`, `/api/v1/*` außer `login` hinter `RequireSession`.
  Flags API: `--listen :443 --db /var/lib/betternas/betternas.db --agent-socket /run/betternas/agent.sock --tls-dir /etc/betternas/tls --web-dir /usr/share/betternas/web --dev` (`--dev`: HTTP auf `:8080`, Cookie nicht `Secure`). Beim Start `jobs.MarkInterrupted`.
  Flags Agent: `--socket /run/betternas/agent.sock --socket-group betternas --api-user betternas --allow-loop`.

- [ ] **Step 1: Failing tests:** `TestSysinfoParsesProc` (RootFS mit Fixture-`/proc/meminfo`, `/proc/uptime`, `/etc/os-release` → `memTotalBytes == 16303472*1024`, `os == "Debian GNU/Linux 13 (trixie)"`); `TestUnauthenticatedAPIIs401`; `TestSPAFallback` (`GET /speicher/pools` → `index.html`, `GET /api/v1/unknown` → 404 JSON); `TestSystemInfoIncludesVersion` (Agent-Fake über `rpc`-Testserver); `TestEnsureSelfSignedCertIdempotent` (zweiter Aufruf erzeugt keine neue Datei, mtime gleich).
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implementieren. **Step 4:** Run → PASS.
- [ ] **Step 5:** Run `make build build-arm64` → zwei Binaries je Architektur, `file bin/arm64/betternas-api` zeigt `ARM aarch64, statically linked`.
- [ ] **Step 6:** Commit `feat(api): server wiring, TLS, system info, jobs and websocket`.

---

### Task 8: Frontend-Grundgerüst

**Files:**
- Create: `frontend/` (Vite Vue-TS-Template), `src/main.ts`, `src/router.ts`, `src/api/client.ts`, `src/api/ws.ts`, `src/stores/auth.ts`, `src/stores/jobs.ts`, `src/utils/format.ts`, `src/components/AppLayout.vue`, `src/components/JobsPanel.vue`, `src/views/LoginView.vue`, `src/views/DashboardView.vue`, Tests unter `src/**/__tests__/`
- Modify: `vite.config.ts` (Proxy `/api` → `process.env.BNAS_API ?? "http://betternas.local:8080"`, `ws: true`)

**Interfaces:**
- Consumes: HTTP-Routen aus Task 7.
- Produces:
  ```ts
  class ApiError extends Error { code: string; status: number; details?: Record<string, unknown> }
  function apiFetch<T>(path: string, init?: RequestInit): Promise<T>  // credentials 'same-origin'; X-CSRF-Token bei Nicht-GET; 401 → Router zu /login; Fehler-JSON → ApiError
  function connectEvents(onMessage: (m: { type: string; data?: unknown }) => void): () => void  // Reconnect mit Backoff 1s→30s
  function formatBytes(n: number): string   // dezimal, de-DE
  useAuthStore(): { user, csrfToken, login(u,p), logout(), fetchMe() }
  useJobsStore(): { jobs, track(jobId): Promise<Job> /* resolved bei succeeded, rejected bei failed */ }
  ```
  Routen: `/login`, `/` (Dashboard), `/speicher/datentraeger`, `/speicher/pools` (Platzhalter bis Task 15). Navigation: Übersicht, Speicher → Datenträger, Pools. PrimeVue Theme Aura, Dark Mode nach System.

- [ ] **Step 1: Failing tests (Vitest):**
  - `client.test.ts`: POST setzt `X-CSRF-Token`, GET nicht; Antwort 409 mit `{"error":{"code":"pool_busy","message":"Pool wird verwendet"}}` → `ApiError` mit `code === "pool_busy"`, `status === 409`, `message === "Pool wird verwendet"`.
  - `format.test.ts`: `formatBytes(4000787030016) === "4,0 TB"`, `formatBytes(512110190592) === "512,1 GB"`, `formatBytes(0) === "0 B"`.
  - `LoginView.test.ts`: Fehler 401 zeigt die Server-Message; Button während Request deaktiviert.
- [ ] **Step 2:** Run `npm --prefix frontend test` → FAIL.
- [ ] **Step 3:** Implementieren; Dashboard zeigt Hostname, OS, Kernel, Uptime, RAM (Balken), Version.
- [ ] **Step 4:** Run → PASS; `npm --prefix frontend run build` → `frontend/dist/index.html` existiert.
- [ ] **Step 5:** Commit `feat(web): Vue skeleton with login, layout, dashboard and jobs panel`.

---

### Task 9: Debian-Paket und systemd-Units

**Files:**
- Create: `nfpm.yaml`, `packaging/systemd/betternas-api.service`, `packaging/systemd/betternas-agent.service`, `packaging/scripts/postinst.sh`, `packaging/scripts/prerm.sh`
- Modify: `Makefile` (Target `deb ARCH=amd64|arm64`), `.github/workflows/ci.yml` (Job `package` aktiv)

**Interfaces:**
- Produces: Paket `betternas_<version>_<arch>.deb` mit `/usr/bin/betternas-api`, `/usr/sbin/betternas-agent`, `/usr/share/betternas/web/**`, `/lib/systemd/system/betternas-{api,agent}.service`.
  `Depends: mergerfs, smartmontools, gdisk, parted, e2fsprogs, xfsprogs, util-linux, udev, libpam-modules-bin, systemd`.
  `betternas-agent.service`: `Type=simple`, `RuntimeDirectory=betternas`, `RuntimeDirectoryMode=0755`, `Restart=on-failure`.
  `betternas-api.service`: `User=betternas`, `Group=betternas`, `StateDirectory=betternas`, `AmbientCapabilities=CAP_NET_BIND_SERVICE`, `Requires=betternas-agent.service`, `After=betternas-agent.service network-online.target`, `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ReadWritePaths=/var/lib/betternas /etc/betternas/tls`.
  postinst: System-User/-Gruppe `betternas`, Gruppe `betternas-admin`, Verzeichnisse `/etc/betternas/tls` (betternas:betternas 0750), `/srv/disks`, `/srv/pools`, `/srv/appdata`, `/srv/vms` (root 0755), `systemctl daemon-reload`, `enable --now` beider Units, Hinweis `sudo adduser <name> betternas-admin`. Idempotent bei Upgrade.

- [ ] **Step 1:** Units, Skripte, `nfpm.yaml` schreiben.
- [ ] **Step 2:** Run `make web build deb ARCH=amd64` → `dist/betternas_dev_amd64.deb`; `dpkg-deb -c` (bzw. `ar`/`tar` auf macOS) listet die genannten Pfade.
- [ ] **Step 3:** Run `shellcheck packaging/scripts/*.sh` → keine Befunde.
- [ ] **Step 4: Ins Image integrieren:** `image/build-rootfs.sh` übergibt `dist/betternas_<version>_<arch>.deb` als zusätzliches `--include=<pfad>` an mmdebstrap. postinst muss im chroot laufen können: `systemctl enable` über `deb-systemd-helper enable`, `start` nur wenn `[ -d /run/systemd/system ]`. `image/tests/check-rootfs.sh` prüft zusätzlich `usr/bin/betternas-api`, `etc/systemd/system/multi-user.target.wants/betternas-api.service` und dass die Gruppe `betternas-admin` existiert.
- [ ] **Step 5:** Run `make web build deb rootfs iso && make e2e MODE=uefi` → PASS; `test/image/e2e.sh` prüft zusätzlich `curl -k https://localhost:8443/` → HTTP 200 (Port-Forward `hostfwd=tcp::8443-:443`) und `POST /api/v1/auth/login` mit dem im Installer angelegten Admin `ittest` → 200.
- [ ] **Step 6 (manuell):** Installierte VM im Browser öffnen → Zertifikatswarnung, Login mit dem Installer-Admin, Dashboard zeigt Daten. `ls -l /run/betternas/agent.sock` → `srw-rw---- root betternas`. `make deploy VM=<host>` (Paket-Update auf installiertem System) läuft fehlerfrei.
- [ ] **Step 7:** Commit `build: debian package, systemd units and image integration`.

---

### Task 10: Datenträger-Erkennung (lsblk, Disk-IDs, Systemplatte)

**Files:**
- Create: `backend/internal/storage/lsblk.go`, `diskid.go`, `lsblk_test.go`, `diskid_test.go`, `testdata/lsblk-*.json`

**Interfaces:**
- Produces:
  ```go
  type Partition struct{ Device, FSType, UUID, Label string; SizeBytes uint64; Mountpoints []string }
  type Disk struct{
      ID, Device, Model, Serial, WWN, Transport string; SizeBytes uint64; Rotational bool
      System, Manageable bool; Reason string; Partitions []Partition }
  type IDOptions struct{ AllowLoop bool }
  func ParseLsblk(data []byte, opts IDOptions) ([]Disk, error)
  func DiskID(model, serial, wwn, kname string, opts IDOptions) (id string, ok bool)
  func ShortID(diskID string) string   // erste 7 Hex-Zeichen von sha256(diskID)
  func FSLabel(diskID string) string   // "bnas-" + ShortID → 12 Zeichen (passt für ext4 und XFS)
  const LsblkColumns = "NAME,KNAME,PATH,TYPE,SIZE,MODEL,SERIAL,WWN,TRAN,ROTA,FSTYPE,UUID,LABEL,MOUNTPOINTS"
  ```
  Aufruf (im Agent, Task 12): `lsblk --json --bytes --tree -o <LsblkColumns>`.
  Regeln: nur `type == "disk"` (bzw. `loop` bei `AllowLoop`); `zram*`, `ram*`, `sr*`, Größe 0 ignorieren. ID: WWN vorhanden → `wwn-<wwn>`; sonst Model + Serial → `<model>_<serial>`; sonst Loop (`AllowLoop`) → `loop-<kname>`; sonst nicht verwaltbar, Reason `"Keine eindeutige Kennung"`. Sanitizing: jedes Zeichen außerhalb `[A-Za-z0-9._-]` → `_`; führende `.`/`-` → `_`. Doppelte IDs → beide `Manageable=false`, Reason `"Doppelte Kennung (z. B. USB-Gehäuse)"`. `System=true`, wenn irgendeine Partition/Nachfahre auf `/`, `/boot`, `/boot/efi` oder `[SWAP]` gemountet ist; Systemplatten sind nie `Manageable`.

- [ ] **Step 1: Failing tests:**
  - `TestParseBasic` (Fixture: NVMe-System mit `/`, `/boot/efi`, `[SWAP]`; zwei SATA-HDDs, eine mit WWN `0x50014ee2b5d1a2c3`, eine ohne WWN mit Model `WDC WD40EFRX-68N32N0`, Serial `WD-WCC7K1234567`; ein `sr0`): 3 Disks; NVMe `System && !Manageable`; IDs `wwn-0x50014ee2b5d1a2c3` und `WDC_WD40EFRX-68N32N0_WD-WCC7K1234567`; `Rotational == true`; Partition der zweiten HDD mit `FSType "ext4"`.
  - `TestDuplicateSerials` (Fixture: zwei USB-Disks gleiches Model/Serial `0123456789AB`, kein WWN): beide `!Manageable`, Reason wie oben.
  - `TestNoIdentity`: Disk ohne Serial und WWN → `!Manageable`, Reason `"Keine eindeutige Kennung"`.
  - `TestLoopOnlyWhenAllowed`: Fixture mit `loop0` → ohne Option 0 Disks; mit `AllowLoop` ID `loop-loop0`.
  - `TestDiskIDSanitizing`: Serial `"A:B,C/../D E"` → ID enthält weder `:` noch `,` noch `/` noch Leerzeichen; `"..x"` beginnt nicht mit `.`.
  - `TestFSLabelLength`: `len(FSLabel(id)) == 12` und beginnt mit `bnas-`.
- [ ] **Step 2:** Run `go test ./internal/storage/` → FAIL. **Step 3:** Implementieren. **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `feat(storage): disk discovery from lsblk with stable IDs`.

---

### Task 11: Soll-Zustand und systemd-Unit-Renderer

**Files:**
- Create: `backend/internal/storage/state.go`, `escape.go`, `render.go`, `escape_test.go`, `render_test.go`, `testdata/golden/*.mount`

**Interfaces:**
- Produces:
  ```go
  type DiskMount struct{ DiskID, FSUUID, FSType string }            // Where = DiskPath(DiskID)
  type Pool struct{ Name string; Branches []string; CreatePolicy, MinFree string } // Branches = Disk-IDs in Reihenfolge
  type State struct{ Disks []DiskMount; Pools []Pool }
  func DiskPath(id string) string   // "/srv/disks/" + id
  func PoolPath(name string) string // "/srv/pools/" + name
  func EscapePath(p string) string  // wie `systemd-escape --path`
  func UnitName(path string) string // EscapePath(path) + ".mount"
  func (s State) Validate() error   // jede Branch referenziert eine DiskMount; Disk max. in einem Pool; Policy/MinFree gültig; Pool-Name gültig
  func RenderUnits(s State) (map[string][]byte, error)  // Unit-Dateiname → Inhalt
  const ManagedHeader = "# Verwaltet von BetterNAS – nicht manuell bearbeiten"
  var PoolNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`); var MinFreeRe = regexp.MustCompile(`^[0-9]{1,6}[KMG]$`)
  ```
  Disk-Unit:
  ```
  # Verwaltet von BetterNAS – nicht manuell bearbeiten
  [Unit]
  Description=BetterNAS Datenplatte <id>

  [Mount]
  What=/dev/disk/by-uuid/<fsuuid>
  Where=/srv/disks/<id>
  Type=<fstype>
  Options=defaults,noatime,nofail

  [Install]
  WantedBy=multi-user.target
  ```
  Pool-Unit:
  ```
  # Verwaltet von BetterNAS – nicht manuell bearbeiten
  [Unit]
  Description=BetterNAS Pool <name>
  Wants=<branch-units, leerzeichengetrennt>
  After=<branch-units>

  [Mount]
  What=/srv/disks/<id1>:/srv/disks/<id2>
  Where=/srv/pools/<name>
  Type=fuse.mergerfs
  Options=category.create=<policy>,minfreespace=<minfree>,moveonenospc=true,cache.files=off,dropcacheonclose=true,fsname=bnas-<name>,nofail

  [Install]
  WantedBy=multi-user.target
  ```

- [ ] **Step 1: Failing tests:**
  - `TestEscapePath`: `/srv/disks/WDC_WD40EFRX-68N32N0_WD-WCC7K1234567` → `srv-disks-WDC_WD40EFRX\x2d68N32N0_WD\x2dWCC7K1234567`; `/srv/pools/media` → `srv-pools-media`; `/srv/disks/.x` → `srv-disks-\x2ex`.
  - `TestRenderGolden`: State mit 2 Disks + Pool `media` (`epmfs`, `20G`) → genau 3 Dateien, Inhalte byte-gleich mit `testdata/golden/*` (Update-Flag `-update`).
  - `TestPoolUsesWantsNotRequires`: Pool-Unit enthält `Wants=` und kein `Requires=` (Review Focus 2).
  - `TestValidateRejects`: Branch ohne DiskMount; Disk in zwei Pools; Policy `"rand"`; MinFree `"20G,allow_other"`; Pool-Name `"../etc"`, `"Media"`, `"a:b"` → jeweils Fehler `invalid_params`.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implementieren. **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `feat(storage): desired state and systemd mount unit renderer`.

---

### Task 12: Agent-Storage-Operationen

**Files:**
- Create: `backend/internal/agent/disks.go`, `smart.go`, `format.go`, `apply.go`, `disks_test.go`, `smart_test.go`, `format_test.go`, `apply_test.go`

**Interfaces:**
- Consumes: `storage.ParseLsblk`, `storage.RenderUnits`, `storage.UnitName`, `storage.FSLabel`, `sysexec`, `apierr`.
- Produces (Agent-Methoden):
  - `disk.list` → `[]storage.Disk`
  - `disk.smart` `{"device"}` → `{"passed":bool,"temperatureC":int,"powerOnHours":int}`; `smartctl --json -H -A <dev>`; nur Exit-Bits 0–1 sind fatal; `device` muss ein `type=disk`-Gerät aus `disk.list` sein.
  - `disk.format` `{"diskId","device","sizeBytes","fsType"}` → `{"partition","fsUuid","fsType","label"}`
  - `storage.apply` `storage.State` → `{"changed":[unit…]}`
  ```go
  func Format(ctx context.Context, d Deps, req FormatRequest) (FormatResult, error)
  func Apply(ctx context.Context, d Deps, st storage.State) (ApplyResult, error)
  ```
  **Format-Ablauf:** `disk.list` erneut ausführen; Disk mit `diskId` muss existieren, `Manageable`, `Device` und `SizeBytes` gleich der Anfrage, keine Partition gemountet – sonst `conflict` (Systemplatte → `system_disk`). Dann exakt: `wipefs --all <dev>` · `sgdisk --zap-all <dev>` · `sgdisk --new=1:0:0 --typecode=1:8300 --change-name=1:bnas-data <dev>` · `partprobe <dev>` · `udevadm settle` · `lsblk --json --bytes -o PATH,TYPE <dev>` (erste `part`) · `mkfs.ext4 -F -m 0 -L <label> <part>` bzw. `mkfs.xfs -f -L <label> <part>` · `blkid -s UUID -o value <part>`. Jeder ExitCode ≠ 0 → `internal` mit Stderr im Log, Abbruch.
  **Apply-Ablauf:** `st.Validate()`; rendern; vorhandene verwaltete Units = `Glob("/etc/systemd/system/srv-disks-*.mount")` ∪ `srv-pools-*.mount`, gefiltert auf `ManagedHeader` in Zeile 1; Backup der alten Inhalte. (1) Veraltete Units: erst Pools, dann Disks → `systemctl disable --now <unit>`; Datei löschen. (2) Neue/geänderte Dateien schreiben; `MkdirAll` für alle `Where`. (3) `systemctl daemon-reload`. (4) Geänderte Units (erst Disks, dann Pools): `systemctl stop <unit>`, dann `findmnt --mountpoint <where>`; ist noch gemountet (ExitCode 0) oder `stop` schlägt fehl → **Rollback** (alle Backups zurückschreiben, neu angelegte Dateien löschen, `daemon-reload`, gestoppte Units wieder `start`) und Fehler `pool_busy` (Pool) bzw. `disk_in_use` (Disk) mit `details.unit`. (5) Neue und geänderte Units: `systemctl enable --now <unit>`. Unveränderte Units werden nicht angefasst. Ein Disk-Unit wird erst entfernt, wenn kein Pool-Unit es mehr nutzt (folgt aus Reihenfolge).

- [ ] **Step 1: Failing tests** (FakeRunner + RootFS in TempDir; `disk.list` liefert Fixture aus Task 10):
  - `TestFormatCommandSequence`: Calls exakt in obiger Reihenfolge für `ext4`; Ergebnis `fsUuid` aus `blkid`-Fake.
  - `TestFormatRejectsChangedDisk`: `sizeBytes` weicht ab → `conflict`, kein `wipefs` in `Calls`; Partition inzwischen gemountet → `conflict`; Systemplatte → `system_disk` (Review Focus 4).
  - `TestFormatStopsOnError`: `sgdisk --zap-all` ExitCode 2 → Fehler, kein `mkfs.*` in `Calls`.
  - `TestApplyFreshState`: leeres `/etc/systemd/system` → 3 Dateien geschrieben, `daemon-reload` vor `enable --now`, Disk-Units vor Pool-Unit enabled, Verzeichnisse `/srv/disks/<id>` und `/srv/pools/media` existieren.
  - `TestApplyIsIdempotent`: zweites `Apply` mit gleichem State → keinerlei `systemctl`-Aufrufe (auch kein `daemon-reload`), keine Datei neu geschrieben, `changed` leer.
  - `TestApplyRemovesStale`: Pool aus State entfernt → `systemctl disable --now srv-pools-media.mount`, Datei gelöscht; Disk-Units bleiben.
  - `TestApplyIgnoresForeignUnits`: `srv-disks-foo.mount` ohne Header → weder geändert noch gelöscht.
  - `TestApplyRollbackOnBusyPool`: Pool-Branches geändert, Fake `findmnt --mountpoint /srv/pools/media` ExitCode 0 → Fehler `pool_busy`, Unit-Datei wieder byte-gleich mit Original, Unit erneut gestartet (Review Focus 3).
  - `TestSmartParsesJSON`: Fixture-JSON mit ExitCode 4 (Bit 2) → `passed true`, `temperatureC 34`, `powerOnHours 12034`; ExitCode 2 → Fehler.
- [ ] **Step 2:** Run `go test ./internal/agent/` → FAIL. **Step 3:** Implementieren und in `agent.Register` eintragen. **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `feat(agent): disk list, SMART, format and reconciling storage apply`.

---

### Task 13: udev-Ereignisse bis in die UI

**Files:**
- Create: `backend/internal/agent/udev.go`, `udev_test.go`
- Modify: `backend/internal/api/server.go` (Subscriber starten)

**Interfaces:**
- Consumes: `rpc.Server.HandleStream`, `rpc.Client.Subscribe`, `events.Hub`.
- Produces: Agent-Stream `events.subscribe` → Events `{"event":"disk.changed"}`. Quelle: `type UdevSource interface{ Events(ctx) (<-chan struct{}, error) }`, Implementierung mit `go-udev` (Netlink, Filter `SUBSYSTEM=block`, `add`/`remove`/`change`). Entprellung 1 s. API: Goroutine abonniert mit Reconnect (Backoff 1 s → 30 s) und publiziert `events.Message{Type:"disks.changed"}`.

- [ ] **Step 1: Failing test** `TestUdevDebounce`: Fake-Quelle sendet 5 Ereignisse in 100 ms → genau 1 Event `disk.changed` innerhalb 1,5 s; danach weiteres Ereignis → zweites Event.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implementieren. **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `feat(agent): udev block events forwarded to web clients`.

---

### Task 14: API-Storage-Manager und Endpunkte

**Files:**
- Create: `backend/internal/storage/manager.go`, `manager_test.go`, `backend/internal/api/storage.go`, `storage_api_test.go`
- Modify: `backend/internal/api/server.go` (Routen), `backend/cmd/betternas-api/main.go` (Manager erzeugen)

**Interfaces:**
- Consumes: `rpc.Client.Call` (über Interface), `jobs.Manager.Start`, `events.Hub`, `audit.Record`, `storage.State`, Agent-Methoden aus Task 12.
- Produces:
  ```go
  type Agent interface{ Call(ctx context.Context, method string, params, result any) error }
  type DependentsFunc func(ctx context.Context, pool string) ([]string, error)  // ab Phase 2: Shares/Stacks
  func NewManager(db *sql.DB, agent Agent, jobs *jobs.Manager, hub *events.Hub, deps ...DependentsFunc) *Manager
  type SmartInfo struct{ Passed bool; TemperatureC, PowerOnHours int }
  type DiskView struct{ storage.Disk; Role string /* unassigned|data|parity|system */; State string /* online|missing */; Pool string; FSType string; Smart *SmartInfo }
  // Smart: per Agent `disk.smart`, je Disk 5 min im Speicher gecacht; Fehler → nil (UI zeigt „unbekannt“), blockiert die Liste nicht
  type FormatPreview struct{ DiskID, Device string; SizeBytes uint64; Partitions []storage.Partition; ConfirmToken string; ExpiresAt time.Time }
  type FormatRequest struct{ FSType, ConfirmSerial, ConfirmToken string }
  type PoolInput struct{ Name string; Branches []string; CreatePolicy, MinFree string }  // leere Policy/MinFree → Defaults
  type PoolView struct{ PoolInput; Path string; SizeBytes, FreeBytes uint64; Mounted bool }
  func (m *Manager) ListDisks(ctx) ([]DiskView, error)
  func (m *Manager) PreviewFormat(ctx, diskID string) (FormatPreview, error)
  func (m *Manager) Format(ctx, actor, diskID string, req FormatRequest) (jobID string, err error)
  func (m *Manager) Import(ctx, actor, diskID string) (string, error)
  func (m *Manager) Release(ctx, actor, diskID string) (string, error)
  func (m *Manager) ListPools(ctx) ([]PoolView, error)
  func (m *Manager) CreatePool(ctx, actor string, in PoolInput) (string, error)
  func (m *Manager) UpdatePool(ctx, actor, name string, in PoolInput) (string, error)   // Name unveränderlich
  func (m *Manager) DeletePool(ctx, actor, name string) (string, error)
  ```
  **Regeln:** Alle Mutationen über einen Manager-Mutex serialisiert. Validierung synchron (Fehler sofort als HTTP-Antwort), Ausführung als Job. Muster jeder Mutation: neuen State im Speicher berechnen → `storage.apply` → erst bei Erfolg DB-Transaktion schreiben → Audit → `hub.Publish("disks.changed"|"pools.changed")`. Bei Fehler bleibt die DB unverändert.
  - `ConfirmToken`: 32 Byte zufällig, im Speicher gebunden an `diskID+device+sizeBytes`, gültig 5 min, einmalig verwendbar.
  - `Format`: Token gültig, `ConfirmSerial` == Serial der Disk (ohne Serial: == Disk-ID), `FSType ∈ {ext4,xfs}`, Rolle `unassigned` → Agent `disk.format` → Disk mit Rolle `data` + Apply.
  - `Import`: Rolle `unassigned`, genau die erste Partition mit `ext4|xfs|btrfs` und ohne Mountpoint → übernehmen; sonst `invalid_params` „Kein unterstütztes Dateisystem gefunden“.
  - `Release`: Disk in Pool → `disk_in_use` mit `details.pool`.
  - `DeletePool`: `DependentsFunc` liefert Einträge → `conflict` mit `details.dependents`.
  - `ListDisks`: Disks aus DB, die live fehlen → `State "missing"` (mit gespeicherten Model/Serial/Size). `ListPools`: Größe/Frei per `syscall.Statfs` auf `PoolPath` (API darf lesen), `Mounted` aus `/proc/self/mountinfo`.
  Routen (202 → `{"jobId":…}`): `GET /api/v1/disks`, `POST /api/v1/disks/{id}/format/preview` (200), `POST /api/v1/disks/{id}/format|import|release`, `GET|POST /api/v1/pools`, `GET|PATCH|DELETE /api/v1/pools/{name}`.

- [ ] **Step 1: Failing tests** (Fake-Agent, echte SQLite in TempDir):
  - `TestListDisksMergesRolesAndMissing`: DB-Disk nicht im Agent-Ergebnis → `missing` (Review Focus 2); Systemplatte → Rolle `system`.
  - `TestFormatRequiresValidTokenAndSerial`: falsche Serial → `invalid_params`; abgelaufener/benutzter Token → `conflict`; korrekt → Agent `disk.format` aufgerufen, danach `storage.apply` mit Disk im State, DB-Rolle `data`.
  - `TestFormatTokenBoundToDevice`: Preview, danach liefert Agent dieselbe ID mit anderem `device` → `conflict`, kein `disk.format` (Review Focus 4).
  - `TestCreatePoolValidation`: Name `"../x"`, `"a,b"`, Branch mit Rolle `unassigned`, Branch bereits in anderem Pool, leere Branches → 400 mit Code `invalid_params` bzw. `conflict`, kein Agent-Aufruf (Review Focus 5).
  - `TestCreatePoolDefaults`: leere Policy/MinFree → State enthält `epmfs` und `20G`.
  - `TestUpdatePoolBusyKeepsDB`: Fake-Agent gibt bei `storage.apply` `pool_busy` → Job `failed`, Log enthält „Pool wird verwendet“, DB-Branches unverändert (Review Focus 3).
  - `TestDeletePoolWithDependents`: DependentsFunc liefert `["share:filme"]` → `conflict`, `details.dependents`.
  - `TestReleaseDiskInPool`: → `disk_in_use`, `details.pool == "media"`.
  - `TestMutationsAudited`: erfolgreiche Pool-Erstellung → Audit `pool.create` / `ok`.
  - `TestStorageRoutes` (httptest): `POST /api/v1/pools` mit gültigem Body → 202 + `jobId`; ungültig → 400 im Fehlerformat.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implementieren, Routen in `api.NewServer` eintragen. **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `feat(storage): disk and pool management API`.

---

### Task 15: Storage-Oberfläche

**Files:**
- Create: `frontend/src/api/storage.ts`, `src/stores/storage.ts`, `src/views/storage/DisksView.vue`, `src/views/storage/PoolsView.vue`, `src/components/storage/FormatDialog.vue`, `src/components/storage/PoolDialog.vue`, `src/components/storage/ConfirmDeleteDialog.vue`, Tests
- Modify: `src/router.ts`

**Interfaces:**
- Consumes: Routen aus Task 14, `apiFetch`, `connectEvents`, `useJobsStore().track`.
- Produces: `useStorageStore(): { disks, pools, refresh(), previewFormat(id), format(id, req), importDisk(id), release(id), createPool(in), updatePool(name, in), deletePool(name) }` – Mutationen warten per `track(jobId)` und rufen danach `refresh()`; WS-Nachrichten `disks.changed`/`pools.changed` lösen `refresh()` aus. `const POOL_NAME_RE = /^[a-z][a-z0-9-]{0,31}$/` (identisch zur Backend-Regex).
  **DisksView:** Tabelle (Gerät, Modell, Seriennummer, Größe, Typ HDD/SSD, Rolle, Pool, Zustand, SMART/Temperatur); Systemplatte mit Schloss-Symbol und ohne Aktionen; nicht verwaltbare Platten mit Begründung als Tooltip; Aktionen je Rolle: `unassigned` → Formatieren / Importieren, `data` ohne Pool → Freigeben.
  **FormatDialog:** zeigt Vorschau (vorhandene Partitionen und Dateisysteme in Rot: „Diese Daten werden unwiderruflich gelöscht“), Dateisystem-Auswahl (ext4 vorausgewählt), Eingabefeld „Seriennummer zur Bestätigung eingeben“; Button „Formatieren“ erst aktiv, wenn Eingabe exakt der Serial entspricht.
  **PoolsView/PoolDialog:** Karten pro Pool (Name, Pfad, belegt/frei, Platten, Policy); Dialog mit Name (nur beim Anlegen editierbar, Live-Validierung), Mehrfachauswahl der Platten (nur Rolle `data`, nicht in anderem Pool, plus die eigenen beim Bearbeiten), Policy-Auswahl mit Kurzbeschreibung, MinFree. Hinweis im Dialog: „mergerfs bietet keine Ausfallsicherheit – SnapRAID folgt in einer späteren Version.“ Löschen über `ConfirmDeleteDialog` (Poolnamen eintippen; Text: „Die Daten auf den Platten bleiben erhalten.“). Fehler `pool_busy` → Meldung „Der Pool wird gerade verwendet. Schließe offene Dateien und versuche es erneut.“

- [ ] **Step 1: Failing tests (Vitest):**
  - `FormatDialog.test.ts`: Button deaktiviert bei leerer Eingabe und bei `"wd-wcc7k1234567"` (Groß-/Kleinschreibung zählt), aktiv bei `"WD-WCC7K1234567"`; Submit sendet `{fsType:"ext4", confirmSerial, confirmToken}`.
  - `PoolDialog.test.ts`: Auswahlliste enthält keine Platte mit Rolle `unassigned`/`system` und keine Platte aus anderem Pool; Name `"Media"` zeigt Validierungsfehler; Speichern ohne Platte deaktiviert.
  - `DisksView.test.ts`: Systemplatte rendert keine Aktions-Buttons; `missing`-Platte zeigt Zustand „Fehlt“.
  - `storage.store.test.ts`: WS-Nachricht `pools.changed` → `refresh` aufgerufen.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implementieren. **Step 4:** Run → PASS, `npm run build` erfolgreich.
- [ ] **Step 5:** Commit `feat(web): disks and pools management UI`.

---

### Task 16: Integrationstests in der Debian-13-VM

**Files:**
- Create: `test/integration/README.md`, `test/integration/storage_test.go` (`//go:build integration`)
- Modify: `Makefile` (Target `integration VM=<host>`)

**Interfaces:**
- Consumes: `agent.Format`, `agent.Apply`, `agent` `disk.list` mit `Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/"), AllowLoop: true}`.
- Produces: `make integration VM=<host>` = `GOOS=linux CGO_ENABLED=0 go test -c -tags integration -o bin/integration.test ./test/integration` → `scp` → `ssh <host> sudo /tmp/integration.test -test.v`.
  README: Test-VM aus dem BetterNAS-ISO installieren (UTM oder QEMU, 2 vCPU, 4 GB RAM; alle benötigten Pakete sind im Image), SSH-Key für den Admin hinterlegen, `sudo` ohne Passwort für den Testbenutzer. Zusätzliche virtuelle Datenplatten mit Seriennummer anlegen (QEMU: `-drive …,serial=BNAS0001`), sonst gelten sie als „Keine eindeutige Kennung“.

- [ ] **Step 1: Test schreiben** `TestStorageLifecycle` (Setup: zwei 1-GiB-Dateien, `losetup -P --find --show`; Cleanup: Pool und Units per `Apply(State{})` entfernen, `losetup -d`):
  1. `disk.list` enthält beide Loop-Disks als `Manageable`.
  2. Beide formatieren (ext4 und xfs) → `blkid` zeigt Label `bnas-…`.
  3. `Apply` mit beiden Disks + Pool `ittest` → `findmnt -t fuse.mergerfs /srv/pools/ittest` ExitCode 0; Datei über Pool schreiben → liegt unter genau einer `/srv/disks/<id>/`.
  4. Pool auf eine Disk reduzieren → `findmnt -no SOURCE` zeigt nur noch einen Branch.
  5. Datei auf dem Pool offen halten (`os.Open`) und Pool-Policy ändern → Fehler `pool_busy`, Unit-Datei unverändert, Pool weiterhin gemountet; Datei schließen → erneutes `Apply` erfolgreich.
  6. `systemctl daemon-reload && systemctl restart srv-pools-ittest.mount` → weiterhin gemountet (Units überstehen Reload; Reboot-Test manuell, siehe README).
  7. `Apply(State{})` → keine `srv-*-*.mount` mit BetterNAS-Header mehr in `/etc/systemd/system`, nichts mehr unter `/srv/pools/ittest` gemountet.
- [ ] **Step 2:** Run `make integration VM=<host>` → zunächst auftretende Fehler beheben (die Fix-Commits gehören zur jeweiligen Komponente).
- [ ] **Step 3:** Run → PASS.
- [ ] **Step 4 (manuell, im README dokumentiert):** Paket installieren, in der GUI zwei virtuelle Platten formatieren, Pool anlegen, VM neu starten → Pool nach dem Boot gemountet; eine virtuelle Platte in UTM entfernen, neu starten → Pool mit verbleibender Platte gemountet, GUI zeigt die fehlende Platte als „Fehlt“.
- [ ] **Step 5:** Commit `test: storage integration tests on Debian 13 with loop devices`.

---

## Bewusst nicht in diesem Plan (folgen in späteren Phasen)

SnapRAID (Phase 3, Tabellen und Rolle `parity` sind vorbereitet), Shares und Benutzerverwaltung (Phase 2, registrieren sich über `DependentsFunc`), Playwright-E2E (mit Phase 2, sobald ein Ende-zu-Ende-Nutzerfluss existiert), Spindown, SMART-Benachrichtigungen, LUKS.
