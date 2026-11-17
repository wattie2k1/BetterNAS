# Phase 6a – Überwachung: Benachrichtigungen, SMART-/SnapRAID-Alarme, HDD-Spindown – Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (inline). Steps use checkbox syntax.

**Goal:** BetterNAS meldet Probleme (SMART, Temperatur, fehlende Platte, nicht eingehängter Pool, fehlgeschlagener SnapRAID-Lauf) per Mail und ntfy, die GUI zeigt aktive Meldungen und den Verlauf, und Datenplatten können pro Platte in den Ruhezustand gehen (`hdparm -S`), ohne dass die Überwachung sie weckt.

**Architecture (Entscheidungen, Spec §4.6 / §8):**
- **Überwachung im API-Prozess** (`internal/monitor`): Eine Schleife (alle 5 min, erster Lauf 30 s nach Start) liest Platten (`storage.Manager.ListDisks`), Pools (`ListPools`) und SnapRAID-Status (`SnapraidStatus`) und leitet daraus *Befunde* (`Finding{Key, Level, Title, Body}`) ab. Kein neuer Agent-Code für die Erkennung, keine neuen Timer.
- **Entprellen über die DB:** Tabelle `alerts(key PRIMARY KEY, level, title, body, since)` hält aktive Befunde. Neuer Schlüssel → eine Meldung; Schlüssel verschwunden → eine „Behoben“-Meldung und Zeile löschen. Keine Wiederholung.
- **Kanäle** (`internal/notify`): Mail über `net/smtp` (SMTP-Server frei konfigurierbar, STARTTLS/TLS/unverschlüsselt, optional Login) und ntfy (HTTP-POST an `<server>/<topic>`, optionales Bearer-Token). Konfiguration als JSON in `settings` (Schlüssel `notify.config`). Der API-Prozess hat Netzzugriff (Unit ohne Einschränkung). Jeder Kanal scheitert für sich; Ergebnis je Meldung in `notifications` (Verlauf, letzte 200 behalten).
- **SMART weckt keine Platten:** `smartctl -n standby`; Exit-Bit 2 mit „STANDBY“ = Ruhezustand → letzte bekannte Werte bleiben, `standby: true`. Fehlschlag mit Standardtreiber → einmal mit `-d sat` (USB-Gehäuse).
- **Spindown:** Einstellung `power.spindown` (JSON `{diskId: minuten}`) in `settings`; der Agent setzt `hdparm -S` (Methode `disk.spindown`). `hdparm` merkt sich den Wert nicht über Neustarts hinweg → Manager wendet die Einstellung beim Start (`Reconcile`) und bei `disks.changed` erneut an. Erlaubte Minuten: `0` (aus), `5, 10, 20, 30, 60, 120, 240`.
- **Nicht in 6a:** Mail-Vorlagen, Eskalation/Wiederholung, weitere Kanäle (Telegram, Webhook), Ruhezeit-Fenster, SnapRAID-Wiederherstellung (6b).

**Tech Stack:** Go (`net/smtp`, `crypto/tls`, `net/http`, SQLite-Migration), Vue 3 + PrimeVue, Vitest.

**Spec:** `docs/specs/2026-11-15-betternas-design.md` §4.6 (Benachrichtigungen, Energie), §4.1 (SnapRAID-Status); Abweichungen kommen nach Abschluss in §8.

