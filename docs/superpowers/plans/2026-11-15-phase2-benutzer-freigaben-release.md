# Phase 2 – Benutzer, Freigaben, Einrichtungsassistent, APT-Repo (MVP-Release) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** BetterNAS verwaltet Benutzer, Gruppen und Samba-Freigaben über die GUI, führt nach dem ersten Login durch die Einrichtung, bezieht Updates aus einem eigenen signierten APT-Repository und wird als ISO 0.1.0 veröffentlicht.

**Architecture:** Benutzer und Gruppen leben im System (`/etc/passwd`, `/etc/group`); der Agent führt feste Operationen (`useradd`, `usermod`, `gpasswd`, `smbpasswd` …) aus, die API validiert und auditiert. Freigaben liegen in SQLite, ein Renderer erzeugt `/etc/samba/betternas.d/*.conf`, der Agent prüft mit `testparm` und lädt Samba neu. Updates laufen über `apt` in einer eigenen transienten systemd-Unit, damit ein Update von BetterNAS selbst den Lauf nicht abbricht.

**Tech Stack:** Go 1.24 (ohne cgo), SQLite (modernc), Samba 4 / wsdd2 / avahi, apt-ftparchive + GnuPG, Vue 3.5 + PrimeVue 4 + Pinia, Vitest.

**Spec:** `docs/specs/2026-11-15-betternas-design.md` (Abschnitte 4.0, 4.2, 4.3, 4.6, 5, 6, 8, 10)

## Global Constraints

- NAS-Benutzer: UID 10000–59999, Shell `/usr/sbin/nologin`, kein Home (`-M -d /nonexistent`), primäre Gruppe `users`.
- BetterNAS-Gruppen: GID 10000–59999. Rolle Admin = Mitglied in `betternas-admin`.
- Benutzername und Gruppenname: `^[a-z_][a-z0-9_-]{0,31}$`; reservierte Namen aus `internal/installer/validate.go` (wandern nach `internal/users`).
- Passwort: mindestens 8 Zeichen, ungleich Benutzername, ohne `\n`, `\r`, `\0`.
- Samba-Passwort wird bei jeder Passwortänderung mitgesetzt (`smbpasswd -a -s`).
- Rechte pro Freigabe und Benutzer/Gruppe: `none` · `ro` · `rw`.
- Generierte Dateien beginnen mit `# Verwaltet von BetterNAS – nicht manuell bearbeiten` (`storage.ManagedHeader`). Fremde Konfiguration wird nie überschrieben.
- Samba: `/etc/samba/betternas.d/<share>.conf`, Include-Liste `/etc/samba/betternas.d/shares.conf`; Prüfung `testparm -s`, dann `smbcontrol smbd reload-config`.
- Fehlerformat `{"error":{"code","message","details"}}`; vorhandene Codes aus `internal/apierr`.
- API läuft nie als root; der Agent validiert jede Operation erneut.
- APT-Repo-URL ist Build-Parameter `APT_URL` (leer = keine BetterNAS-Quelle). Der private Signierschlüssel liegt außerhalb des Repos (`~/.betternas/apt-gnupg`); ins Repo kommt nur der öffentliche Schlüssel.
- Alle sichtbaren Texte auf Deutsch.

## Review Focus

1. **Name kollidiert mit vorhandenem Systemkonto oder -gruppe** (z. B. `docker`, `users`, `libvirt`): Der Agent lehnt ab (`conflict`) statt ein Konto umzubauen. Test in Task 2 (`TestCreateUserRejectsExistingSystemName`).
2. **Freigabe auf einem Pool, der gerade nicht eingehängt ist:** Kein Ordner landet auf der System-SSD; die Anwendung bricht mit klarer Meldung ab, und leere Pool-Einhängeordner sind unveränderlich. Tests in Task 6 (`TestApplySharesRefusesUnmountedPool`, `TestApplyProtectsPoolMountpoint`).
3. **`testparm` lehnt die erzeugte Konfiguration ab:** Die vorherigen Dateien werden wiederhergestellt, Samba wird nicht neu geladen. Test in Task 6 (`TestApplySharesRollsBackOnTestparmFailure`).
4. **Update des BetterNAS-Pakets aus der GUI startet API und Agent neu:** Der apt-Lauf läuft in eigener Unit weiter; die Seite zeigt danach das Ergebnis. Tests in Task 10 (`TestInstallUpdatesUsesTransientUnit`, `UpdatesView` übersteht Verbindungsabbruch).
5. **Letzten Admin entfernen oder sich selbst löschen:** Wird mit `conflict` abgelehnt, sonst sperrt sich der Nutzer aus der GUI aus. Test in Task 3 (`TestCannotRemoveLastAdminOrSelf`).

---

### Task 1: Paket `users` – Validierung und Parser

**Files:**
- Create: `backend/internal/users/users.go`, `backend/internal/users/parse.go`
- Test: `backend/internal/users/users_test.go`
- Modify: `backend/internal/installer/validate.go` (Regex und Reserviert-Liste aus `users` beziehen)

