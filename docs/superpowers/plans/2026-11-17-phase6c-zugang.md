# Phase 6c – Zugang: TOTP-2FA und NFS-Freigaben – Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (inline). Steps use checkbox syntax.

**Goal:** (A) Administratoren können für ihre GUI-Anmeldung optional eine Zwei-Faktor-Anmeldung (TOTP, Authenticator-App) mit Wiederherstellungscodes einschalten. (B) Eine Freigabe lässt sich zusätzlich per NFS für bestimmte LAN-Clients exportieren.

**Architecture (Entscheidungen, Spec §4.2 / §4.3 / §8):**
- **TOTP ohne Fremdbibliothek** (`internal/auth/totp.go`): RFC 6238 mit HMAC-SHA1, 6 Stellen, 30 s, Toleranz ±1 Schritt, Wiederverwendungsschutz über den zuletzt akzeptierten Zeitschritt. Geheimnis (160 Bit, Base32) liegt im Klartext in der DB (Tabelle `totp`, keine Schlüsselverwaltung vorhanden – wie Mail-Passwort in 6a, im Backup-Hinweis erwähnt).
- **Zweistufige Anmeldung:** Passwort richtig und 2FA aktiv → `POST /api/v1/auth/login` antwortet `200 {"mfaRequired":true,"mfaToken":"…"}` **ohne** Sitzung. Der `mfaToken` (zufällig, nur im Speicher, 5 min gültig, einmal verwendbar, höchstens 5 Fehlversuche) wird mit `POST /api/v1/auth/login/totp {mfaToken, code}` (6-stelliger Code **oder** Wiederherstellungscode) gegen die Sitzung getauscht. Fehlversuche zählen zusätzlich im bestehenden IP-Rate-Limit.
- **Einrichten** (angemeldet, Profilseite): `setup` erzeugt ein vorläufiges Geheimnis (`enabled=0`) und liefert `otpauth://`-URI + Geheimnis (QR im Browser mit `qrcode`, nur Anzeige); `enable {code}` bestätigt und liefert **einmalig** 10 Wiederherstellungscodes (SHA-256 gespeichert, je einmal nutzbar); `disable {code}` (aktueller TOTP- oder Wiederherstellungscode) schaltet ab. Aussperrung: ein anderer Administrator kann 2FA eines Benutzers zurücksetzen (`DELETE /api/v1/users/{name}/totp`), notfalls `sqlite3 /var/lib/betternas/betternas.db "DELETE FROM totp WHERE username='…'"` (Spec §8).
- **NFS = zusätzlicher Export einer bestehenden Freigabe** (kein eigener Freigabetyp): `Share.NFS` (`enabled`, `clients []string`, `readOnly`, `squash` `root|all`). Clients sind Pflicht und nur IP, CIDR oder Hostname (**nie `*`**). Der Agent schreibt `/etc/exports.d/betternas.exports` (Kopfzeile `ManagedHeader`, je Export `fsid=<stabil>`), lädt per `exportfs -ra` und setzt bei Fehler alles zurück (wie testparm bei Samba). NFS kennt keine Benutzerrechte der Freigabe (ACLs gelten nur für Samba): Zugriff regelt allein die Client-Liste; die Oberfläche sagt das deutlich.
- **mergerfs für NFS:** Pool-Mount-Optionen bekommen `noforget,inodecalc=path-hash,nfsopenhack=all`. Geänderte Mount-Units greifen erst nach Neumounten/Neustart; der Agent prüft deshalb vor dem Export per `findmnt -o OPTIONS`, ob der Pool `noforget` bereits aktiv hat, und lehnt sonst mit dem Hinweis „Pool neu einhängen (Neustart)“ ab.
- **Nicht in 6c:** TOTP-Pflicht für alle/Erzwingen, WebAuthn, NFS-Kerberos/`sec=krb5`, NFSv3-Spezialfälle, Time-Machine-Ziel, 2FA für Samba/SSH.

**Tech Stack:** Go (`crypto/hmac`, `encoding/base32`), SQLite-Migrationen, Vue 3 + PrimeVue, `qrcode` (npm, nur Darstellung), `nfs-kernel-server`.

**Spec:** `docs/specs/2026-11-15-betternas-design.md` §4.2 (NFS), §4.3 (TOTP-2FA); Abweichungen kommen nach Abschluss in §8.