## Global Constraints
- Alle Texte und Fehlermeldungen deutsch; Fehler als `apierr` (`invalid`, `conflict`, …) wie in den übrigen Paketen.
- Mail-Adressen per `net/mail.ParseAddress`; ntfy-Server muss `http://` oder `https://` sein, Topic `^[A-Za-z0-9_-]{1,64}$`; SMTP-Port 1–65535; Sicherheit `none|starttls|tls`.
- Das SMTP-Passwort und das ntfy-Token verlassen die API nie: GET liefert `passwordSet`/`tokenSet` (bool); PUT mit leerem Feld behält den alten Wert.
- Header-Injektion: Titel und Adressen ohne `\r`/`\n` in Mail-Headern (Zeilenumbrüche werden zu Leerzeichen).
- Netzwerk-Timeout je Kanal 15 s; die Überwachung blockiert nie länger als 1 min pro Lauf.
- Temperatur-Schwelle Standard 55 °C (Einstellung, 30–90); Temperatur `0` (unbekannt, z. B. USB) löst nie aus.
- Jede Änderung (Einstellungen speichern, Testnachricht, Spindown) landet im Audit-Log (`audit.Record`).
- Bestehendes Muster beibehalten: Manager mit `db`, `agent`, `hub`; Routen in `internal/api/<thema>.go` (`registerX`); Agent-Methoden in `internal/agent/<thema>.go` mit `registerX(s, d)` in `Register`.

## Review Focus
1. SMTP-Server nicht erreichbar / hängt → Testnachricht liefert verständlichen Fehler nach ≤ 15 s, Überwachung läuft weiter, Verlauf zeigt `error` (Task 2, 4).
2. Zeilenumbruch im Titel/Betreff → kein zusätzlicher Header (Task 2).
3. GET der Einstellungen enthält nie Passwort/Token; PUT ohne Passwort behält es (Task 1, 6).
4. Schlafende Platte: kein Aufwecken, Werte bleiben, keine „SMART-Fehler“-Meldung wegen leerer Antwort (Task 3, 4).
5. Derselbe Befund in zwei Läufen → nur eine Meldung; Behebung → genau eine „Behoben“-Meldung; Neustart der API wiederholt nichts (Task 4).
6. Ein Kanal fällt aus → der andere sendet trotzdem (Task 2).
7. Spindown auf nicht unterstützter Platte (USB) → Fehlermeldung, Einstellung wird nicht gespeichert (Task 5).

---

## File Structure
- `backend/internal/db/migrations/0005_notifications.sql` – Tabellen `alerts`, `notifications`.
- `backend/internal/notify/config.go` – `Config`, Laden/Speichern/Validieren, Maskierung.
- `backend/internal/notify/mail.go`, `ntfy.go` – Sender; `notify.go` – `Service` (Senden, Verlauf, Test).
- `backend/internal/monitor/monitor.go` – Befunde, Abgleich, Schleife.
- `backend/internal/storage/spindown.go` – Minuten→`-S`-Wert, Manager-Methoden, Reconcile.
- `backend/internal/agent/disks.go` (SMART), `backend/internal/agent/spindown.go` – `disk.spindown`.
- `backend/internal/api/notifications.go` – Routen; Verdrahtung in `server.go`, `cmd/betternas-api/main.go`.
- `frontend/src/api/notifications.ts`, `stores/notifications.ts`, `views/system/NotificationsView.vue`; Änderungen in `router.ts`, `AppLayout.vue`, `DisksView.vue`, `DashboardView.vue`, `api/storage.ts`.

## Tasks
- [ ] 1 Migration und Konfiguration (`internal/notify/config.go`)
- [ ] 2 Sender, Service, Verlauf (`internal/notify`)
- [ ] 3 SMART: Ruhezustand, `-d sat`, Sektorwerte (Agent + Manager)
- [ ] 4 Monitor (`internal/monitor`)
- [ ] 5 Spindown (Agent, Manager, Route, Paket `hdparm`)
- [ ] 6 API-Routen und Verdrahtung
- [ ] 7 GUI
- [ ] 8 Integrationstest, Ausrollen (0.5.0-rc1), Spec §8, Memory

### Task 1: Migration und Konfiguration

**Files:** Create `backend/internal/db/migrations/0005_notifications.sql`, `backend/internal/notify/config.go`, `backend/internal/notify/config_test.go`

