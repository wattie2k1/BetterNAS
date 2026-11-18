# Update-Verteilung über GitHub Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ein Tag (`vX.Y.Z` / `vX.Y.Z-rcN`) baut, signiert und veröffentlicht `betternas` als APT-Update (Kanäle `stable`/`testing`) über GitHub Pages; Nutzer wechseln den Kanal in der GUI.

**Architecture:** `tools/apt-repo.sh` bekommt einen nativen Modus mit Suiten, Aufräumen und Mehrfach-Signatur. Ein GitHub-Actions-Workflow (test → build → publish mit geschütztem Environment `release`) benutzt ihn und pusht einen Orphan-Commit nach `gh-pages`. Auf dem Gerät schreibt ein neuer typisierter Agent-RPC das `Suites:`-Feld der eigenen Quelldatei; API und Update-Seite bekommen einen Kanalumschalter.

**Tech Stack:** POSIX-sh, `apt-ftparchive`, `gpg`, GitHub Actions, Go 1.24 (Agent/API), Vue 3 + PrimeVue + Vitest.

**Spec:** `docs/superpowers/specs/2026-11-18-update-distribution-design.md`

## Global Constraints

- Repo `wattie2k1/BetterNAS`; `APT_URL` = `https://wattie2k1.github.io/BetterNAS`. Go-Modulpfad bleibt `github.com/betternas/betternas`.
- Tag-Schema `vX.Y.Z` und `vX.Y.Z-rcN`; Paketversion = Tag ohne `v`, `-` → `~` (`v0.6.0-rc1` → `0.6.0~rc1`).
- Suiten `stable` (Standard) und `testing`; `stable`-Release landet in beiden, `-rc` nur in `testing`. Pro Suite bleiben die letzten 5 Versionen (`BNAS_KEEP`, Standard 5). `gh-pages` wird bei jedem Release als Orphan-Commit force-gepusht.
- Signierschlüssel nur im Environment `release` (Secret `APT_SIGNING_KEY`, Pflicht-Reviewer), nie in Pull-Request-Jobs; `permissions:` minimal je Job; Actions auf Commit-SHA gepinnt.
- Kein ARM64, kein ISO-Bau in Actions.
- Alle GUI-Texte, Fehlermeldungen und Skriptausgaben deutsch. Neue Agent-Operationen: API validiert → typisierter Agent-RPC → Audit. Fremde (nicht von BetterNAS verwaltete) Dateien werden nie überschrieben.
- Lizenz GPL-3.0-or-later; Beiträge nur mit Contributor-Hinweis.
- Keine Commits mit Attributionszeilen. Schritte, die etwas auf GitHub anlegen oder pushen (Task 10), erst nach ausdrücklichem OK des Nutzers.

## Review Focus

1. Tag `v0.6.0-rc1` → Paketversion `0.6.0~rc1`, die in `dpkg --compare-versions` **unter** `0.6.0` liegt (Task 2).
2. Tag, der nicht dem Schema entspricht (`v1.2`, `vfoo`, `v1.2.3-beta1`): Workflow bricht vor dem Publish ab, nichts wird veröffentlicht (Tasks 2, 8).
3. Erstes Release, `gh-pages` existiert noch nicht: Repo wird von Grund auf erzeugt, nicht abgebrochen (Task 3).
4. Zwei Tags kurz hintereinander: Publish-Läufe werden serialisiert, kein verlorenes Release (Task 8).
5. Hand editierte oder fehlende Paketquelle sowie ein Kanalwert mit Zeilenumbruch (`"stable\nURIs: x"`): Kanalwechsel wird abgelehnt, Datei bleibt unverändert (Task 4).
6. Leeres/fehlendes Secret `APT_SIGNING_KEY`: Publish schlägt fehl statt unsigniert zu veröffentlichen (Tasks 3, 8).

---

### Task 1: Lizenz und Contributor-Hinweis