## Global Constraints
- Alle Texte deutsch; Fehler als `apierr`; Audit (`audit.Record`) für 2FA einrichten/bestätigen/abschalten/zurücksetzen, fehlgeschlagene 2FA-Anmeldung (`auth.login.totp`, Result `error`) und NFS-Änderungen (läuft über die vorhandenen Freigaben-Audits).
- Code: 6 Ziffern `^\d{6}$`; Wiederherstellungscode Format `XXXXX-XXXXX` (Base32, Großbuchstaben/Ziffern), 10 Stück; Vergleich konstantzeitig (`subtle.ConstantTimeCompare`).
- `mfaToken` nie in Logs/Audit; 32 Byte Zufall, URL-sicher Base64.
- 2FA-Routen (außer `login`/`login/totp`) verlangen Sitzung + CSRF wie alle `/api/`-Routen.
- NFS-Clients: `^[A-Za-z0-9.:/_-]{1,253}$` und als IPv4/IPv6-Adresse, CIDR (`net.ParseCIDR`) oder Hostname (RFC-1123-Labels); `*`, Leerzeichen und `(` `)` `,` verboten (Export-Optionssyntax!).
- Exportzeile (Beispiel): `<pfad> <client>(rw,sync,no_subtree_check,root_squash,fsid=<n>)`; `squash=all` → `all_squash,anonuid=65534,anongid=100`; `readOnly` → `ro`.
- Bestehendes Muster beibehalten: Manager mit `db`/`agent`/`hub`; Routen `registerX`; Agent-Methoden mit `registerX(s, d)`.

## Review Focus
1. Falscher/abgelaufener/wiederverwendeter TOTP-Code, derselbe Code zweimal im selben Zeitschritt → abgelehnt (Task 1, 3).
2. `mfaToken` zweimal benutzt, nach 5 Fehlversuchen oder nach Ablauf → ungültig; ein `mfaToken` ist nie eine Sitzung (Task 3).
3. Wiederherstellungscode nur einmal nutzbar; nach dem Abschalten sind alle Codes ungültig (Task 2, 3, 4).
4. 2FA einschalten ohne gültigen Bestätigungscode → bleibt aus; zweites `setup` überschreibt nur ein *vorläufiges* Geheimnis, nie ein aktives (Task 2, 4).
5. NFS-Client `*`, leer, mit Klammer/Komma/Leerzeichen oder Hostname mit Sonderzeichen → abgelehnt, keine Exportzeile entsteht (Task 6, 7).
6. `exportfs -ra` schlägt fehl → Exportdatei und Samba-Dateien wie vorher (Task 7).
7. Pool ohne `noforget` aktiv → NFS-Export abgelehnt mit klarem Hinweis, Samba-Teil unverändert (Task 7).
8. Zwei Freigaben mit demselben `fsid` (Hash-Kollision) → abgelehnt statt stillschweigend überschrieben (Task 6).

---

## File Structure
- `backend/internal/auth/totp.go`, `totp_store.go`, `mfa.go` (+ Tests) – Code, Speicher, zweistufige Anmeldung; `handlers.go`, `backend/internal/api/totp.go` – Routen.
- `backend/internal/db/migrations/0007_totp.sql`, `0008_share_nfs.sql`.
- `backend/internal/shares/state.go` (`NFS`), `render.go`/`nfs.go` (Exportdatei), `manager.go`; `backend/internal/agent/shares.go` (Anwenden), `backend/internal/storage/render.go` (mergerfs-Optionen).
- `frontend/src/api/auth.ts` o. Ä., `stores/auth.ts`, `views/LoginView.vue`, `views/ProfileView.vue`, `components/users/TotpCard.vue`, `components/shares/*` (Freigabe-Dialog/-Liste).

## Tasks
- [ ] 1 TOTP-Kern (`internal/auth/totp.go`)
- [ ] 2 TOTP-Speicher und Wiederherstellungscodes (Migration 0007)
- [ ] 3 Zweistufige Anmeldung (`mfaToken`, `login/totp`)
- [ ] 4 Verwaltungsrouten (status, setup, enable, disable, Admin-Reset)
- [ ] 5 GUI: Anmeldung Schritt 2, Profil-Karte mit QR
- [ ] 6 NFS-Datenmodell (`Share.NFS`, Validierung, Migration 0008, Manager)
- [ ] 7 NFS-Anwenden im Agent, mergerfs-Optionen, Paket
- [ ] 8 GUI: NFS im Freigaben-Dialog und in der Liste
- [ ] 9 Integrationstest, Ausrollen (0.5.0-rc3), Spec §8, Memory