**Interfaces:**
- Produces:
  ```go
  type Mail struct { Enabled bool; Host string; Port int; Security string; Username, Password, From string; To []string }
  type Ntfy struct { Enabled bool; Server, Topic, Token string }
  type Config struct { Mail Mail; Ntfy Ntfy; TempWarnC int }
  type PublicConfig // wie Config, aber Mail.Password/Ntfy.Token ersetzt durch PasswordSet/TokenSet (JSON: passwordSet, tokenSet)
  func LoadConfig(ctx context.Context, db *sql.DB) (Config, error)         // Standard: alles aus, Port 587, Security "starttls", TempWarnC 55
  func SaveConfig(ctx context.Context, db *sql.DB, in Config) error        // validiert; leeres Password/Token übernimmt den gespeicherten Wert
  func (c Config) Public() PublicConfig
  func (c Config) Validate() error
  ```
- Migration: `alerts(key TEXT PRIMARY KEY, level TEXT NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL, since TEXT NOT NULL)` und `notifications(id INTEGER PRIMARY KEY AUTOINCREMENT, ts TEXT NOT NULL, level TEXT NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL, results_json TEXT NOT NULL)`; JSON-Schlüssel der Config camelCase (`tempWarnC`, …).

- [ ] **Step 1: Tests** (Namen und Assertions)
  - `TestLoadConfigDefaults`: leere DB → `Mail.Enabled==false`, `Mail.Port==587`, `Mail.Security=="starttls"`, `TempWarnC==55`.
  - `TestSaveConfigRoundTripKeepsSecret`: speichern mit Password „geheim“, danach erneut speichern mit `Password==""` → `LoadConfig().Mail.Password=="geheim"`; `Public().Mail.PasswordSet==true`, JSON von `Public()` enthält „geheim“ nicht.
  - `TestValidate` (Tabelle): ungültige Adresse, Port 0/70000, Security „ssl“, ntfy-Server „ftp://x“, Topic „a b“, `TempWarnC` 29 und 91 → je `apierr`-Fehler `invalid`; aktivierter Kanal ohne Host/Server → `invalid`; deaktivierter Kanal mit leeren Feldern → ok.
- [ ] **Step 2:** `go test ./internal/notify/ -run 'Config|Validate'` → FAIL (Paket fehlt).
- [ ] **Step 3:** Migration und `config.go` implementieren (Muster `saveReport`/`loadReport` in `internal/docker/manager.go`, Schlüssel `notify.config`). Test-DB wie in `internal/db/db_test.go` (`db.Open(t.TempDir()+…)`).
- [ ] **Step 4:** Tests → PASS; außerdem `go test ./internal/db/` (Migrationen laufen).
- [ ] **Step 5: Commit** `feat(notify): migration and config store`

### Task 2: Sender, Service, Verlauf

**Files:** Create `backend/internal/notify/mail.go`, `ntfy.go`, `notify.go`, `notify_test.go`

**Interfaces:**
- Consumes: Task 1 `Config`, `LoadConfig`.
- Produces:
  ```go
  type Level string // "info" | "warn" | "crit"
  type Message struct { Level Level; Title, Body string }
  type Result struct { Channel string `json:"channel"`; OK bool `json:"ok"`; Error string `json:"error,omitempty"` }
  type HistoryEntry struct { ID int64; TS time.Time; Level Level; Title, Body string; Results []Result }
  type Service struct{ … }
  func NewService(db *sql.DB, hub *events.Hub) *Service
  func (s *Service) Send(ctx context.Context, m Message) []Result      // an alle aktivierten Kanäle, schreibt Verlauf (max. 200 Zeilen), nie Fehler nach außen
  func (s *Service) SendTest(ctx context.Context, channel string) error // "mail"|"ntfy", ungespeicherte Config nicht nötig: nutzt gespeicherte; Fehler als apierr.Conflict mit Klartext
  func (s *Service) History(ctx context.Context, limit int) ([]HistoryEntry, error)
  ```
  Test-Naht: `Service` hat die Felder `dial func(ctx, network, addr string) (net.Conn, error)` und `httpc *http.Client`.