**Interfaces:**
- Produces:
  - `const AdminGroup = "betternas-admin"`, `const MinID, MaxID = 10000, 59999`
  - `func ValidateName(name string) error` (apierr `invalid_params`, details `field`)
  - `func ValidatePassword(user, pw string) error`
  - `func ValidateFullName(s string) error` – max. 64 Zeichen, keine Steuerzeichen, kein `:` und kein `,`
  - `type User struct { Name, FullName string; UID int; Groups []string; Admin bool; System bool }` (`System` = UID < 10000, z. B. der Installer-Admin mit UID 1000)
  - `type Group struct { Name string; GID int; Members []string }`
  - `func Parse(passwd, group []byte) (users []User, groups []Group, all map[string]bool)` – Benutzer mit UID 1000–59999, Gruppen mit GID 10000–59999, `Groups` nur BetterNAS-Gruppen; `all` enthält **alle** Benutzer- und Gruppennamen des Systems (für Kollisionsprüfung)
  - `func IsReserved(name string) bool`

- [ ] **Step 1: Tests schreiben** – `TestValidateName` (gültig: `anna`, `_svc`, `a-b_1`; ungültig: `Anna`, `1a`, `a:b`, `a b`, 33 Zeichen, `root`, `betternas-admin`), `TestValidatePassword` (`kurz` → Fehler, `anna`/`anna` → Fehler, `"gutes\npasswort"` → Fehler, `geheim123` → ok), `TestParse` mit Fixture `testdata/passwd`, `testdata/group` (enthält root, nobody 65534, wattie 1000 mit `betternas-admin`, anna 10000, Gruppe familie 10000 mit `anna,wattie`, docker 990): Ergebnis zwei Benutzer (wattie `System:true Admin:true Groups:["familie"]`, anna `Admin:false`), eine Gruppe `familie` mit Members `[anna wattie]`, `all["docker"] == true`.
- [ ] **Step 2: Test laufen lassen** – `cd backend && go test ./internal/users` → FAIL (Paket fehlt).
- [ ] **Step 3: Implementieren**; installer nutzt `users.ValidateName`-Regeln (Regex und Liste nur noch an einer Stelle).
- [ ] **Step 4: Tests** – `go test ./internal/users ./internal/installer/...` → PASS.
- [ ] **Step 5: Commit** – `feat(users): validation and passwd/group parsing`

### Task 2: Agent – Konten-Operationen

**Files:**
- Create: `backend/internal/agent/accounts.go`
- Modify: `backend/internal/agent/server.go` (Methoden registrieren, unter `mutate`)
- Test: `backend/internal/agent/accounts_test.go`, `backend/internal/agent/accounts_integration_test.go` (Tag `integration`)
- Modify: `Makefile` (`integration` führt `-test.run 'TestStorageLifecycle|TestAccountsLifecycle'` aus)

**Interfaces:**
- Consumes: `users.*` (Task 1), `sysexec.Runner`/`FS`
- Produces RPC-Methoden (Parameter JSON, Ergebnis):
  - `account.list` → `{users: []users.User, groups: []users.Group}` (liest `/etc/passwd`, `/etc/group` über `FS`)
  - `user.create {name, fullName, password, groups[], admin}`
  - `user.update {name, fullName, groups[], admin}`
  - `user.password {name, password}`
  - `user.delete {name}`
  - `group.create {name}`, `group.delete {name}`, `group.members {name, members[]}`

Befehlsfolgen (exakt, für FakeRunner-Tests):
- create: `useradd -K UID_MIN=10000 -K UID_MAX=59999 -M -d /nonexistent -s /usr/sbin/nologin -N -g users -c <fullName> [-G <g1,g2[,betternas-admin]>] <name>` · `chpasswd` (stdin `name:pw\n`) · `smbpasswd -a -s <name>` (stdin `pw\npw\n`). Scheitert ein Schritt nach `useradd`: `userdel <name>` und Fehler zurück.
- update: `usermod -c <fullName> -G <behalten ∪ gewählt> <name>`; „behalten“ = aktuelle Gruppen außerhalb von BetterNAS-Gruppen und `betternas-admin` (z. B. `sudo` des Installer-Admins bleibt).
- password: `chpasswd` · `smbpasswd -a -s <name>`.
- delete: `smbpasswd -x <name>` (Exit ≠ 0 ignorieren: Benutzer war nicht in Samba) · `userdel <name>`.
- group.create: `groupadd -K GID_MIN=10000 -K GID_MAX=59999 <name>`; group.delete: `groupdel <name>`; group.members: `gpasswd -M <a,b> <name>` (leer: `gpasswd -M "" <name>`).

Prüfungen im Agent (zusätzlich zur API): Name/Passwort gültig; create lehnt Namen ab, die in `all` existieren (`conflict`); update/password/delete nur für UID 1000–59999 und nie `betternas`; Gruppenoperationen nur für GID 10000–59999; gewählte Gruppen müssen BetterNAS-Gruppen sein.