### Task 1: TOTP-Kern

**Files:** Create `backend/internal/auth/totp.go`, `totp_test.go`

**Interfaces:**
- Produces:
  ```go
  const TOTPDigits = 6; const TOTPPeriod = 30
  func NewSecret() (string, error)                                  // 20 Zufallsbytes, Base32 ohne Padding, Großbuchstaben
  func TOTPCode(secret string, step int64) (string, error)           // 6-stellig mit führenden Nullen; Fehler bei ungültigem Base32
  func TOTPStep(t time.Time) int64                                   // Unix/30
  func VerifyTOTP(secret, code string, now time.Time, lastStep int64) (step int64, ok bool) // ±1 Schritt, nur Schritte > lastStep
  func OTPAuthURI(issuer, account, secret string) string              // otpauth://totp/<issuer>:<account>?secret=…&issuer=…&algorithm=SHA1&digits=6&period=30 (URL-kodiert)
  func NewRecoveryCodes(n int) (plain []string, err error)            // Format XXXXX-XXXXX
  func HashRecoveryCode(code string) string                           // SHA-256 hex über normalisierten Code (Großbuchstaben, ohne Bindestrich/Leerzeichen)
  ```

- [ ] **Step 1: Tests**
  - `TestTOTPRFC6238Vectors`: Secret `GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ` (= ASCII `12345678901234567890`): Zeit 59 → Schritt 1 → `287082`; 1111111109 → `081804`; 1111111111 → `050471`; 2000000000 → `279037` (6-stellige Endungen der RFC-Werte 94287082, 07081804, 14050471, 69279037).
  - `TestVerifyTOTPWindowAndReplay`: Code des aktuellen, vorherigen und nächsten Schritts ok; Schritt ±2 → nicht ok; mit `lastStep == step` → nicht ok (Wiederverwendung); gibt den verbrauchten Schritt zurück; `"12345"`, `"abcdef"`, `""` → nicht ok.
  - `TestNewSecretAndURI`: Länge 32 Base32-Zeichen; zwei Secrets verschieden; URI enthält `issuer=BetterNAS`, `secret=`, kodiertes `@`/Leerzeichen im Konto.
  - `TestRecoveryCodes`: 10 Stück, Format `^[A-Z2-7]{5}-[A-Z2-7]{5}$`, alle verschieden; `HashRecoveryCode("abcde-fghij")==HashRecoveryCode("ABCDEFGHIJ")`.
- [ ] **Step 2:** `go test ./internal/auth/ -run 'TOTP|Recovery|Secret'` → FAIL. **Step 3:** implementieren (HMAC-SHA1, dynamische Kürzung nach RFC 4226). **Step 4:** PASS.
- [ ] **Step 5: Commit** `feat(auth): totp core (rfc 6238)`

### Task 2: TOTP-Speicher und Wiederherstellungscodes

**Files:** Create `backend/internal/db/migrations/0007_totp.sql`, `backend/internal/auth/totp_store.go`, `totp_store_test.go`; Modify `backend/internal/db/db_test.go` (Version 7)

**Interfaces:**
- Migration: `totp(username TEXT PRIMARY KEY, secret TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 0, last_step INTEGER NOT NULL DEFAULT 0, recovery_json TEXT NOT NULL DEFAULT '[]', created_at TEXT NOT NULL)`.
- Produces:
  ```go
  type TOTPStore struct{ … }
  func NewTOTPStore(d *sql.DB) *TOTPStore
  func (s *TOTPStore) Enabled(ctx context.Context, user string) (bool, error)
  func (s *TOTPStore) BeginSetup(ctx context.Context, user, secret string) error      // legt vorläufig an/ersetzt; apierr.Conflict, wenn bereits aktiv
  func (s *TOTPStore) Pending(ctx context.Context, user string) (secret string, ok bool, err error)
  func (s *TOTPStore) Enable(ctx context.Context, user string, step int64, recoveryHashes []string) error // enabled=1, last_step=step, Codes speichern
  func (s *TOTPStore) Verify(ctx context.Context, user, code string, now time.Time) (bool, error)         // TOTP (mit Replay-Schutz, schreibt last_step) ODER Wiederherstellungscode (wird verbraucht); nur bei enabled=1
  func (s *TOTPStore) Disable(ctx context.Context, user string) error                    // Zeile löschen (idempotent)
  ```