- [ ] **Step 1: Tests**
  - `TestMailSendsViaFakeSMTP`: Mini-SMTP-Server (TCP-Listener im Test, versteht EHLO/MAIL/RCPT/DATA/QUIT, Security `none`) empfängt genau eine Mail; Betreff enthält `[BetterNAS]` und den Titel, `To` enthält alle Empfänger, Body UTF-8 (Umlaute überstehen, Betreff als RFC-2047-codiert).
  - `TestMailHeaderInjectionStripped`: Titel `"A\r\nBcc: x@evil"` → Server sieht keinen eigenen `Bcc:`-Header.
  - `TestNtfyPosts`: `httptest.Server`; erwartet `POST /<topic>`, Header `Title` (ASCII-sicher via RFC-2047 bei Umlauten), `Priority` (`warn`→`4`, `crit`→`5`, `info`→`3`), `Authorization: Bearer <token>` nur wenn Token gesetzt, Body = Text.
  - `TestSendOneChannelFailsOtherStillSends`: Mail zeigt auf geschlossenen Port, ntfy ok → `Result` mit `Mail.OK==false` (Fehlertext deutsch, ohne Passwort), `Ntfy.OK==true`; Verlauf enthält beide.
  - `TestSendTimeout`: Server nimmt an, antwortet nie → `Send` kehrt nach ≤ 16 s zurück (Timeout im Test per Feld auf 200 ms verkürzbar: `timeout time.Duration`).
  - `TestHistoryTrim`: 205 Sendungen → `History(…, 500)` liefert 200, neueste zuerst.
  - `TestSendTestUnknownOrDisabledChannel` → `invalid` bzw. `conflict` („Kanal ist nicht aktiviert“).
- [ ] **Step 2:** Tests → FAIL.
- [ ] **Step 3:** Implementieren. Mail: `net/smtp` mit eigener Verbindung (`tls.Dial` bei `tls`, `StartTLS` bei `starttls`, `PlainAuth` nur wenn Benutzername gesetzt und Verbindung verschlüsselt oder Host Loopback), Betreff `mime.QEncoding.Encode("utf-8", …)`. Veröffentlicht nach dem Speichern `events.Message{Type: "notifications.changed"}`.
- [ ] **Step 4:** `go test ./internal/notify/` → PASS.
- [ ] **Step 5: Commit** `feat(notify): mail and ntfy senders with history`

### Task 3: SMART – Ruhezustand, `-d sat`, Sektorwerte

**Files:** Modify `backend/internal/agent/disks.go` (`Smart`, `SmartInfo`), `backend/internal/agent/storage_test.go` bzw. neue `disks_smart_test.go`; Modify `backend/internal/storage/manager.go` (`SmartInfo`, `smartFor`); Test `backend/internal/storage/manager_test.go` (vorhandene Fakes nutzen)

**Interfaces:**
- Produces: `SmartInfo` (Agent *und* Storage, JSON) um `Standby bool json:"standby"`, `ReallocatedSectors int json:"reallocatedSectors"`, `PendingSectors int json:"pendingSectors"` erweitert.
- Verhalten Agent `Smart`: Aufruf immer `smartctl --json -H -A -n standby <device>`. `res.ExitCode&2 != 0` und Ausgabe (Stdout/Stderr) enthält „STANDBY“ → `SmartInfo{Standby: true}`, kein Fehler. Sonst bei `ExitCode&3 != 0` genau ein zweiter Versuch `smartctl --json -H -A -n standby -d sat <device>`; scheitert auch der → Fehler wie bisher. Sektorwerte aus `ata_smart_attributes.table` (ID 5 und 197, `raw.value`).
- Verhalten Manager `smartFor`: Liefert der Agent `Standby: true`, werden die zuletzt bekannten Werte (auch älter als 5 min) mit `Standby: true` zurückgegeben; gibt es keine, nur `{Standby: true}`. Cache-Zeit für Standby-Antworten 5 min wie bisher.