**Files:**
- Create: `LICENSE`, `CONTRIBUTING.md`
- Modify: `README.md` (Abschnitt „Lizenz")

**Interfaces:**
- Produces: nichts, das spätere Tasks verwenden.

- [ ] **Step 1: `LICENSE`** mit dem unveränderten Originaltext der GPL-3.0 anlegen (kanonische Quelle `https://www.gnu.org/licenses/gpl-3.0.txt`; Download vorher mit dem Nutzer abstimmen).
- [ ] **Step 2: `CONTRIBUTING.md`** (deutsch): Beiträge nur mit Signed-off-by-Zeile (DCO) und der Zusage, dass der Kern GPL-3.0-or-later bleibt, der Maintainer aber zusätzlich nicht öffentliche Zusatzmodule unter anderer Lizenz anbieten darf; Beitragende stimmen zu, dass ihr Beitrag dafür unter diesen Bedingungen nutzbar ist.
- [ ] **Step 3: `README.md`:** Abschnitt „Lizenz" (GPL-3.0-or-later, Verweis auf `LICENSE` und `CONTRIBUTING.md`).
- [ ] **Step 4: Prüfen:** `head -n 3 LICENSE` zeigt „GNU GENERAL PUBLIC LICENSE / Version 3, 29 June 2007".
- [ ] **Step 5: Commit:** `git add LICENSE CONTRIBUTING.md README.md && git commit -m "docs: GPL-3.0-or-later und Contributor-Hinweis"`

---

### Task 2: Paketversion aus dem Tag

**Files:**
- Create: `tools/deb-version.sh`, `tools/tests/deb-version.test.sh`, `tools/tests/run.sh`
- Modify: `Makefile` (Ziel `test-tools`, in `test` aufnehmen)

**Interfaces:**
- Produces: `tools/deb-version.sh <tag>` → stdout `<debversion> <channel>` (`channel` = `stable` oder `testing`), Exit 1 mit deutscher Meldung auf stderr bei ungültigem Tag. Erlaubt: `^v[0-9]+\.[0-9]+\.[0-9]+(-rc[0-9]+)?$`.
- Produces: `tools/tests/run.sh` führt alle `tools/tests/*.test.sh` aus (Task 3 hängt seinen Test dort ein). `make test-tools`: auf Linux direkt, sonst im Builder-Container (`$(DOCKER_RUN) tools/tests/run.sh`).

- [ ] **Step 1: Failing test** `tools/tests/deb-version.test.sh` mit Assertions:
  - `deb-version.sh v0.6.0` → `0.6.0 stable`
  - `deb-version.sh v0.6.0-rc1` → `0.6.0~rc1 testing`
  - `deb-version.sh v1.2`, `vfoo`, `v1.2.3-beta1`, `0.6.0`, `v1.2.3-rc` → Exit ≠ 0
  - wenn `dpkg` vorhanden: `dpkg --compare-versions 0.6.0~rc1 lt 0.6.0` ist wahr
- [ ] **Step 2: Test ausführen, muss scheitern:** `sh tools/tests/deb-version.test.sh` → FAIL (Skript fehlt).
- [ ] **Step 3: `tools/deb-version.sh` implementieren** (Regex-Prüfung per `case`/`expr`, Umschreibung `-rc` → `~rc`).
- [ ] **Step 4: Test ausführen, muss bestehen:** `sh tools/tests/deb-version.test.sh` → „OK".
- [ ] **Step 5: Commit:** `git add tools Makefile && git commit -m "feat(release): Paketversion und Kanal aus dem Tag ableiten"`

---

### Task 3: `apt-repo.sh` mit Suiten, Aufräumen und Mehrfach-Signatur

**Files:**
- Modify: `tools/apt-repo.sh`, `tools/dev-repo.sh` (nur falls die Aufrufform bricht)
- Create: `tools/tests/apt-repo.test.sh`

**Interfaces:**
- Consumes: `tools/tests/run.sh` aus Task 2.
- Produces: `tools/apt-repo.sh [--native] [--suite <stable|testing>]... <repo-dir> <deb>...`
  - Ohne `--suite`: beide Suiten (rückwärtskompatibel zu `make repo`/`dev-repo`).
  - `--native`: kein Docker, `apt-ftparchive`/`gpg` vom Host; Schlüssel aus Datei `$BNAS_APT_KEY` (Standard `~/.betternas/apt-signing-key.asc`). Ohne `--native` bleibt das bisherige Docker-Verhalten (Schlüssel über stdin).
  - Pool je Suite: `pool/<suite>/main/b/betternas/`. Pro Suite werden `dists/<suite>/main/binary-amd64|arm64/Packages(.gz)`, `Release`, `InRelease`, `Release.gpg` erzeugt (`Suite`/`Codename` = Suitenname).
  - Aufräumen: nach dem Kopieren bleiben pro Suite nur die `BNAS_KEEP` (Standard 5) höchsten Versionen (Reihenfolge per `dpkg --compare-versions`).
  - Signatur mit **allen** importierten geheimen Schlüsseln (`gpg --clearsign` / `-abs` mit je einem `--local-user` pro Schlüssel).
  - Fehlt der Schlüssel oder ist die Datei leer: Exit 1, „FEHLER: Signierschlüssel fehlt oder ist leer", kein `InRelease` geschrieben.

- [ ] **Step 1: Failing test** `tools/tests/apt-repo.test.sh` (läuft nur mit `apt-ftparchive`, `gpg`, `dpkg-deb`; sonst „übersprungen" mit Exit 0). Aufbau: zwei Wegwerf-Schlüssel (`gpg --quick-generate-key … ed25519 sign never`, eigenes `GNUPGHOME`), Dummy-`.deb`s per `dpkg-deb --build` (Paket `betternas`, Versionen `0.1.0`, `0.2.0~rc1`, `0.2.0`, …). Asserts, jeweils eigene Funktion:
  - `test_rc_goes_only_to_testing`: `--suite testing` mit `0.2.0~rc1` → Datei liegt in `pool/testing/…`, nicht in `pool/stable`; `dists/stable` fehlt oder enthält es nicht.
  - `test_stable_goes_to_both`: `--suite stable --suite testing` mit `0.2.0` → in beiden Packages-Dateien.
  - `test_first_release_from_empty_dir`: leeres, nicht existierendes Repo-Verzeichnis funktioniert.
  - `test_prune_keeps_five`: 7 Versionen nacheinander (`BNAS_KEEP` Standard) → pro Suite genau 5, die ältesten fehlen.
  - `test_signature_verifies`: `gpgv --keyring <pub.gpg> dists/stable/InRelease` und `Release.gpg`/`Release` erfolgreich.
  - `test_double_signature`: mit Schlüsseldatei aus zwei Schlüsseln verifiziert `InRelease` gegen jeden der beiden Public-Keyrings einzeln.
  - `test_missing_key_aborts`: `BNAS_APT_KEY` zeigt auf leere Datei → Exit ≠ 0, kein `InRelease`.
- [ ] **Step 2: Test ausführen, muss scheitern:** `sh tools/tests/apt-repo.test.sh` (auf macOS: `make test-tools`).
- [ ] **Step 3: `tools/apt-repo.sh` umbauen** entlang der Interface-Liste; bestehende `--inner`-Logik wird zum nativen Kern, der Docker-Pfad ruft ihn weiter über `sh tools/apt-repo.sh --inner …` im Builder-Container auf. `/tmp/Release` durch `mktemp` ersetzen.
- [ ] **Step 4: Test ausführen, muss bestehen:** alle Funktionen „OK".
- [ ] **Step 5: Regressionscheck** `make repo VERSION=<vorhandenes dist-deb>` (oder Aufruf ohne `--suite`) erzeugt `dists/stable` und `dists/testing`; `tools/dev-repo.sh` bleibt aufrufkompatibel (Quelle zeigt weiter auf `Suites: stable`).
- [ ] **Step 6: Commit:** `git add tools && git commit -m "feat(apt): Suiten stable/testing, Aufräumen, Mehrfach-Signatur, nativer Modus"`

---

### Task 4: Agent: Update-Kanal lesen und setzen

**Files:**
- Modify: `backend/internal/agent/updates.go`, `backend/internal/agent/server.go` (neben `updates.status`, ca. Zeile 157)
- Test: `backend/internal/agent/updates_test.go` (nutzt `updatesEnv`)

**Interfaces:**
- Produces:
  - `const aptSourcesPath = "/etc/apt/sources.list.d/betternas.sources"`
  - `func UpdateChannel(ctx context.Context, d Deps) (string, error)` → `"stable"`, `"testing"` oder `""` (Datei fehlt oder `Suites:` unbekannt). Kein Fehler bei fehlender Datei.
  - `func SetUpdateChannel(ctx context.Context, d Deps, channel string) error`
    - `channel` muss exakt `stable` oder `testing` sein, sonst `apierr.InvalidParams` („Ungültiger Update-Kanal").
    - Datei fehlt → `apierr.NotFound` („Keine BetterNAS-Paketquelle eingerichtet.").
    - Datei muss als von BetterNAS verwaltet erkennbar sein: enthält die Zeile `# Verwaltet von BetterNAS – nicht manuell bearbeiten` **oder** `Signed-By: /usr/share/keyrings/betternas-archive-keyring.gpg`; sonst `apierr.Conflict` („Die BetterNAS-Paketquelle wurde manuell angepasst."), Datei unverändert.
    - Ersetzt nur die `Suites:`-Zeile, schreibt atomar (`d.FS.WriteFile`, Rechte 0644), alles andere bleibt byte-gleich; fügt den Header ein, falls er fehlt.
  - RPC-Methoden `updates.channel` (→ `{"channel": string}`) und `updates.setchannel` (Params `{"channel": string}` → `{}`), über `decode` wie die Nachbarn; `setchannel` unter dem vorhandenen `mutate`-Lock nur, falls ein solches im Umfeld gilt (sonst ohne).

- [ ] **Step 1: Failing tests** in `updates_test.go`:
  - `TestUpdateChannelReadsSuites`: Datei mit `Suites: testing` → `"testing"`; Datei fehlt → `""`.
  - `TestSetUpdateChannelRewritesOnlySuites`: verwaltete Datei (Header) mit `Suites: stable` → nach `testing` ist der Inhalt gleich dem Original bis auf diese eine Zeile.
  - `TestSetUpdateChannelRejectsInvalid`: Tabelle aus `"beta"`, `""`, `"stable\nURIs: http://evil"`, `"Stable"` → je `apierr.InvalidParams`, Datei unverändert.
  - `TestSetUpdateChannelRefusesForeignFile`: Datei ohne Header und ohne BetterNAS-Keyring → `apierr.Conflict`, Datei unverändert.
  - `TestSetUpdateChannelMissingFile`: → `apierr.NotFound`.
  - `TestSetUpdateChannelIdempotent`: zweimal derselbe Kanal → kein Fehler, Datei gleich.
  - in `register_test.go`-Stil: `updates.channel` und `updates.setchannel` über `rpcClient` erreichbar.
- [ ] **Step 2: Tests ausführen, müssen scheitern:** `cd backend && go test ./internal/agent -run 'UpdateChannel' -v` → Compile-Fehler/FAIL.
- [ ] **Step 3: Implementieren** (Zeilenweises Ersetzen der `Suites:`-Zeile; Whitelist statt Escaping).
- [ ] **Step 4: Tests bestehen:** `cd backend && go vet ./... && go test ./internal/agent -v`.
- [ ] **Step 5: Commit:** `git add backend && git commit -m "feat(agent): Update-Kanal lesen und setzen"`

---

### Task 5: Update-Manager und API: Kanal

**Files:**
- Modify: `backend/internal/system/updates.go`, `backend/internal/api/updates.go`
- Test: `backend/internal/system/updates_test.go`, `backend/internal/api/updates_test.go` (die Fake-Agents um `updates.channel`/`updates.setchannel` erweitern)

**Interfaces:**
- Consumes: RPC `updates.channel`, `updates.setchannel` (Task 4).
- Produces:
  - `Overview` bekommt `Channel string \`json:"channel"\`` (`""` = keine Quelle/unbekannt). `Get` ruft `updates.channel`; schlägt der Aufruf fehl, bleibt `Channel` leer und `Get` gelingt trotzdem.
  - `func (u *Updates) SetChannel(ctx context.Context, actor, channel string) error`: validiert (`stable`/`testing`, sonst `apierr.InvalidParams`), ruft `updates.setchannel`, schreibt Audit `updates.setchannel` (Target = Kanal, Result `ok`/`error`), leert die gespeicherte Paketliste (`keyPackages` = `"[]"`, Liste ist für den alten Kanal erhoben) und publiziert `updates.changed`.
  - Route `PUT /api/v1/updates/channel`, Body `{"channel":"stable|testing"}`, Antwort 204; Fehler über `apierr.Write`.

- [ ] **Step 1: Failing tests:** 
  - `system`: `TestGetIncludesChannel` (Fake liefert `testing` → `Overview.Channel == "testing"`); `TestGetSurvivesChannelError`; `TestSetChannelAuditsAndClearsPackages`; `TestSetChannelRejectsInvalid` (Agent wird nicht aufgerufen).
  - `api`: `TestUpdatesChannelRoute` (PUT mit gültigem Body → 204 und Agent-Call; ungültiger Kanal → 400; GET `/api/v1/updates` enthält `channel`).
- [ ] **Step 2: Tests ausführen, müssen scheitern:** `cd backend && go test ./internal/system ./internal/api -run 'Channel' -v`.
- [ ] **Step 3: Implementieren** gemäß Interfaces.
- [ ] **Step 4: Gesamte Backend-Suite:** `cd backend && go vet ./... && go test ./...` → PASS.
- [ ] **Step 5: Commit:** `git add backend && git commit -m "feat(api): Update-Kanal in Übersicht und PUT /updates/channel"`

---

### Task 6: Update-Seite: Kanalumschalter

**Files:**
- Modify: `frontend/src/api/updates.ts`, `frontend/src/stores/updates.ts`, `frontend/src/views/system/UpdatesView.vue`
- Test: `frontend/src/__tests__/updates.test.ts`

**Interfaces:**
- Consumes: `GET /api/v1/updates` mit `channel`, `PUT /api/v1/updates/channel` (Task 5).
- Produces:
  - `UpdateOverview.channel: '' | 'stable' | 'testing'`; `updatesApi.setChannel(channel: 'stable' | 'testing'): Promise<void>`.
  - Store: `setChannel(channel: 'stable' | 'testing'): Promise<void>` ruft API, lädt danach `load()`; Fehler werden geworfen (View zeigt Toast wie bei `check`).
  - View: PrimeVue `SelectButton` (`data-test="updates-channel"`) mit „Stabil" / „Testing", nur sichtbar wenn `overview.channel !== ''`, deaktiviert während `store.installing`; darunter Hinweis (deutsch): „Testing enthält Vorabversionen. Ein Wechsel zurück zu Stabil installiert keine ältere Version; das NAS bleibt auf seiner Version, bis Stabil sie einholt." Nach Wechsel Toast „Update-Kanal geändert" und Hinweis, erneut nach Updates zu suchen.

- [ ] **Step 1: Failing tests** in `updates.test.ts` (Muster `script({...})`): 
  - `zeigt den Umschalter mit dem aktuellen Kanal` (`channel: 'stable'` in der Übersicht → SelectButton vorhanden, „Stabil" aktiv).
  - `blendet den Umschalter aus, wenn keine Quelle eingerichtet ist` (`channel: ''`).
  - `wechselt den Kanal per PUT und lädt neu` (PUT-Aufruf mit `{"channel":"testing"}`, danach erneutes GET).
  - `zeigt den Fehler als Toast, wenn der Wechsel scheitert` (PUT liefert 409).
- [ ] **Step 2: Tests ausführen, müssen scheitern:** `cd frontend && npx vitest run src/__tests__/updates.test.ts`.
- [ ] **Step 3: Implementieren** (alle bestehenden Fixtures `overview(...)` um `channel` ergänzen).
- [ ] **Step 4: Frontend-Suite und Typen:** `cd frontend && npm test && npx vue-tsc --noEmit` → PASS.
- [ ] **Step 5: Commit:** `git add frontend && git commit -m "feat(web): Update-Kanal auf der Update-Seite umschaltbar"`

---

### Task 7: Image und ISO-Upload

**Files:**
- Modify: `image/hooks/rootfs/40-apt.sh`, `image/tests/check-rootfs.sh`, `Makefile` (`APT_SUITE ?= stable`, an `DOCKER_RUN` durchreichen), `tools/remote-build.sh`, `README.md`
- Create: `tools/release-iso.sh` (ersetzt `tools/release.sh`, per `git mv`/`git rm`)

**Interfaces:**
- Produces:
  - `40-apt.sh` schreibt `betternas.sources` mit erster Zeile `# Verwaltet von BetterNAS – nicht manuell bearbeiten` und `Suites: ${APT_SUITE:-stable}`; ohne `APT_URL` weiterhin keine Datei.
  - `check-rootfs.sh` prüft Header, `URIs:` und `Suites:`.
  - `tools/release-iso.sh <version>`: lädt `dist/betternas-<version>-amd64.iso` plus `.sha256` per `gh release upload v<version> --repo wattie2k1/BetterNAS --clobber` an ein bestehendes Release; Fehler, wenn ISO fehlt oder das Release nicht existiert („Release v<version> existiert nicht – erst Tag pushen").

- [ ] **Step 1: Failing check:** `check-rootfs.sh` zuerst um die Header-/Suites-Prüfung erweitern; mit Docker auf einem vorhandenen Rootfs (oder nur Shell-Teil) laufen lassen → schlägt fehl (Header fehlt).
- [ ] **Step 2: `40-apt.sh` und Makefile/remote-build anpassen**; `APT_SUITE` mitreichen.
- [ ] **Step 3: Check bestehen lassen:** `make rootfs APT_URL=https://wattie2k1.github.io/BetterNAS` (auf dem ThinkCentre über `make remote`) bzw. `check-rootfs.sh` gegen frisch gebautes Rootfs → OK.
- [ ] **Step 4: `tools/release-iso.sh` schreiben; `tools/release.sh` entfernen;** README-Abschnitt „Release veröffentlichen (GitHub)" auf neuen Ablauf umschreiben (Tag pushen → Workflow; ISO nach `make remote` per `tools/release-iso.sh`).
- [ ] **Step 5: Commit:** `git add -A image Makefile tools README.md && git commit -m "feat(image): Kanal im Image, ISO-Upload statt manuellem Repo-Release"`

---

### Task 8: Release-Workflow

**Files:**
- Create: `.github/workflows/release.yml`
- Modify: `.github/workflows/ci.yml` (`make test` → zusätzlich `make test-tools` ist bereits über `test`; nur prüfen, dass `apt-utils` auf dem Runner vorhanden ist, sonst Installationsschritt)

**Interfaces:**
- Consumes: `tools/deb-version.sh` (Task 2), `tools/apt-repo.sh --native` (Task 3), `packaging/build-deb.sh <arch> <version>`, Make-Ziele `build`, `web`, `test`.
- Produces: Workflow `Release` (Trigger `push: tags: ['v*']`), Jobs:
  1. `test`: Checkout, Go 1.24, Node (Version aus `frontend/package.json` `engines`, sonst 22), `make test`.
  2. `build` (needs test): `tools/deb-version.sh "$GITHUB_REF_NAME"` → Version/Kanal als Job-Outputs (ungültiger Tag = Job schlägt fehl), `make build web`, `packaging/build-deb.sh amd64 <version>`, `.deb` als Artefakt.
  3. `publish` (needs build; `environment: release`; `concurrency: { group: release-publish, cancel-in-progress: false }`; `permissions: contents: write`): `.deb` laden, `gh-pages` auschecken (falls vorhanden, sonst leeres Verzeichnis), Schlüssel aus `secrets.APT_SIGNING_KEY` in eine 0600-Datei (Abbruch bei leerem Secret), `tools/apt-repo.sh --native` mit `--suite testing` (bei `-rc`) bzw. `--suite stable --suite testing`, Keyring-Datei und `.nojekyll` ins Repo, Orphan-Branch committen und `git push --force origin HEAD:gh-pages`, Schlüsseldatei löschen, `gh release create "$TAG" --prerelease` (bei `-rc`) `--generate-notes` mit dem `.deb`.
- Jeder `uses:` ist auf eine Commit-SHA gepinnt (mit Versionskommentar).

- [ ] **Step 1: Workflow schreiben** gemäß Interface; `permissions: {}` auf Workflow-Ebene, Jobs heben nur an, was sie brauchen.
- [ ] **Step 2: Statische Prüfung:** `docker run --rm -v "$PWD:/repo" -w /repo rhysd/actionlint:latest` → keine Fehler; `grep -nE 'uses: [^@]+@[^0-9a-f]' .github/workflows/release.yml` findet keine Tag-/Branch-Referenzen (nur SHAs).
- [ ] **Step 3: Skript-Trockenlauf** der Publish-Schritte lokal im Builder/Linux gegen ein Temp-Repo mit Wegwerf-Schlüssel (nutzt den Test aus Task 3) – Reihenfolge Suite-Auswahl für `v0.4.0-rc1` und `v0.4.0` per Shell-Snippet bestätigen.
- [ ] **Step 4: Commit:** `git add .github && git commit -m "ci: Release-Workflow (Tag → test → build → signieren → Pages + Release)"`

---

### Task 9: Schlüssel-Rotation und Dokumentation

**Files:**
- Modify: `packaging/apt/betternas-archive-keyring.asc`, `packaging/apt/betternas-archive-keyring.gpg` (neu erzeugt), `README.md` (Abschnitt „Updates und APT-Repository"), `tools/apt-key.sh` (Hinweistext)

**Interfaces:**
- Consumes: `tools/apt-key.sh` (verweigert Überschreiben eines vorhandenen Schlüssels), `packaging/build-deb.sh` (installiert `betternas-archive-keyring.gpg`; eine Keyring-Datei darf mehrere Schlüssel enthalten).

- [ ] **Step 1 (Nutzer-Schritt, vorher bestätigen lassen):** alten privaten Schlüssel sichern/verschieben: `mv ~/.betternas/apt-signing-key.asc ~/.betternas/apt-signing-key.old.asc`.
- [ ] **Step 2: Neuen Schlüssel erzeugen:** `make apt-key` → neue `betternas-archive-keyring.{asc,gpg}`; `gpg --show-keys packaging/apt/betternas-archive-keyring.asc` zeigt genau einen neuen Ed25519-Schlüssel mit anderem Fingerprint als `git show HEAD:packaging/apt/betternas-archive-keyring.asc`.
- [ ] **Step 3: Offline-Kopie** des neuen privaten Schlüssels im Passwortmanager ablegen (Nutzer); danach den alten Schlüssel löschen: `rm ~/.betternas/apt-signing-key.old.asc` (erst nach Bestätigung des Nutzers).
- [ ] **Step 4: README** (deutsch) um zwei Teile erweitern: **Kanäle** (stable/testing, Tag-Schema, `~`-Regel, Umschalter in der GUI, kein automatischer Downgrade) und **Schlüssel-Rotation** (Überlappungsverfahren: Release N liefert beide öffentlichen Schlüssel im Keyring und signiert mit dem alten; N+1 signiert mit beiden – `apt-repo.sh` signiert mit allen Schlüsseln der Datei; N+2 entfernt den alten aus dem Keyring). Den Abschnitt über den lokalen Schlüssel auf „Secret `APT_SIGNING_KEY` im Environment `release` + Offline-Kopie" aktualisieren.
- [ ] **Step 5: Verifikation:** `make build web deb VERSION=0.0.0-keytest` baut; `dpkg-deb -c dist/betternas_0.0.0-keytest_amd64.deb | grep archive-keyring` zeigt den Schlüsselbund; `gpg --show-keys --with-colons packaging/apt/betternas-archive-keyring.gpg | grep ^fpr` stimmt mit dem neuen Fingerprint überein.
- [ ] **Step 6: Commit:** `git add packaging README.md tools && git commit -m "feat(apt): neuer Signierschlüssel, Kanal- und Rotationsdoku"`

---

### Task 10: GitHub einrichten und ersten Release-Candidate ausrollen (mit Nutzer-OK je Schritt)

**Files:** keine (GitHub-Einstellungen) · danach evtl. Korrekturen aus dem Probelauf

**Interfaces:**
- Consumes: alle vorherigen Tasks, Schlüssel aus Task 9 unter `~/.betternas/apt-signing-key.asc`.

- [ ] **Step 1: Repo anlegen und pushen** (öffentlich, **erst nach ausdrücklichem OK**): `gh repo create wattie2k1/BetterNAS --public --source . --push`.
- [ ] **Step 2: Pages aktivieren:** Quelle Branch `gh-pages` (Branch existiert nach dem ersten Publish; sonst nach Step 6 setzen).
- [ ] **Step 3: Environment `release`:** Pflicht-Reviewer = `wattie2k1`, Deployment nur für Tags `v*`; Secret setzen: `gh secret set APT_SIGNING_KEY --env release --repo wattie2k1/BetterNAS < ~/.betternas/apt-signing-key.asc`.
- [ ] **Step 4: Schutz:** Ruleset „Tags `v*` nur durch Maintainer" und Branch-Schutz für `main` (Web-UI oder `gh api`).
- [ ] **Step 5: Probelauf:** `git tag v0.4.0-rc1 && git push origin v0.4.0-rc1`; Workflow-Freigabe im Environment erteilen. Erwartung: Jobs grün, Pre-release `v0.4.0-rc1` mit `.deb`, `https://wattie2k1.github.io/BetterNAS/dists/testing/InRelease` ist erreichbar, `.../dists/stable/InRelease` enthält das Paket noch nicht.
- [ ] **Step 6: Verifikation vom Gerät aus** (ThinkCentre/Testgerät, einmalig manuell): neues `.deb` per `dpkg -i`, `/etc/apt/sources.list.d/betternas.sources` mit Header, `URIs: https://wattie2k1.github.io/BetterNAS`, `Suites: testing` anlegen, `sudo apt-get update && apt-cache policy betternas` → Kandidat `0.4.0~rc1`; in der GUI „Nach Updates suchen" zeigt die Quelle, der Kanalumschalter steht auf „Testing".
- [ ] **Step 7: Stabiles Release:** `git tag v0.4.0 && git push origin v0.4.0` → beide Suiten enthalten `0.4.0`; Testgerät sieht `0.4.0` als Update über `0.4.0~rc1`; ISO nach `make remote … APT_URL=https://wattie2k1.github.io/BetterNAS` mit `tools/release-iso.sh 0.4.0` anhängen.