- [ ] **Step 1: Tests schreiben** – `TestCreateUserCommands` (exakte Befehle + Stdins), `TestCreateUserRollsBackOnSmbpasswdFailure` (`userdel anna` wird aufgerufen), `TestCreateUserRejectsExistingSystemName` (`docker` → `conflict`, kein `useradd`), `TestUpdateUserKeepsForeignGroups` (wattie mit `sudo,betternas-admin,familie` → `-G sudo,kinder` bei admin=false, groups=[kinder]), `TestDeleteRefusesSystemAccounts` (`root`, `betternas`, UID 65534 → `forbidden`), `TestGroupOpsOnlyManagedRange` (`docker` → `forbidden`), `TestPasswordNeverInArgs` (Passwort erscheint in keinem Argument, nur in Stdins).
- [ ] **Step 2: RED** – `go test ./internal/agent -run 'User|Group|Password'` → FAIL.
- [ ] **Step 3: Implementieren** und in `Register` eintragen (ändernde Methoden unter `mutate`).
- [ ] **Step 4: GREEN** – `go test ./internal/agent` → PASS.
- [ ] **Step 5: Integrationstest** `TestAccountsLifecycle`: Präfix `bnasit`, bricht ab, wenn `bnasit*` schon existiert; Gruppe `bnasit-g` anlegen, Benutzer `bnasit-u` mit Passwort, `VerifyPassword` → true, `pdbedit -L` enthält `bnasit-u`, `id -nG` enthält `users bnasit-g`; Passwort ändern → altes Passwort false, neues true; Mitglieder per `group.members` leeren; Benutzer und Gruppe löschen; Cleanup per `t.Cleanup`. Ausführen: `make integration` → PASS.
- [ ] **Step 6: Commit** – `feat(agent): user and group operations with samba password sync`

### Task 3: API – Benutzer und Gruppen

**Files:**
- Create: `backend/internal/users/manager.go`, `backend/internal/api/users.go`
- Modify: `backend/internal/api/server.go` (`Deps.Users`), `backend/cmd/betternas-api/main.go`
- Test: `backend/internal/users/manager_test.go`, `backend/internal/api/users_api_test.go`

**Interfaces:**
- Consumes: RPC-Methoden aus Task 2
- Produces:
  - `type UsageFunc func(ctx context.Context, kind, name string) ([]string, error)` (kind `user|group`; liefert z. B. `share:filme`; ab Task 7 aus Freigaben)
  - `func NewManager(d *sql.DB, agent Agent, hub *events.Hub, usage ...UsageFunc) *Manager`
  - `List(ctx) (Accounts, error)`; `CreateUser(ctx, actor string, in UserInput) error`; `UpdateUser(ctx, actor, name string, in UserUpdate) error`; `SetPassword(ctx, actor, name, pw string) error`; `DeleteUser(ctx, actor, name string) error`; `CreateGroup(ctx, actor string, in GroupInput) error`; `SetMembers(ctx, actor, name string, members []string) error`; `DeleteGroup(ctx, actor, name string) error`
  - Routen: `GET /api/v1/users`, `POST /api/v1/users` (201), `PATCH /api/v1/users/{name}`, `PUT /api/v1/users/{name}/password` (204), `DELETE /api/v1/users/{name}` (204), `GET /api/v1/groups`, `POST /api/v1/groups` (201, `{name, members[]}`), `PATCH /api/v1/groups/{name}` (`{members[]}`), `DELETE /api/v1/groups/{name}` (204)
  - Ereignis `users.changed`; Audit-Aktionen `user.create|update|password|delete`, `group.create|members|delete` (nie mit Passwort in Details)

Regeln: Synchron (schnelle Befehle, kein Job). Eigenes Konto löschen → `conflict`. Admin-Recht des letzten Admins entfernen oder ihn löschen → `conflict`. Benutzer oder Gruppe, die in Freigaben verwendet wird, löschen → `conflict` mit `details.usedBy`.

- [ ] **Step 1: Tests schreiben** – `TestCannotRemoveLastAdminOrSelf`, `TestDeleteBlockedWhenUsed` (UsageFunc liefert `share:filme` → `conflict`, details), `TestCreateUserValidatesBeforeAgent` (ungültiger Name → kein Agent-Aufruf), `TestAuditHasNoPassword`, API-Test: Routen liefern erwartete Statuscodes und `users.changed` wird veröffentlicht.
- [ ] **Step 2: RED** – `go test ./internal/users ./internal/api` → FAIL.
- [ ] **Step 3: Implementieren.**
- [ ] **Step 4: GREEN** – `go test ./...` → PASS.
- [ ] **Step 5: Commit** – `feat(api): users and groups endpoints`

### Task 4: Frontend – Benutzer und Gruppen

**Files:**
- Create: `frontend/src/api/users.ts`, `frontend/src/stores/users.ts`, `frontend/src/views/users/UsersView.vue`, `frontend/src/views/users/GroupsView.vue`, `frontend/src/components/users/UserDialog.vue`, `frontend/src/components/users/PasswordDialog.vue`, `frontend/src/components/users/GroupDialog.vue`
- Modify: `frontend/src/router.ts` (`/benutzer`, `/benutzer/gruppen`), `frontend/src/components/AppLayout.vue` (Navigationsgruppe „Benutzer“; `users.changed` an den Store)
- Test: `frontend/src/__tests__/users.spec.ts`

**Interfaces:**
- Consumes: Routen aus Task 3
- Produces: `usersApi` (`list`, `create`, `update`, `setPassword`, `remove`, `groups`, `createGroup`, `setMembers`, `removeGroup`), `useUsersStore()` mit `users`, `groups`, `refresh()`, `handleEvent()`