- [ ] **Step 1: Tests:** `TestSetupEnableVerifyFlow` (BeginSetup → `Enabled`=false; `Verify` bei nicht aktiv → false; Enable → true); `TestVerifyReplayRejected` (derselbe gültige Code zweimal → zweites Mal false); `TestRecoveryCodeSingleUse` (einmal true, danach false, andere Codes bleiben gültig); `TestBeginSetupRefusedWhenActive` (→ `conflict`, Geheimnis unverändert); `TestBeginSetupReplacesPending`; `TestDisableInvalidatesEverything` (danach Verify mit altem TOTP- und Wiederherstellungscode false, `Enabled` false); `TestUsersIsolated` (Code von `anna` gilt nicht für `ben`).
- [ ] **Step 2–4:** FAIL → implementieren (Transaktion für Verify, damit `last_step`/Codes atomar sind) → PASS. **Step 5: Commit** `feat(auth): totp store with recovery codes`

### Task 3: Zweistufige Anmeldung

**Files:** Create `backend/internal/auth/mfa.go`, `mfa_test.go`; Modify `backend/internal/auth/handlers.go` (`NewHandlers` bekommt optional `WithTOTP(*TOTPStore)`; `Login`, neuer `LoginTOTP`), `backend/internal/api/server.go` (Route `POST /api/v1/auth/login/totp` auf `root` neben `login`, ohne Sitzung), `backend/cmd/betternas-api/main.go`

**Interfaces:**
- ```go
  type mfaTokens struct{ … }                                           // Speicher im Prozess
  func (m *mfaTokens) New(user string) string                           // 5 min, max. 5 Fehlversuche
  func (m *mfaTokens) Peek(token string) (user string, ok bool)
  func (m *mfaTokens) Fail(token string) (remaining int)               // zählt hoch, löscht bei 0
  func (m *mfaTokens) Consume(token string)
  func (h *Handlers) WithTOTP(s *TOTPStore) *Handlers
  func (h *Handlers) LoginTOTP(w http.ResponseWriter, r *http.Request)  // Body {"mfaToken","code"}
  ```
- `Login`: nach erfolgreichem Passwort und Gruppenprüfung und `TOTPStore.Enabled` → `200 {"mfaRequired":true,"mfaToken":…}`, **kein Cookie**, Audit `auth.login` Result `mfa`. `LoginTOTP`: IP-Rate-Limit prüfen; Token unbekannt/abgelaufen → `401`; Code falsch → `Fail`, IP-`limiter.Fail`, Audit `auth.login.totp` `error`, `401` „Code ungültig“ (nach 5 Fehlversuchen „Bitte erneut anmelden“, Token tot); Code richtig → `Consume`, Sitzung + Cookie wie `Login`, Audit `auth.login.totp` `ok`.

- [ ] **Step 1: Tests (httptest, `AgentVerifier`-Fake, vorhandene Muster in `auth_test.go`):** `TestLoginWithoutTOTPUnchanged`; `TestLoginRequiresSecondStep` (kein `Set-Cookie`, `mfaRequired:true`, Token nicht leer); `TestLoginTOTPSuccess` (richtiger Code → Cookie, Sitzung gültig für `Me`); `TestLoginTOTPWrongCode401AndCounts`; `TestMfaTokenSingleUse` (zweiter Tausch → 401); `TestMfaTokenLockedAfterFiveFailures` (6. Versuch mit richtigem Code → 401); `TestMfaTokenExpires` (Uhr vorstellen); `TestMfaTokenIsNotASession` (als Cookie `bnas_session` benutzt → 401 an `/api/v1/auth/me`); `TestReplayedCodeRejected` (derselbe Code nach erfolgreicher Anmeldung erneut → 401); `TestRecoveryCodeLogsIn` (einmal ok, danach 401); `TestIPRateLimitAppliesToTOTP` (5 Fehlversuche → `429`).
- [ ] **Step 2–4:** FAIL → implementieren → `go test ./...` PASS. **Step 5: Commit** `feat(auth): two-step login with totp`

### Task 4: Verwaltungsrouten

