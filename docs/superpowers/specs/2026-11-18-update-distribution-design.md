# Update-Verteilung über GitHub (Design)

Stand: 2026-11-18 · Status: umgesetzt und in Betrieb (Releases v0.7.0-rc1, v0.7.0 und v0.7.1-rc1 veröffentlicht; Abschnitt „Umsetzungsstand“ am Ende)

## Intention

Alle BetterNAS-Nutzer sollen Updates des Pakets `betternas` über die Weboberfläche (System → Updates) bekommen.
Der Code liegt öffentlich auf GitHub, die Auslieferung läuft komplett über GitHub (Pages + Releases). Ein Release
entsteht durch das Setzen eines Tags und nicht mehr durch ein Skript auf dem Entwicklungsrechner.

Ausgangslage (Entwurfszeitpunkt): signiertes APT-Repo (`tools/apt-repo.sh`), Update-Seite mit `apt-get dist-upgrade`
in eigener systemd-Unit, APT-Quelle im Image nur bei gesetztem `APT_URL`, manuelles `tools/release.sh` (Pages + Release).
Es gab noch kein GitHub-Remote und nie ein Release. Heutiger Stand: siehe „Umsetzungsstand“.

## Ziele

- Release per Tag (`vX.Y.Z`, `vX.Y.Z-rcN`): GitHub Actions testet, baut das `.deb`, signiert und veröffentlicht.
- Zwei Kanäle: `stable` (Standard) und `testing` (Release-Candidates). Nutzer wechseln den Kanal in der GUI.
- Neuer Signierschlüssel, bevor irgendetwas veröffentlicht wird; dokumentierte Rotation für später.
- Reproduzierbar: jedes veröffentlichte `.deb` stammt aus einem getaggten Commit mit grünen Tests.

## Nicht-Ziele

- ARM64-Pakete (die Pipeline wird so gebaut, dass eine Architektur-Matrix später dazukommt).
- ISO-Bau in GitHub Actions: bleibt auf dem ThinkCentre (`make remote`), Upload per `gh release upload`.
- Automatisches, unbeaufsichtigtes Einspielen von Updates auf den Geräten; Release-Notes in der GUI.
- Eigene Domain; Pages-URL reicht vorerst.

## Anti-Ziele

- Ein Update darf nie ohne gültige Signatur von einem vertrauten Schlüssel auf einem Gerät landen.
- Ein Pull-Request (auch aus einem Fork) darf nie an den Signierschlüssel kommen.
- Rotation darf kein Gerät zurücklassen, das keine Updates mehr annimmt.

## Randbedingungen

- Debian-Versionsreihenfolge: `0.6.0-rc1` sortiert **über** `0.6.0` (alles nach dem Bindestrich ist die
  Debian-Revision). Deshalb wird die Tag-Version fürs Paket umgeschrieben: `v0.6.0-rc1` → `0.6.0~rc1`.
  Das bereits installierte `0.3.0-rc3` auf dem Testgerät muss einmalig manuell abgelöst werden.
- Pages-Grenzen: 1 GB Größe, 100 GB Traffic/Monat (weich). Das ISO liegt deshalb nur als Release-Asset vor.
- Alle Texte der GUI deutsch; neue Agent-Operationen folgen dem Muster API validiert → typisierter Agent-RPC →
  Renderer mit Rollback → Audit.

## Ansätze (Wahl: B)

- **A, manuell** (`tools/release.sh` wie bisher): einfach, Schlüssel nur lokal; Release hängt am Entwicklungsrechner,
  Herkunft der Binaries nicht nachvollziehbar.
- **B, Actions baut und veröffentlicht** *(gewählt)*: Tag → Tests → `.deb` → signiertes Repo auf `gh-pages` → Release.
  Schlüssel als Secret in geschütztem Environment. Restrisiko: wer Konto/Repo übernimmt, kann Updates signieren.
- **C, Actions baut, lokal signiert**: Schlüssel bleibt lokal; zweistufig und weiter manuell.

## Design

### Kanäle im Repo

Ein APT-Repo, zwei Suiten (`dists/stable`, `dists/testing`), gemeinsamer `pool/`.

- Tag ohne Suffix (`v0.6.0`) → Paket in `stable` **und** `testing`, damit `testing`-Geräte nie hinter `stable` zurückfallen.
- Tag mit `-rc` (`v0.6.0-rc1`) → nur `testing`; GitHub-Release als Pre-release.
- Es bleiben pro Suite die letzten 5 Versionen im Pool; `gh-pages` wird bei jedem Release als Orphan-Commit
  force-gepusht (keine wachsende Historie).