- [ ] **Step 1: Tests**
  - `TestSmartStandbyDoesNotFail`: FakeRunner antwortet mit Exit 2 und `Device is in STANDBY (OS)` → `SmartInfo.Standby==true`, genau ein Aufruf, Aufruf enthält `-n standby`.
  - `TestSmartFallsBackToSat`: erster Aufruf Exit 1, zweiter (`… -d sat …`) liefert gültiges JSON → Werte kommen an; Fehler nur, wenn beide scheitern.
  - `TestSmartParsesSectors`: JSON mit ID 5 → raw 8 und ID 197 → raw 2 → `ReallocatedSectors==8`, `PendingSectors==2`; Exit-Bit 3/4 (Befund) bleibt kein Fehler (bestehender Test bleibt grün).
  - `TestManagerKeepsLastSmartOnStandby`: erster Agent-Aufruf liefert Werte, nach Cache-Ablauf (`m.now` vorstellen) liefert er `{standby:true}` → `ListDisks` zeigt die alten Werte mit `Standby==true`.
- [ ] **Step 2:** Tests → FAIL.
- [ ] **Step 3:** Umsetzen wie oben.
- [ ] **Step 4:** `go test ./internal/agent/ ./internal/storage/` → PASS.
- [ ] **Step 5: Commit** `feat(smart): standby-safe queries, usb fallback, sector counts`

### Task 4: Monitor

**Files:** Create `backend/internal/monitor/monitor.go`, `monitor_test.go`

**Interfaces:**
- Consumes: `storage.Manager` über ein schmales Interface `Source { ListDisks(ctx) ([]storage.DiskView, error); ListPools(ctx) ([]storage.PoolView, error); SnapraidStatus(ctx, pool string) (snapraid.Status, error) }`; `notify.Service` über `Notifier { Send(ctx, notify.Message) []notify.Result }`; `notify.LoadConfig` (Schwelle).
- Produces:
  ```go
  type Finding struct { Key string; Level notify.Level; Title, Body string }
  type Active struct { Key string; Level notify.Level; Title, Body string; Since time.Time }  // JSON-Tags key, level, title, body, since
  type Monitor struct{ … }
  func New(db *sql.DB, src Source, n Notifier, hub *events.Hub) *Monitor
  func (m *Monitor) Findings(ctx context.Context) ([]Finding, error)  // reine Ableitung
  func (m *Monitor) Check(ctx context.Context) error                   // Findings + Abgleich mit `alerts`, Senden, Event "alerts.changed"
  func (m *Monitor) Active(ctx context.Context) ([]Active, error)
  func (m *Monitor) Run(ctx context.Context)                           // erster Check nach 30 s, dann alle 5 min; Check-Timeout 1 min
  ```
- Befund-Schlüssel und Stufen: `smart:<diskId>` crit bei `Passed==false` (nur wenn nicht Standby ohne Werte); `smart-sectors:<diskId>` warn bei `PendingSectors>0 || ReallocatedSectors>0`; `temp:<diskId>` warn bei `TemperatureC >= TempWarnC` und `TemperatureC > 0`; `disk-missing:<diskId>` crit bei `State=="missing"`; `pool-unmounted:<name>` crit bei `!Mounted`; `snapraid-sync:<pool>` crit bei `LastSync.Result` in `failed|blocked`, `snapraid-scrub:<pool>` crit bei `LastScrub.Result` in `failed|errors` (nur Pools mit `Enabled`). Titel/Text deutsch mit Plattenmodell/Seriennummer bzw. Poolname und Meldungstext des Laufs.

- [ ] **Step 1: Tests** (Fakes für `Source`/`Notifier`, In-Memory-DB)
  - `TestFindingsFromDisksPoolsSnapraid`: ein Fake-Zustand mit je einem Auslöser → erwartete Schlüssel, Stufen; gesunder Zustand → leer.
  - `TestTemperatureZeroNeverAlerts`, `TestStandbyWithoutValuesNoSmartAlert`.
  - `TestCheckSendsOnceAndRecoversOnce`: zwei Läufe mit gleichem Befund → 1 `Send`; danach Befund weg → 1 `Send` mit Titel beginnend „Behoben:“ und Stufe `info`; `Active` leer.
  - `TestCheckPersistsAcrossRestart`: neuer `Monitor` auf derselben DB, gleicher Befund → kein erneutes `Send`.
  - `TestCheckSourceErrorKeepsAlerts`: `ListDisks` liefert Fehler → Fehler zurück, bestehende Alerts bleiben, nichts wird „behoben“ gemeldet.
  - `TestCheckPublishesEvent`: `alerts.changed` auf dem Hub, wenn sich die Menge ändert.