**Files:** Create `backend/internal/api/totp.go`, `totp_api_test.go`; Modify `backend/internal/api/server.go` (`Deps.TOTP *auth.TOTPStore`, `registerTOTP`), `backend/internal/auth/handlers.go` (`Me` liefert zusätzlich `totp bool`), `backend/cmd/betternas-api/main.go`

**Routen** (Sitzung + CSRF):
```
GET    /api/v1/auth/totp                → {"enabled":bool}
POST   /api/v1/auth/totp/setup          → {"secret","uri"}        (409, wenn aktiv)
POST   /api/v1/auth/totp/enable         Body {"code"} → {"recoveryCodes":[10]}   (400/409 bei falschem Code/ohne setup)
POST   /api/v1/auth/totp/disable        Body {"code"} → 204       (aktueller TOTP- oder Wiederherstellungscode; falsch → 403)
DELETE /api/v1/users/{name}/totp        → 204   (Admin setzt 2FA eines *anderen* Benutzers zurück; für sich selbst 409 → disable nutzen; Audit)
```
- Audit: `auth.totp.setup|enable|disable|reset` (Ziel: Benutzer).

- [ ] **Step 1: Tests:** `TestTotpSetupEnableFlow` (setup → uri enthält Konto; enable mit berechnetem Code → 10 Codes; danach `GET` enabled; erneutes setup → 409); `TestEnableWrongCodeStaysOff`; `TestEnableWithoutSetupConflicts`; `TestDisableNeedsValidCode` (falsch → 403 und noch aktiv; Wiederherstellungscode funktioniert und ist danach verbraucht); `TestAdminResetOtherUser` (anderer Benutzer ohne 2FA danach `enabled:false`; eigener Name → 409; unbekannter Benutzer → 404/204 idempotent; Audit); `TestMeShowsTotpFlag`; `TestTotpRoutesNeedSession`.
- [ ] **Step 2–4:** FAIL → implementieren → PASS. **Step 5: Commit** `feat(api): totp management routes`

### Task 5: GUI – Anmeldung Schritt 2, Profil-Karte

**Files:** Create `frontend/src/components/users/TotpCard.vue`, `frontend/src/__tests__/totp.test.ts`; Modify `frontend/package.json` (`qrcode` exakt gepinnt, Typen `@types/qrcode`), `src/stores/auth.ts` (`login` liefert `mfaRequired`; `loginTotp(mfaToken, code)`; `Me.totp`), `src/views/LoginView.vue`, `src/views/ProfileView.vue`, `src/api/users.ts` bzw. neue `src/api/totp.ts`, Admin-Aktion „2FA zurücksetzen“ in `UsersView.vue`

**Verhalten:** Login: antwortet die API mit `mfaRequired`, wechselt die Seite zu „Code aus deiner Authenticator-App“ (Feld `autocomplete="one-time-code"`, Link „Wiederherstellungscode verwenden“, „Zurück“); bei `401` Fehlertext, nach Token-Tod zurück zu Schritt 1. Profil-Karte „Zwei-Faktor-Anmeldung“: aus → „Einrichten“ → zeigt QR (lazy `import('qrcode')`) + Geheimnis als Text + Eingabe des Codes → nach Bestätigung einmalige Anzeige der Wiederherstellungscodes mit „Kopieren“/„Herunterladen“ und Hinweis „Danach nicht mehr einsehbar“; an → „Abschalten“ (verlangt Code). Benutzerliste: Aktion „2FA zurücksetzen“ (Bestätigung) nur für andere Benutzer.

- [ ] **Step 1: Tests (Vitest):** Login zeigt nach `mfaRequired` das Codefeld und ruft `POST …/auth/login/totp` mit `{mfaToken, code}`, Erfolg → Weiterleitung wie bisher; falscher Code zeigt Fehler und bleibt auf Schritt 2; Wiederherstellungscode-Umschalter sendet denselben Endpunkt; Karte: „Einrichten“ ruft `setup`, zeigt Geheimnis, Bestätigen sperrt ohne 6 Ziffern, nach `enable` erscheinen 10 Codes und der Hinweis; „Abschalten“ sendet `disable` mit Code; Admin-Reset sendet `DELETE …/users/<name>/totp` erst nach Bestätigung; für den eigenen Benutzer gibt es die Aktion nicht. (`qrcode` wird im Test gemockt.)
- [ ] **Step 2–4:** FAIL → umsetzen → `npx vitest run`, `npx vue-tsc --noEmit` PASS. **Step 5: Commit** `feat(web): two-factor login and profile card`