### Workflow `.github/workflows/release.yml` (Trigger: Tag `v*`)

1. **test**: `go vet`, `go test ./...`, Frontend-Tests. Ohne Erfolg kein Publish.
2. **build**: Go-Binaries (amd64, `CGO_ENABLED=0`, `-X …version.Version`), Frontend, `packaging/build-deb.sh`.
   Version = Tag ohne `v`, `-` → `~`. Artefakt: `.deb`.
3. **publish** (`environment: release`, Pflicht-Reviewer = Maintainer, nur für Tags freigegeben):
   aktuelles `gh-pages` holen → `.deb` in den Pool → Suiten neu erzeugen und signieren → Orphan-Push →
   GitHub-Release (`--prerelease` bei `-rc`) mit `.deb` und generierten Notes.

`tools/apt-repo.sh` bekommt einen nativen Modus (ohne Docker-Builder, `apt-ftparchive` und `gpg` sind auf dem Runner),
unterstützt `--suite` (stable/testing) und das Aufräumen alter Versionen. Lokal weiter nutzbar (`make repo`, `make dev-repo`).

Härtung: `permissions:` minimal je Job (nur `publish` bekommt `contents: write`), Actions auf Commit-SHA gepinnt,
Ruleset: nur Maintainer dürfen `v*`-Tags anlegen, `main` geschützt. Das Secret `APT_SIGNING_KEY` existiert nur im Environment `release`.

### Gerät: Kanalwahl