- [ ] **Step 2:** Tests → FAIL.
- [ ] **Step 3:** Implementieren; bei Quellfehler eines Teils (z. B. SnapRAID-Status eines Pools) nur diesen Teil überspringen und dessen Schlüssel nicht als „behoben“ werten.
- [ ] **Step 4:** `go test ./internal/monitor/` → PASS.
- [ ] **Step 5: Commit** `feat(monitor): alert detection with dedup and recovery`

### Task 5: Spindown

**Files:** Create `backend/internal/storage/spindown.go`, `spindown_test.go`, `backend/internal/agent/spindown.go`, `spindown_test.go`; Modify `backend/internal/agent/server.go` (`registerSpindown(s, d)`), `backend/internal/storage/manager.go` (`DiskView.Spindown int json:"spindown"`, `Standby` kommt aus `Smart`), `packaging/build-deb.sh` (`hdparm` in Depends), `image/packages/rootfs.list` (`hdparm`), Startpfad `Reconcile` in `cmd/betternas-api/main.go` (Schritt „spindown“ nach „mount-units“)

**Interfaces:**
- Produces:
  ```go
  // storage
  var SpindownChoices = []int{0, 5, 10, 20, 30, 60, 120, 240}
  func SpindownValue(minutes int) (int, error)   // 0→0; ≤20 → minutes*12; ≥30 → 240+minutes/30; sonst apierr invalid
  func (m *Manager) Spindown(ctx) (map[string]int, error)                 // diskId → Minuten
  func (m *Manager) SetSpindown(ctx context.Context, actor, diskID string, minutes int) error
  func (m *Manager) ReconcileSpindown(ctx context.Context) error          // wendet gespeicherte Werte auf online-Platten an; Fehler einzelner Platten nur loggen
  // agent: RPC "disk.spindown" {device string, value int}; Runner: `hdparm -S <value> <device>`
  ```
- Regeln: nur Platten der Rolle `data`/`parity` und `Manageable`; Systemplatte und unzugeordnete → `conflict`. Agent prüft das Gerät gegen `ListDisks` wie `Smart` und `value` ∈ 0..251. Schlägt `hdparm` fehl (Exit ≠ 0, Ausgabe „bad/missing sense data“, „HDIO“, „not supported“) → `conflict` „Diese Platte unterstützt keinen Ruhezustand (häufig bei USB-Gehäusen).“; dann wird **nichts gespeichert**. `SetSpindown(…, 0)` setzt `-S 0` und entfernt den Eintrag. Reconcile läuft zusätzlich bei `disks.changed` (Hub-Abo in `main.go`, entprellt 5 s).

- [ ] **Step 1: Tests**
  - `TestSpindownValue`: `{0:0, 5:60, 10:120, 20:240, 30:241, 60:242, 120:244, 240:248}`; 7, 15, 300, −1 → `invalid`.
  - Agent: `TestSpindownRunsHdparm` (erwarteter Aufruf `hdparm -S 242 /dev/sdb`), `TestSpindownUnknownDevice` (`not_found`), `TestSpindownUnsupportedMapsToConflict`.
  - Manager: `TestSetSpindownSavesOnlyOnSuccess` (Agent-Fehler → Einstellung unverändert), `TestSetSpindownRejectsSystemDisk`, `TestReconcileSpindownAppliesAndSkipsMissing` (fehlende Platte wird übersprungen, Fehler einer Platte stoppt die anderen nicht).