### Task 6: NFS-Datenmodell

**Files:** Create `backend/internal/db/migrations/0008_share_nfs.sql`, `backend/internal/shares/nfs.go`, `nfs_test.go`; Modify `backend/internal/shares/state.go` (`Share.NFS`, `Validate`), `manager.go` (Laden/Speichern, `ShareView.NFSPath`), `db_test.go` (Version 8)

**Interfaces:**
- Migration: `ALTER TABLE shares ADD COLUMN nfs_json TEXT NOT NULL DEFAULT ''` (leer = kein NFS).
- ```go
  type NFS struct { Enabled bool `json:"enabled"`; Clients []string `json:"clients"`; ReadOnly bool `json:"readOnly"`; Squash string `json:"squash"` } // Squash "root" (Standard) | "all"
  func (n NFS) Validate() error                          // nur wenn Enabled: Clients 1..32, je Eintrag gültig (siehe Global Constraints), Squash bekannt
  func ValidClient(c string) bool
  func FSID(shareName string) int                         // 1000 + crc32(lower(name)) % 900000
  ```
  `Share.NFS NFS` (immer gesetzt, bei fehlendem JSON `{Enabled:false, Squash:"root", Clients:[]}`); `State.Validate` prüft zusätzlich: gleicher `FSID` bei zwei NFS-aktiven Freigaben → `invalid_params`.
  `ShareView` bekommt `NFSPath` (= `Path`, wenn NFS aktiv, sonst leer) für den Mount-Hinweis `host:/pfad`.

- [ ] **Step 1: Tests:** `TestValidClient` (Tabelle: `192.168.1.10`, `192.168.1.0/24`, `fe80::1`, `2001:db8::/32`, `nas.local`, `host-1.example.org` ok; `*`, ``, `a b`, `10.0.0.1(rw)`, `a,b`, `-x.de`, `999.1.1.1/40`, 300 Zeichen → nein); `TestNFSValidate` (aktiv ohne Clients → `invalid_params`; Squash `x` → invalid; deaktiviert mit Müll → ok); `TestStateRejectsFsidCollision` (zwei Namen mit gleichem `FSID` per Hilfsfunktion, die Kollision erzwingt); `TestManagerPersistsNFS` (anlegen/ändern/lesen, Standardwerte, ACL unverändert); `TestFSIDStable` (feste erwartete Zahl für „media“, Groß-/Kleinschreibung gleich).
- [ ] **Step 2–4:** FAIL → implementieren → `go test ./...` PASS. **Step 5: Commit** `feat(shares): nfs export settings`

### Task 7: NFS anwenden, mergerfs-Optionen, Paket

**Files:** Modify `backend/internal/shares/render.go` (`RenderExports(State) []byte`), `backend/internal/agent/shares.go` (`ApplyShares`), `backend/internal/storage/render.go` (+ Golden-Dateien), `packaging/build-deb.sh` (`nfs-kernel-server`), `image/packages/rootfs.list`; Tests in `render_test.go`, `agent/shares_test.go`

**Interfaces:**
- `func RenderExports(st State) []byte`: Kopfzeile `ManagedHeader`, je NFS-Freigabe (sortiert nach Name) eine Zeile `<Share.Path()> <client>(<optionen>) …` mit Optionen `rw|ro,sync,no_subtree_check,root_squash|all_squash,anonuid=65534,anongid=100,fsid=<FSID>`; ohne NFS-Freigaben nur die Kopfzeile.
- Pool-Mountoptionen: `…,cache.files=off,dropcacheonclose=true,noforget,inodecalc=path-hash,nfsopenhack=all,fsname=…`.
- `ApplyShares`: Datei `/etc/exports.d/betternas.exports` (nur überschreiben, wenn leer/verwaltet – sonst `conflict`); Reihenfolge: Samba wie bisher → Exportdatei schreiben → bei NFS-Freigaben je Pool `findmnt -n -o OPTIONS --mountpoint <pool>` muss `noforget` enthalten, sonst `conflict` „Der Pool <p> muss für NFS neu eingehängt werden (Neustart des NAS)“ **vor** jeder Änderung → `exportfs -ra`; bei Fehler Exportdatei **und** Samba-Dateien zurücksetzen. Keine NFS-Freigaben und keine alte Exportdatei → nichts schreiben, `exportfs` nicht aufrufen.