Verhalten: Tabelle mit Name, vollständigem Namen, Rolle (Tag „Admin“), Gruppen. Dialog mit Passwort und Wiederholung (client-seitig: ≥ 8 Zeichen, gleich). Löschen mit Bestätigung. Konflikte (letzter Admin, in Freigabe verwendet) als Toast mit Server-Meldung.

- [ ] **Step 1: Tests schreiben** – UserDialog: Submit deaktiviert bei Passwort < 8 oder ungleicher Wiederholung (`data-test="user-submit"`), emittiert `submit` mit `{name, fullName, password, groups, admin}`; Store: `users.changed` löst `refresh` aus.
- [ ] **Step 2: RED** – `cd frontend && npx vitest run src/__tests__/users.spec.ts` → FAIL.
- [ ] **Step 3: Implementieren.**
- [ ] **Step 4: GREEN** – `npm test && npx vue-tsc --noEmit` → PASS.
- [ ] **Step 5: Commit** – `feat(web): users and groups pages`

### Task 5: Paket `shares` – Datenmodell, Validierung, Renderer

**Files:**
- Create: `backend/internal/db/migrations/0002_shares.sql`, `backend/internal/shares/state.go`, `backend/internal/shares/render.go`
- Test: `backend/internal/shares/render_test.go` mit Golden-Files unter `testdata/golden/`

**Interfaces:**
- Produces:
  - `type ACL struct { Type string /*user|group*/; Name string; Access string /*none|ro|rw*/ }`
  - `type Share struct { Name, Pool, Subdir, Comment string; Browsable, Guest, Recycle, Fruit bool; ACL []ACL }`
  - `func (s Share) Path() string` → `storage.PoolPath(s.Pool) + "/" + s.Subdir`
  - `type State struct { Shares []Share }`, `func (s State) Validate() error`
  - `func Render(st State) (map[string][]byte, error)` – Dateiname → Inhalt, immer inklusive `shares.conf`
  - `const ConfDir = "/etc/samba/betternas.d"`