- [ ] **Step 2:** Tests → FAIL.
- [ ] **Step 3:** Umsetzen; Einstellung als `settings`-Zeile `power.spindown` (JSON-Map).
- [ ] **Step 4:** `go test ./internal/storage/ ./internal/agent/` → PASS.
- [ ] **Step 5: Commit** `feat(storage): per-disk hdparm spindown`

### Task 6: API-Routen und Verdrahtung

**Files:** Create `backend/internal/api/notifications.go`, `notifications_api_test.go`; Modify `backend/internal/api/server.go` (`Deps.Notify *notify.Service`, `Deps.Monitor *monitor.Monitor`, `registerNotifications`), `backend/internal/api/storage.go` (Spindown-Route), `backend/cmd/betternas-api/main.go`

**Routen** (Admin-Sitzung wie alle anderen, CSRF wie bisher; Audit bei Schreibzugriffen):
```
GET  /api/v1/notifications/config        → PublicConfig
PUT  /api/v1/notifications/config        Body Config → PublicConfig   (Audit: notifications.config)
POST /api/v1/notifications/test          Body {"channel":"mail"|"ntfy"} → 204 | Fehler  (Audit: notifications.test)
GET  /api/v1/notifications/history       ?limit=50 → []HistoryEntry
GET  /api/v1/alerts                      → []Active
PUT  /api/v1/disks/{id}/spindown         Body {"minutes":int} → 204   (Audit: disk.spindown)
GET  /api/v1/power/spindown-choices      → {"choices":[0,5,…]}        (optional – nur wenn die GUI sie nicht fest hat; sonst weglassen)
```
- `main.go`: `notify.NewService(d, hub)`, `monitor.New(d, sm, ns, hub)`, `go mon.Run(ctx)`, Spindown-Reconcile-Schritt und `disks.changed`-Abo (Task 5).

- [ ] **Step 1: Tests** (Muster `snapraid_api_test.go`/`vms_api_test.go`)
  - `TestNotificationsConfigNeverLeaksSecrets`: PUT mit Passwort/Token, GET-Body enthält die Geheimnisse nicht, `passwordSet:true`.
  - `TestNotificationsPutKeepsSecretWhenEmpty`.
  - `TestNotificationsTestRoute`: Fake-ntfy-Server → 204; deaktivierter Kanal → 409 mit deutscher Meldung.
  - `TestHistoryAndAlertsRoutes`: Einträge erscheinen, `limit` wird auf 1..200 begrenzt.
  - `TestSpindownRoute`: gültig → 204 + Audit-Eintrag; `minutes:7` → 400; Systemplatte → 409.
  - Ohne Sitzung → 401 (wie die übrigen Routen; ein Test genügt).
- [ ] **Step 2:** Tests → FAIL.
- [ ] **Step 3:** Implementieren, `go vet ./...`.
- [ ] **Step 4:** `go test ./...` im `backend/` → PASS.
- [ ] **Step 5: Commit** `feat(api): notification, alert and spindown routes`

### Task 7: GUI

**Files:** Create `frontend/src/api/notifications.ts`, `frontend/src/stores/notifications.ts`, `frontend/src/views/system/NotificationsView.vue`, `frontend/src/__tests__/notifications-view.test.ts`; Modify `frontend/src/router.ts` (Route `system/benachrichtigungen`, Titel „Benachrichtigungen“), `frontend/src/components/AppLayout.vue` (Dock-Eintrag `pi pi-bell`, Farbe `['#eab308','#facc15']`), `frontend/src/api/storage.ts` (`Smart`: `standby`, `reallocatedSectors`, `pendingSectors`; `DiskView.spindown`; `setSpindown`), `frontend/src/views/storage/DisksView.vue`, `frontend/src/views/DashboardView.vue`