- [ ] **Step 1: Tests:** `TestRenderExportsGolden` (Golden: zwei Freigaben, eine `ro`+`squash=all` mit zwei Clients, eine nur Samba → fehlt); `TestRenderExportsEmpty`; `TestStoragePoolUnitHasNfsOptions` (Golden der Mount-Unit aktualisiert); Agent: `TestApplyWritesExportsAndReloads` (Datei da, `exportfs -ra` nach Samba-Reload), `TestApplyNfsRefusedWithoutNoforget` (Review Focus 7: nichts geschrieben, kein `exportfs`), `TestApplyExportfsFailureRollsBack` (Review Focus 6: Exportdatei und Samba-Dateien wie vorher), `TestApplyLeavesForeignExports`, `TestApplyNoNfsDoesNotTouchExportfs`, `TestExportsRejectInjection` (Review Focus 5: Share mit Client `10.0.0.1(rw,no_root_squash)` scheitert schon in `Validate`, nie in der Datei).
- [ ] **Step 2–4:** FAIL → implementieren (Exportdatei erst nach erfolgreichem Samba-Teil, `exportfs -ra` zuletzt) → `go vet ./... && go test ./...` PASS. **Step 5: Commit** `feat(shares): nfs exports via exportfs, mergerfs options for nfs`

### Task 8: GUI – NFS in Freigaben

**Files:** Modify `frontend/src/components/shares/ShareDialog.vue` (Pfad ggf. anpassen), `frontend/src/views/shares/SharesView.vue`, `frontend/src/api/shares.ts`, `frontend/src/__tests__/shares.test.ts`

**Verhalten:** Dialog-Abschnitt „NFS (Linux/Mac/NAS-Clients)“: Schalter, Clients (ein Eintrag je Zeile, Hinweistext „Nur diese Rechner dürfen zugreifen – keine Benutzerrechte; Samba-Rechte gelten hier nicht“), „Nur lesen“, Squash-Auswahl („Root wird abgebildet“ / „Alle werden abgebildet“); clientseitige Prüfung wie `ValidClient` (Wildcard/Klammern sofort markiert). Liste: Tag „NFS“ und Mount-Hinweis `mount -t nfs <host>:<pfad> /mnt` mit Kopieren.

- [ ] **Step 1: Tests:** Schalter zeigt Felder; Speichern sendet `nfs:{enabled,clients[],readOnly,squash}` (Zeilen getrimmt, Leerzeilen entfernt); `*` und `10.0.0.1(rw)` sperren Speichern mit Fehlertext; aktiv ohne Clients gesperrt; Liste zeigt Tag und Mount-Zeile nur bei aktivem NFS.
- [ ] **Step 2–4:** FAIL → umsetzen → `npx vitest run`, `npx vue-tsc --noEmit` PASS. **Step 5: Commit** `feat(web): nfs settings for shares`

### Task 9: Integrationstest, Ausrollen, Doku

**Files:** Create `backend/internal/agent/nfs_integration_test.go` (Tag `integration`); Modify `Makefile` (Testliste), `docs/specs/2026-11-15-betternas-design.md` (§8 „Abweichung Phase 6c“), Memory

- [ ] **Step 1:** `TestNfsExports` (root, `exportfs` vorhanden, Loop-Ordner statt echtem Pool: eigenes Verzeichnis `/var/tmp/bnas-nfs…` mit `exportfs`-tauglichem Dateisystem): schreibt eine Exportdatei im Managed-Format an ihren echten Ort, `exportfs -ra` ohne Fehler, `exportfs -v` nennt den Pfad und den Client, danach Aufräumen (Datei weg, `exportfs -ra`). Überspringt, wenn `/etc/exports.d/betternas.exports` schon existiert.
- [ ] **Step 2:** `make build web deb VERSION=0.5.0-rc3 && make deploy VERSION=0.5.0-rc3`; `make integration` → PASS. Auf dem Testgerät **keinen** echten NFS-Export oder 2FA für den Nutzer einschalten (Pools bräuchten für NFS einen Neustart; 2FA-Test macht der Nutzer selbst im Browser).
- [ ] **Step 3:** Spec §8 (inkl. Hinweis zum Zurücksetzen per `sqlite3`), Memory, Go + Vitest grün; Commit `docs: phase 6c notes`.