Regeln: Name `^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`, eindeutig ohne Groß-/Kleinschreibung, nicht `global|homes|printers|print$|ipc$`; Dateiname `strings.ToLower(name)+".conf"`. Subdir: 1–4 Segmente `^[A-Za-z0-9][A-Za-z0-9 _.-]{0,63}$` mit `/` getrennt. Comment max. 128 Zeichen, keine Steuerzeichen, kein `\`. Mindestens ein `ro`/`rw`-Eintrag oder `Guest`. Prinzipal-Namen nach `users.ValidateName`.

Migration:
```sql
CREATE TABLE shares(
  name TEXT PRIMARY KEY COLLATE NOCASE,
  pool TEXT NOT NULL REFERENCES pools(name),
  subdir TEXT NOT NULL,
  comment TEXT NOT NULL DEFAULT '',
  browsable INTEGER NOT NULL, guest INTEGER NOT NULL,
  recycle INTEGER NOT NULL, fruit INTEGER NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE share_acl(
  share_name TEXT NOT NULL REFERENCES shares(name) ON DELETE CASCADE ON UPDATE CASCADE,
  principal_type TEXT NOT NULL CHECK(principal_type IN ('user','group')),
  principal_name TEXT NOT NULL,
  access TEXT NOT NULL CHECK(access IN ('none','ro','rw')),
  PRIMARY KEY(share_name, principal_type, principal_name)
);
```

Renderer (Gruppen als `@name`, Reihenfolge der Listen = Eingabereihenfolge):
```
<ManagedHeader>
[Filme]
   path = /srv/pools/media/Filme
   comment = …            (nur wenn gesetzt)
   browseable = yes|no
   read only = yes
   guest ok = no|yes
   valid users = <ro+rw>  (nur ohne Gast)
   invalid users = <none> (nur mit Gast und none-Einträgen)
   read list = <ro>       (nur wenn vorhanden)
   write list = <rw>      (nur wenn vorhanden)
   force group = users
   create mask = 0664
   force create mode = 0664
   directory mask = 2775
   force directory mode = 2775
   vfs objects = catia fruit streams_xattr recycle   (fruit-Teil nur mit Fruit, recycle nur mit Recycle; Zeile entfällt ohne beides)
   recycle:repository = .papierkorb/%U               (die recycle:-Zeilen nur mit Recycle)
   recycle:keeptree = yes
   recycle:versions = yes
   recycle:touch = yes
   recycle:exclude_dir = .papierkorb
```
`shares.conf`: Header + je Freigabe `include = /etc/samba/betternas.d/<datei>`.

- [ ] **Step 1: Tests schreiben** – `TestRenderGolden` (drei Freigaben: nur Benutzer rw; Gast + Gruppe ro + none; fruit+recycle), `TestValidateRejects` (Tabelle: `..` im Subdir, Kommentar mit `\n`, Name `homes`, doppelter Name `filme`/`Filme`, keine Rechte und kein Gast, Prinzipal `a b`), `TestMigrationApplies` in `internal/db` (Tabellen existieren, Cascade beim Löschen einer Freigabe).
- [ ] **Step 2: RED** – `go test ./internal/shares ./internal/db` → FAIL.
- [ ] **Step 3: Implementieren**; Golden-Files mit `-update` erzeugen und **lesen**, bevor sie committet werden.
- [ ] **Step 4: GREEN** – `go test ./...` → PASS.
- [ ] **Step 5: Commit** – `feat(shares): share model, validation and samba renderer`

### Task 6: Agent – `share.apply`, smb.conf-Übernahme, Schutz der Pool-Einhängeordner

**Files:**
- Create: `backend/internal/agent/shares.go`, `packaging/samba/smb.conf`
- Modify: `backend/internal/agent/server.go`, `backend/internal/agent/apply.go` (`protectMountpoint` auch für Pool-Units), `packaging/build-deb.sh` (`/usr/share/betternas/smb.conf`), `packaging/scripts/postinst.sh`, `image/tests/check-rootfs.sh`
- Test: `backend/internal/agent/shares_test.go`, Erweiterung `accounts_integration_test.go` → `TestSharesLifecycle`

**Interfaces:**
- Consumes: `shares.State`, `shares.Render` (Task 5)
- Produces: RPC `share.apply` (Parameter `shares.State`, Ergebnis `{changed: []string}`)

Ablauf `share.apply`:
1. `/etc/samba/smb.conf` muss mit `ManagedHeader` beginnen, sonst `conflict` „smb.conf wird nicht von BetterNAS verwaltet“.
2. Für jede Freigabe: `findmnt -t fuse.mergerfs --mountpoint /srv/pools/<pool>` muss Exit 0 liefern, sonst `invalid_params` „Pool <pool> ist nicht eingehängt“. Fehlt der Ordner: `install -d -m 2775 -o root -g users <path>`.
3. Neue Dateien schreiben, veraltete eigene `.conf` (mit Header) entfernen, alte Inhalte merken.
4. `testparm -s` (stdin leer). Exit ≠ 0 → alte Dateien wiederherstellen, neue entfernen, `invalid_params` mit stderr in `details.testparm`.
5. Nur wenn `systemctl is-active smbd` Exit 0: `smbcontrol smbd reload-config`.

`packaging/samba/smb.conf`:
```
# Verwaltet von BetterNAS – nicht manuell bearbeiten
[global]
   workgroup = WORKGROUP
   server string = BetterNAS
   server role = standalone server
   server min protocol = SMB2_10
   map to guest = bad user
   guest account = nobody
   load printers = no
   printing = bsd
   printcap name = /dev/null
   disable spoolss = yes
   log file = /var/log/samba/log.%m
   max log size = 1000
   logging = file
   fruit:metadata = stream
   fruit:model = MacSamba
   include = /etc/samba/betternas.d/shares.conf
```
postinst: Ist `/etc/samba/smb.conf` byte-gleich mit `/usr/share/samba/smb.conf` (Debian-Auslieferung) oder fehlt sie: nach `smb.conf.betternas-orig` sichern, durch `/usr/share/betternas/smb.conf` ersetzen. Fehlt `/etc/samba/betternas.d/shares.conf`: mit Header anlegen. Sonst nichts anfassen.

- [ ] **Step 1: Tests schreiben** – `TestApplySharesCommands` (Reihenfolge findmnt → install → testparm → is-active → smbcontrol), `TestApplySharesRefusesUnmountedPool` (kein `install`, keine Datei geschrieben), `TestApplySharesRollsBackOnTestparmFailure` (alte Datei wiederhergestellt, kein smbcontrol), `TestApplySharesRefusesForeignSmbConf`, `TestApplySharesRemovesOnlyManagedFiles` (fremde `.conf` ohne Header bleibt), `TestApplyProtectsPoolMountpoint` (Storage-Apply ruft `chattr +i /srv/pools/<pool>` für nicht eingehängte Pools).
- [ ] **Step 2: RED** – `go test ./internal/agent -run 'Share|PoolMountpoint'` → FAIL.
- [ ] **Step 3: Implementieren**, smb.conf und postinst wie oben; `check-rootfs.sh` prüft, dass `/etc/samba/smb.conf` mit dem Header beginnt und `shares.conf` existiert.
- [ ] **Step 4: GREEN** – `go test ./...` → PASS.
- [ ] **Step 5: Auf dem ThinkCentre prüfen** – `ssh betternas-dev 'sudo apt-get install -y smbclient'` (nur für Tests), Paket bauen und deployen (`make build deb deploy VERSION=0.0.7-dev`). Expected: `head -1 /etc/samba/smb.conf` zeigt den Header, `smb.conf.betternas-orig` existiert, `testparm -s` Exit 0, `smbd` aktiv.
- [ ] **Step 6: Integrationstest `TestSharesLifecycle`** – Abbruch, wenn `/etc/samba/betternas.d` andere Freigaben als `shares.conf` enthält. Hängt einen eigenen mergerfs über ein Temp-Verzeichnis an `/srv/pools/bnasit` (ohne Unit), legt `bnasit-rw` und `bnasit-ro` an, wendet eine Freigabe `bnasit` (rw: bnasit-rw, ro: bnasit-ro, Recycle an) an. Expected: `smbclient //127.0.0.1/bnasit -U bnasit-rw%… -c 'put …'` ok; dieselbe Aktion als bnasit-ro schlägt mit `NT_STATUS_ACCESS_DENIED` fehl; `del` als bnasit-rw legt die Datei unter `.papierkorb/bnasit-rw/` ab; Freigaben leeren → `testparm -s` kennt `[bnasit]` nicht mehr. `make integration` → PASS.
- [ ] **Step 7: Commit** – `feat(agent): apply samba shares with testparm rollback; take over pristine smb.conf`

### Task 7: API und Frontend – Freigaben

**Files:**
- Create: `backend/internal/shares/manager.go`, `backend/internal/api/shares.go`, `frontend/src/api/shares.ts`, `frontend/src/stores/shares.ts`, `frontend/src/views/shares/SharesView.vue`, `frontend/src/components/shares/ShareDialog.vue`
- Modify: `backend/cmd/betternas-api/main.go` (Manager verdrahten, `storage.DependentsFunc` und `users.UsageFunc` aus dem Freigaben-Manager, `Reconcile` beim Start wie bei Storage), `frontend/src/router.ts` (`/freigaben`), `AppLayout.vue`
- Test: `backend/internal/shares/manager_test.go`, `backend/internal/api/shares_api_test.go`, `frontend/src/__tests__/shares.spec.ts`

**Interfaces:**
- Consumes: `share.apply` (Task 6), `storage.Manager.ListPools`, `users.Manager.List`
- Produces:
  - `func NewManager(d *sql.DB, agent Agent, j *jobs.Manager, hub *events.Hub) *Manager`
  - `List`, `Get`, `Create(ctx, actor, ShareInput) (jobID string, error)`, `Update(ctx, actor, name, ShareInput) (string, error)`, `Delete(ctx, actor, name) (string, error)`, `Reconcile(ctx) error`
  - `func (m *Manager) PoolDependents(ctx, pool string) ([]string, error)` (→ `share:<name>`), `func (m *Manager) PrincipalUsage(ctx, kind, name string) ([]string, error)`
  - Routen: `GET /api/v1/shares`, `GET /api/v1/shares/{name}`, `POST /api/v1/shares` (202), `PUT /api/v1/shares/{name}` (202, Name unveränderlich), `DELETE /api/v1/shares/{name}` (202); Ereignis `shares.changed`
  - `ShareView` = `Share` + `path`, `poolMounted`, `uncPath` (`\\<hostname>\<name>`)

Regeln: Pool muss existieren; Prinzipale müssen als Benutzer/BetterNAS-Gruppe existieren (`invalid_params`); Ablauf wie Storage: synchron validieren, im Job unter Sperre erneut validieren, erst `share.apply`, dann DB-Transaktion, Audit. Löschen entfernt nur die Konfiguration; die Daten bleiben (Text im Dialog).

Frontend: Karten pro Freigabe (Name, Pfad, UNC-Pfad zum Kopieren, Tags Gast/Papierkorb/macOS). Dialog: Name, Pool (Auswahl), Unterordner (Vorbelegung = Name), Kommentar, Schalter sichtbar/Gast/Papierkorb (Standard an)/macOS (Standard an), Rechte-Tabelle (Benutzer oder Gruppe wählen, Zugriff `Kein Zugriff`/`Lesen`/`Lesen und Schreiben`).

- [ ] **Step 1: Tests schreiben** – `TestCreateShareAppliesThenCommits`, `TestCreateShareRejectsUnknownPrincipal`, `TestPoolDeleteBlockedByShare` (Storage-Manager mit `PoolDependents` → `conflict`), `TestUserDeleteBlockedByShare`, `TestApplyFailureLeavesDBUnchanged`; Frontend: ShareDialog emittiert korrekte `ShareInput`, Submit gesperrt ohne Rechte und ohne Gast (`data-test="share-submit"`), Unterordner folgt dem Namen, bis er von Hand geändert wird.
- [ ] **Step 2: RED** – `go test ./internal/shares ./internal/api` und `npx vitest run src/__tests__/shares.spec.ts` → FAIL.
- [ ] **Step 3: Implementieren.**
- [ ] **Step 4: GREEN** – `make test` und `npx vue-tsc --noEmit` → PASS.
- [ ] **Step 5: Auf dem ThinkCentre prüfen** – deployen, im Pool `test` eine Freigabe `Daten` für den Admin anlegen. Expected: `smbclient -L //127.0.0.1 -U <admin>` listet `Daten`; die Freigabe ist vom Mac aus unter `smb://betternas.local/Daten` erreichbar (der Nutzer prüft das im Finder).
- [ ] **Step 6: Commit** – `feat: samba shares in api and web ui`

### Task 8: Einrichtungsassistent

**Files:**
- Create: `backend/internal/api/setup.go`, `frontend/src/views/SetupView.vue`, `frontend/src/api/setup.ts`
- Modify: `frontend/src/router.ts` (Route `/einrichtung`, Weiterleitung nach dem Login), `frontend/src/views/LoginView.vue` (`next` nur für Pfade, die mit genau einem `/` beginnen)
- Test: `backend/internal/api/setup_test.go`, `frontend/src/__tests__/setup.spec.ts`, Erweiterung `LoginView`-Test

**Interfaces:**
- Produces: `GET /api/v1/setup` → `{completed: bool, hostname: string}`; `POST /api/v1/setup/complete` (204, Audit `setup.complete`). Gespeichert als `settings('setup.completed','1')`.

Ablauf: Nach dem Login, wenn `completed == false`, leitet der Router einmal nach `/einrichtung`. Stepper mit vier Schritten: **Datenplatten** (bettet `DisksView` ein), **Speicherpool** (bettet `PoolsView` ein; weiter erst, wenn ein Pool existiert), **Freigabe** (Button öffnet `ShareDialog`, Rechte vorbelegt mit dem angemeldeten Admin `rw`), **Fertig** (zeigt `\\<hostname>\<freigabe>` und `smb://<hostname>.local/<freigabe>`). „Später einrichten“ und „Fertig“ rufen `complete`.

- [ ] **Step 1: Tests schreiben** – API: `completed` ist zuerst false, nach `complete` true, Audit-Eintrag; Router: nach Login mit `completed=false` → `/einrichtung`, mit `true` → `next` bzw. `/`; LoginView: `next=//evil.example` und `next=https://x` führen nach `/`.
- [ ] **Step 2: RED** → FAIL.
- [ ] **Step 3: Implementieren.**
- [ ] **Step 4: GREEN** – `make test` → PASS.
- [ ] **Step 5: Commit** – `feat: first-run setup wizard; restrict login redirect to local paths`

### Task 9: APT-Repository – Schlüssel, Repo-Werkzeug, Quelle im Image

**Files:**
- Modify: `image/Dockerfile.builder` (`apt-utils gnupg`), `packaging/build-deb.sh` (Keyring), `image/build-rootfs.sh` und `image/hooks/rootfs/40-apt.sh` (Quelle aus `APT_URL`), `Makefile` (`APT_URL ?=`, Ziele `apt-key`, `repo`, `dev-repo`), `image/tests/check-rootfs.sh`, `README.md`
- Create: `tools/apt-key.sh`, `tools/apt-repo.sh`, `tools/dev-repo.sh`, `packaging/apt/betternas-archive-keyring.asc` (von `apt-key.sh` erzeugt)

**Interfaces:**
- Produces:
  - `tools/apt-key.sh` – erzeugt (im Builder-Container, `GNUPGHOME=${BNAS_APT_GNUPG:-$HOME/.betternas/apt-gnupg}` eingebunden) einen Ed25519-Signierschlüssel „BetterNAS Archive Signing Key“ ohne Ablaufdatum und exportiert den öffentlichen Schlüssel nach `packaging/apt/betternas-archive-keyring.asc`. Bricht ab, wenn schon ein Schlüssel existiert.
  - `tools/apt-repo.sh <repo-dir> <deb>...` – legt Pakete unter `pool/main/b/betternas/` ab (bestehende bleiben), erzeugt `dists/stable/main/binary-{amd64,arm64}/Packages{,.gz}`, `Release` (Origin/Label `BetterNAS`, Suite/Codename `stable`, Components `main`, Architectures `amd64 arm64`), `InRelease` und `Release.gpg`.
  - Paket enthält `/usr/share/keyrings/betternas-archive-keyring.gpg` (per `gpg --dearmor`).
  - Mit `APT_URL` schreibt der Hook `/etc/apt/sources.list.d/betternas.sources` (`Types: deb`, `URIs: $APT_URL`, `Suites: stable`, `Components: main`, `Signed-By: /usr/share/keyrings/betternas-archive-keyring.gpg`).
  - `tools/dev-repo.sh` – baut das Repo nach `dist/apt`, kopiert es nach `betternas-dev:/srv/appdata/betternas-repo`, startet dort `systemd-run --unit=bnas-dev-repo` mit einem statischen HTTP-Server auf `127.0.0.1:8088` und schreibt die Quelle auf `http://127.0.0.1:8088`. Als Server dient `python3 -m http.server`, falls vorhanden; sonst bricht das Skript mit Hinweis ab.

- [ ] **Step 1: Schlüssel erzeugen** – `make apt-key`. Expected: `~/.betternas/apt-gnupg` existiert, `packaging/apt/betternas-archive-keyring.asc` beginnt mit `-----BEGIN PGP PUBLIC KEY BLOCK-----`. Dem Nutzer sagen, dass er `~/.betternas/apt-gnupg` sichern muss.
- [ ] **Step 2: Prüfung schreiben** – `check-rootfs.sh`: Keyring existiert; mit gesetztem `APT_URL` enthält `betternas.sources` genau diese URL, ohne gibt es die Datei nicht. RED: `make rootfs` ohne Änderungen → Keyring-Prüfung schlägt fehl.
- [ ] **Step 3: Implementieren.**
- [ ] **Step 4: Repo prüfen** – `make repo` und im Builder `gpgv --keyring <keyring> dists/stable/InRelease`. Expected: „Good signature“.
- [ ] **Step 5: Ende-zu-Ende auf dem ThinkCentre** – `make dev-repo VERSION=0.0.8-dev`, dann `ssh betternas-dev 'sudo apt-get update && apt-cache policy betternas'`. Expected: Kandidat `0.0.8-dev` aus `http://127.0.0.1:8088 stable/main`, keine Signaturwarnung.
- [ ] **Step 6: Commit** – `feat: signed apt repository tooling and betternas apt source in image`

### Task 10: Updates in der GUI

**Files:**
- Create: `backend/internal/agent/updates.go`, `backend/internal/api/updates.go`, `frontend/src/api/updates.ts`, `frontend/src/views/system/UpdatesView.vue`
- Modify: `backend/internal/agent/server.go`, `frontend/src/router.ts` (`/system/updates`), `AppLayout.vue` (Navigationsgruppe „System“)
- Test: `backend/internal/agent/updates_test.go`, `backend/internal/api/updates_test.go`, `frontend/src/__tests__/updates.spec.ts`

**Interfaces:**
- Produces RPC:
  - `updates.check` → `apt-get update`, dann `apt-get -s -o Debug::NoLocking=1 dist-upgrade`; parst Zeilen `^Inst (\S+) (?:\[(\S+)\] )?\((\S+)` → `{packages: [{name, current, candidate}]}`
  - `updates.install` → `systemd-run --unit=betternas-update-<unix> --property=Type=oneshot --property=RemainAfterExit=yes --setenv=DEBIAN_FRONTEND=noninteractive apt-get -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold dist-upgrade` → `{unit}`; läuft schon eine `betternas-update-*`-Unit → `conflict`
  - `updates.status {unit}` (Unit-Name `^betternas-update-[0-9]+$`) → `{state: running|succeeded|failed, log}` aus `systemctl show -p ActiveState,Result` und `journalctl -u <unit> -o cat -n 200`
- API: `GET /api/v1/updates` → `{packages, checkedAt, run}`; `POST /api/v1/updates/check` (202 Job); `POST /api/v1/updates/install` (202 `{unit}`, Unit in `settings('updates.unit')`); `GET /api/v1/updates/run` → Status der letzten Unit. Audit `updates.install`.

UI: Liste mit Paket, installierter und neuer Version, Buttons „Nach Updates suchen“ und „Updates installieren“ (Bestätigung). Während des Laufs fragt die Seite alle 2 s `GET /updates/run` ab und zeigt das Log; Netzwerkfehler (API startet neu) zeigt sie als „BetterNAS startet neu …“, ohne abzubrechen. Nach Erfolg mit geänderter Version aus `system/info` lädt sie neu.

- [ ] **Step 1: Tests schreiben** – `TestParseSimulatedUpgrade` (Fixture mit Upgrade und Neuinstallation), `TestInstallUpdatesUsesTransientUnit` (exakter `systemd-run`-Aufruf, kein direktes `apt-get` im Agent-Prozess), `TestInstallRefusesWhileRunning`, `TestStatusRejectsForeignUnit` (`ssh.service` → `invalid_params`); Frontend: UpdatesView pollt weiter, wenn `fetch` zweimal fehlschlägt, und zeigt danach „Erfolgreich“.
- [ ] **Step 2: RED** → FAIL.
- [ ] **Step 3: Implementieren.**
- [ ] **Step 4: GREEN** – `make test` → PASS.
- [ ] **Step 5: Ende-zu-Ende auf dem ThinkCentre** – 0.0.9-dev deployen, 0.0.10-dev über `make dev-repo` veröffentlichen, in der GUI suchen und installieren. Expected: Die Seite zeigt das Log, übersteht den Neustart, `dpkg -l betternas` zeigt 0.0.10-dev, `dpkg --audit` ist leer.
- [ ] **Step 6: Commit** – `feat: show and install apt updates from the web ui`

### Task 11: Release 0.1.0 – Ende-zu-Ende mit Datenplatte und SMB

**Files:**
- Modify: `test/image/e2e.sh`, `image/packages/rootfs.list` (nur falls Prüfungen fehlende Pakete zeigen), `README.md`

**Interfaces:**
- Consumes: alle vorherigen Tasks

e2e-Erweiterung (nach dem Boot des installierten Systems):
- Zusätzliche virtuelle Platte (8 GiB, `serial=bnas-e2e-data`) und QEMU-Portweiterleitung `hostfwd=tcp::4445-:445`.
- Über die API (curl mit Cookie und CSRF-Token): Platte formatieren (Vorschau → Token → Format mit Seriennummer), Pool `e2e` anlegen, Benutzer `e2euser` anlegen, Freigabe `E2E` mit `e2euser: rw` anlegen, Einrichtung abschließen. Jobs bis zum Ende abfragen.
- `smbclient //127.0.0.1/E2E -p 4445 -U e2euser%… -c 'put /etc/hostname test.txt; ls'` → Datei gelistet.
- Neustart der VM. Danach sind Pool und Freigabe wieder da (`smbclient … -c 'get test.txt -'`).

- [ ] **Step 1: e2e erweitern** und gegen das alte ISO laufen lassen (RED: API-Routen fehlen).
- [ ] **Step 2: ISO bauen und testen** – `make remote VERSION=0.1.0 MODES="uefi bios secureboot"`. Expected: alle drei Modi PASS, ISO `dist/betternas-0.1.0-amd64.iso`.
- [ ] **Step 3: Repo für das Release erzeugen** – `make repo VERSION=0.1.0` (Hosting entscheidet der Nutzer; ohne `APT_URL` enthält das ISO keine BetterNAS-Quelle, und das README sagt das).
- [ ] **Step 4: Commit** – `release: 0.1.0`

---

## Bewusst nicht in Phase 2

- Self-Service-Seite für Benutzer ohne Admin-Recht (Spec: optional). Admins setzen Passwörter.
- Schreibender Gastzugriff: Gäste sind immer nur lesend.
- NFS, Time Machine, Quotas.
- Hosting von Repo und ISO (offener Punkt der Spec; der Nutzer entscheidet vor der Veröffentlichung).