- Die Quelle `/etc/apt/sources.list.d/betternas.sources` (Header „Verwaltet von BetterNAS") steht auf `Suites: stable`.
- System → Updates bekommt einen Umschalter Kanal `stable`/`testing`. API validiert, ein typisierter Agent-RPC
  `update.setchannel` schreibt nur das `Suites:`-Feld (atomar, nur in unserer eigenen Datei), Audit-Eintrag.
- `testing` → `stable` führt nicht zu einem Downgrade; das Gerät bleibt auf seiner Version, bis `stable` sie einholt.
  Die GUI weist darauf hin.

### Schlüssel-Rotation

- **Jetzt** (vor dem ersten Release, es hat noch kein Fremdgerät den alten Schlüssel): neues Ed25519-Paar mit
  `make apt-key`, neuer öffentlicher Schlüssel ins Repo und ins `.deb`, privater Schlüssel als Environment-Secret
  und als verschlüsselte Offline-Kopie (Passwortmanager). Der alte private Schlüssel wird gelöscht.
  Eine Passphrase auf dem CI-Schlüssel hätte keinen Nutzen (läge im selben Secret); der Schutz ist das
  Environment mit Pflicht-Freigabe.
- **Später** (Überlappung, kein Gerät bleibt zurück): Das Keyring-Paket enthält alte und neue öffentliche Schlüssel.
  Release N liefert beide aus, signiert noch mit dem alten; ab Release N+1 signiert `InRelease` mit beiden,
  Release N+2 entfernt den alten Schlüssel aus dem Keyring. Ablauf kommt in die README.
- Das Testgerät (damals `0.3.0-rc3`, altes Keyring, vermutlich ohne Quelle) bekam das neue `.deb` einmalig per `dpkg -i`
  und die Quelle von Hand (erledigt, es läuft inzwischen eine 0.7.x-Version).

### Image

`APT_URL` bleibt Build-Parameter und wird für Release-ISOs auf die Pages-URL gesetzt. Neu: `APT_SUITE` (Standard
`stable`) für den Hook `40-apt.sh`.

## Dem Bauenden überlassen

- genaue Aufteilung/Namen der Workflow-Schritte und Caching (Go-Modul-, npm-Cache)
- Testabdeckung des geänderten `apt-repo.sh` (z. B. Shell-Test gegen ein Temp-Repo mit `apt-get update` per `file:`-Quelle)
- Wortlaut der GUI-Texte

## Meine Entscheidungen (zum Gegenprüfen)

- Pool je Suite (`pool/stable`, `pool/testing`) statt gemeinsamem Pool: `apt-ftparchive` trennt Suiten sonst nicht
  sauber; ein `stable`-Paket liegt dafür doppelt (geringe Mehrkosten bei 5 Versionen).
- 5 behaltene Versionen pro Suite; Orphan-Push auf `gh-pages`.
- Tag-Schema `vX.Y.Z` / `vX.Y.Z-rcN`, Umschreibung `-` → `~` fürs Paket.
- `stable`-Releases landen auch in `testing`.
- Pflicht-Freigabe im Environment `release` statt Schlüssel-Passphrase.
- Kanalumschalter in der GUI als Teil dieses Vorhabens (nicht nur manuelles Editieren der Quelle).

## Festgelegt

- Repo: `wattie2k1/BetterNAS`; APT-Quelle (`APT_URL`): `https://wattie2k1.github.io/BetterNAS`.
  Der Go-Modulpfad `github.com/betternas/betternas` bleibt unverändert (muss nicht zur Repo-URL passen).
  Ein späterer Umzug (z. B. eigene Domain) braucht ein Update, das die Quelle umschreibt.

- Lizenz: **GPL-3.0-or-later** für den Kern (Open Core). Spenden-Optionen später als separates, nicht öffentliches
  Zusatzmodul mit Lizenzschlüssel. `LICENSE` (GPL-3.0-Text) liegt im ersten öffentlichen Commit; die README nennt die
  Lizenz. Fremde Beiträge erst mit Contributor-Hinweis (DCO/CLA) annehmen, damit eine Umlizenzierung möglich bleibt
  (`CONTRIBUTING.md` mit diesem Hinweis gehört zum Umfang).

## Offen

Nichts aus dem ursprünglichen Umfang. Kleinigkeiten stehen unten unter „Umsetzungsstand“.

## Umsetzungsstand (geprüft am 2026-11-18)

Umgesetzt, wie oben beschrieben:

- **Workflow** `.github/workflows/release.yml` (Tag `v*`): Jobs `meta` (Tag prüfen, Paketversion und Kanal über
  `tools/deb-version.sh` ableiten, ungültige Tags brechen vor dem Bauen ab), `test`, `build` (nur amd64), `publish`
  (Environment `release`, `concurrency` ohne Abbruch laufender Veröffentlichungen). Alle Actions sind auf Commit-SHA
  gepinnt, `permissions` ist minimal, der Push auf `gh-pages` läuft über `tools/publish-pages.sh`.
- **Repo** `https://wattie2k1.github.io/BetterNAS`: Suiten `stable` und `testing` mit `InRelease` und `Release.gpg`,
  Pool je Suite, 5 Versionen je Suite (`BNAS_KEEP`). Beide Suiten tragen eine gültige Signatur des neuen Schlüssels
  „BetterNAS Archive Signing Key“ (Fingerabdruck endet auf …D605E1FA7BF06915). Stand: `stable` = 0.7.0,
  `testing` = 0.7.0, 0.7.0~rc1 und 0.7.1~rc1.
- **Kanalumschalter**: Agent-RPC und API (`SetUpdateChannel`) schreiben nur das Feld `Suites:` in der eigenen Datei;
  eine manuell angepasste Quelle wird nie angefasst. Die GUI nutzt dafür den Update-Store.
- **Image**: Hook `40-apt.sh` trägt die BetterNAS-Quelle nur bei gesetztem `APT_URL` ein und prüft `APT_SUITE`
  (`stable` oder `testing`); `image/tests/check-rootfs.sh` und `tools/tests/apt-hook.test.sh` decken das ab.
- **Schlüsselrotation** und Kanalwechsel sind in der README beschrieben, die Lizenz (GPL-3.0-or-later, `LICENSE`,
  `CONTRIBUTING.md`) liegt im Repo.

Abweichungen und Ergänzungen gegenüber dem Entwurf:

- `tools/release.sh` existiert weiter und ist jetzt der empfohlene Kurzweg: Version und Kanal abfragen, ISO auf dem
  ThinkCentre bauen und in QEMU testen, Tag pushen, auf den Workflow warten, ISO per `tools/release-iso.sh` anhängen.
  Es ersetzt nicht den Workflow, sondern steuert ihn. Das ISO hängt als Release-Asset an (z. B. `betternas-0.7.0-amd64.iso`
  mit `.sha256`), nicht auf Pages.
- Die Debian-Quellen im Image sind `trixie`, `trixie-updates` und `trixie-security` (Debian 13, Kernel 6.12). Ein
  Wechsel auf eine neuere Debian-Version ist nicht Teil dieses Vorhabens.
- Das Verzeichnis für `arm64` ist in beiden Suiten angelegt, aber leer; Pakete gibt es nur für amd64.
- Das Paket `betternas` 0.7.0 in `stable` kennt die Abhängigkeiten `virtiofsd`, `nftables` und `openssh-server` noch
  nicht, die `packaging/build-deb.sh` inzwischen verlangt. Sie kommen mit dem nächsten stabilen Release.
- Der Branch `feat/update-distribution` ist in `main` enthalten und kann gelöscht werden.
- Die E-Mail-Adresse des Signierschlüssels (`archive@betternas.invalid`) ist ein Platzhalter und für APT ohne Bedeutung.