**Verhalten:**
- **Benachrichtigungen-Seite** (Muster `UpdatesView.vue`): Abschnitte *Aktive Meldungen* (Liste mit Stufe, Titel, seit wann), *Mail* (Schalter, Server, Port, Sicherheit, Benutzer, Passwort mit Platzhalter „gespeichert“ wenn `passwordSet`, Absender, Empfänger als kommagetrennte Liste), *ntfy* (Schalter, Server, Topic, Token), *Schwellwert* (Temperatur °C), Speichern-Knopf, je Kanal „Testnachricht senden“ (zeigt Erfolg/Fehlertext als Toast, nutzt gespeicherte Werte – Knopf deaktiviert bei ungespeicherten Änderungen), *Verlauf* (Tabelle: Zeit, Stufe, Titel, Ergebnis je Kanal). Aktualisiert sich bei WebSocket-Ereignissen `alerts.changed` / `notifications.changed`.
- **DisksView:** Auswahl „Ruhezustand“ je Daten-/Paritätsplatte (Aus, 5, 10, 20, 30 min, 1, 2, 4 h), Fehlertext der API sichtbar bei Ablehnung; Etikett „Ruhezustand“ statt Temperatur/Stunden, wenn `smart.standby`; Warn-Hinweis bei `pendingSectors`/`reallocatedSectors > 0`.
- **Dashboard:** Kachel „Meldungen“ (Anzahl aktiver, rot bei `crit`, grün „Alles in Ordnung“), Link zur Benachrichtigungen-Seite; Muster der vorhandenen SnapRAID-Kachel; lädt bei `alerts.changed` neu.

- [ ] **Step 1: Tests** (Vitest; ResizeObserver-Stub existiert in `src/test/setup.ts`; SFC-Mocks mit `__esModule: true`)
  - `notifications-view.test.ts`: Passwortfeld zeigt Platzhalter statt Wert und sendet beim Speichern ohne Eingabe `password: ""`; Test-Knopf ruft `POST …/test` und zeigt den Fehlertext aus der Antwort; Verlauf rendert Kanal-Ergebnisse (ok/Fehler); aktive Meldung wird gelistet.
  - DisksView: Standby-Platte zeigt „Ruhezustand“; Änderung der Auswahl ruft `PUT …/spindown` mit `{minutes: 60}`; API-Fehler wird als Meldung gezeigt und Auswahl springt zurück.
  - Dashboard: Kachel zeigt „2 Meldungen“ bei zwei aktiven, „Alles in Ordnung“ bei null.
- [ ] **Step 2:** `cd frontend && npx vitest run` → neue Tests FAIL.
- [ ] **Step 3:** Umsetzen.
- [ ] **Step 4:** `npx vitest run` und `npx vue-tsc --noEmit` (bzw. `npm run build`) → PASS. (Bekannter, seltener Flake in `docker-views.test.ts` → wiederholen.)
- [ ] **Step 5: Commit** `feat(web): notifications page, spindown and alert tile`

### Task 8: Integrationstest, Ausrollen, Doku

**Files:** Create `backend/internal/agent/monitoring_integration_test.go` (Build-Tag wie die anderen Integrationstests); Modify `Makefile` (`make integration`-Testliste), `docs/specs/2026-11-15-betternas-design.md` (§8 „Abweichung Phase 6a“), Memory

- [ ] **Step 1:** `TestSmartAndSpindown`: auf dem Testgerät `Smart` für die USB-Platte aufrufen (kein Fehler dank `-d sat`-Fallback, oder verständlicher Fehler) und `disk.spindown` mit `value 0` (setzt nichts Gefährliches); Ergebnis `conflict` bei Nichtunterstützung gilt als bestanden.
- [ ] **Step 2:** `make build web deb VERSION=0.5.0-rc1 && make deploy VERSION=0.5.0-rc1`; `make integration` → PASS.
- [ ] **Step 3:** Auf dem Testgerät Testnachricht an ein ntfy-Topic des Nutzers senden **nur nach dessen Freigabe** (Topic/Mail-Daten gehören dem Nutzer; Claude trägt nichts ein, GUI-Prüfung macht der Nutzer).
- [ ] **Step 4:** Spec §8 und Memory (`betternas-status.md`) aktualisieren; Go + Vitest abschließend grün; Commit `docs: phase 6a notes`.
