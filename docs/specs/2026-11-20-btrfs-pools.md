# Entwurf: btrfs-Pools (Single, RAID1, RAID1c3, RAID10)

Stand: 2026-11-20 · Status: Entwurf, noch nichts umgesetzt

## 1. Ziel

Speicherpools gibt es bisher nur als mergerfs-Pool (optional mit SnapRAID). Neu: ein zweiter
Pool-Typ **btrfs**, bei dem mehrere Platten ein gemeinsames Dateisystem mit echter Redundanz bilden.

| Profil | Mindestplatten | Ausfallsicherheit | Nutzbar |
|---|---|---|---|
| `single` | 1 | keine (Prüfsummen erkennen Fehler) | Summe aller Platten |
| `raid1` | 2 | 1 Platte | Hälfte (bei gleich großen Platten) |
| `raid1c3` | 3 | 2 Platten | Drittel |
| `raid10` | 4 | 1 Platte je Spiegel | Hälfte |

**Bewusst nicht angeboten:** RAID5/6 (btrfs-Implementierung gilt weiter als unsicher), RAID0 (keine
Redundanz, aber höheres Risiko als `single`), ZFS und mdadm (eigene Entwürfe, falls gewünscht).

mergerfs bleibt Standard. Der Pool-Typ wird beim Anlegen gewählt und lässt sich danach nicht ändern.

## 2. Leitentscheidungen

1. **Gleicher Pfad, gleiche Schnittstelle.** Ein btrfs-Pool hängt unter `/srv/pools/<name>`. Freigaben
   (Samba/NFS), Docker-Stacks, VMs und Backups greifen weiter über `PoolPath(name)` zu und müssen nicht
   geändert werden. Auch `DependentsFunc` beim Löschen bleibt gültig.
2. **Mitgliedsplatten werden nicht einzeln eingehängt.** Sie bleiben als `disks`-Zeile mit Rolle `data`
   erhalten (so ist keine Änderung der `CHECK`-Bedingung in `0001_init.sql` nötig), tragen aber
   `fs_type='btrfs'` und die **gemeinsame** Dateisystem-UUID. Der Renderer überspringt Platten, die zu
   einem btrfs-Pool gehören, und erzeugt eine Mount-Unit für den Pool.
3. **Anlegen formatiert immer neu.** Es gibt keine Übernahme einzelner Platten in einen bestehenden
   btrfs-Pool über „Importieren" (Import eines vorhandenen Mehrplatten-btrfs: später, siehe §9).
4. **Kein automatisches `degraded`.** Fehlt eine Platte, hängt der Pool nicht von selbst ein. Die GUI
   zeigt „Degradiert – Platte fehlt" und bietet eine bewusste Aktion „Degradiert einhängen". Grund:
   Wer ein RAID1 unbemerkt degradiert beschreibt, bekommt Single-Chunks, die später nur mit
   Balance und Aufwand wieder sauber werden.
5. **Kein SnapRAID auf btrfs-Pools.** Redundanz und Prüfsummen liefert das Dateisystem. Die
   SnapRAID-Aktionen sind für diesen Typ gesperrt (API gibt `Conflict` zurück, GUI blendet sie aus).

## 3. Datenmodell

### State (`storage/state.go`)

```go
const (
    PoolMergerfs = "mergerfs"
    PoolBtrfs    = "btrfs"
)

type Pool struct {
    Name         string
    Type         string   // mergerfs (Standard, leer = mergerfs) | btrfs
    Branches     []string // btrfs: Mitgliedsplatten
    CreatePolicy string   // nur mergerfs
    MinFree      string   // nur mergerfs
    Profile      string   // nur btrfs: single|raid1|raid1c3|raid10
    FSUUID       string   // nur btrfs: UUID des gemeinsamen Dateisystems
    Compress     string   // nur btrfs: "" | "zstd:1" | "zstd:3"
}
```

`State.Validate()` verzweigt nach Typ:

- mergerfs: unverändert.
- btrfs: Profil bekannt, Mindestanzahl der Platten (siehe Tabelle), `FSUUID` gültig, alle Mitglieder
  haben `FSType=="btrfs"` und `FSUUID == Pool.FSUUID`, Mitglieder sind keine Paritätsplatten, keine
  Platte in zwei Pools. `CreatePolicy`/`MinFree` werden bei btrfs nicht geprüft.

### Migration `0020_pool_btrfs.sql`

```sql
ALTER TABLE pools ADD COLUMN type TEXT NOT NULL DEFAULT 'mergerfs';
ALTER TABLE pools ADD COLUMN profile TEXT NOT NULL DEFAULT '';
ALTER TABLE pools ADD COLUMN fs_uuid TEXT NOT NULL DEFAULT '';
ALTER TABLE pools ADD COLUMN compress TEXT NOT NULL DEFAULT '';
```

`create_policy` und `min_free` sind `NOT NULL`. Bei btrfs-Pools werden dort die Standardwerte
gespeichert (`epmfs`, `20G`), damit kein Tabellenumbau nötig ist. `loadPools`/`writePool` werden um
die neuen Spalten erweitert.

## 4. Renderer (`storage/render.go`)

- Platten, die Branch eines btrfs-Pools sind, erzeugen **keine** eigene Unit.
- btrfs-Pool → eine Unit `srv-pools-<name>.mount`:

```ini
# Verwaltet von BetterNAS – nicht manuell bearbeiten
[Unit]
Description=BetterNAS Pool <name> (btrfs)

[Mount]
What=/dev/disk/by-uuid/<fsuuid>
Where=/srv/pools/<name>
Type=btrfs
Options=noatime,nofail[,compress=zstd:3]

[Install]
WantedBy=multi-user.target <geräte-unit je Mitglied>
```

- `WantedBy` zeigt auf die `.device`-Unit der gemeinsamen Dateisystem-UUID (alle Mitglieder tragen
  dieselbe UUID). Die udev-Regel aus `btrfs-progs` (`btrfs ready`) sorgt dafür, dass das Gerät erst
  „fertig" meldet, wenn alle Mitglieder da sind. Das Einhängen startet damit erst, wenn der Pool
  vollständig ist; fehlt eine Platte, bleibt der Pool bewusst aus (siehe Leitentscheidung 4).
- Leerer Einhängeordner bleibt `chattr +i`.
- Der Renderer braucht dafür Zugriff auf die Mitglieder-UUIDs nur für die Geräte-Units; die
  Pool-UUID kommt aus `Pool.FSUUID`.

## 5. Agent-Operationen (typisiert, jeweils erneut validiert)

| RPC | Aktion |
|---|---|
| `pool.btrfs.create` | Mitglieder wipen und partitionieren (wie `Format`: `wipefs`, `sgdisk`, Partition `bnas-data`), dann `mkfs.btrfs -f -L bnas-<name> -d <profil> -m <meta> <partitionen…>`; Metadaten = Profil, bei `single` über mehrere Platten `raid1` (einzelne Platte: `dup`); liefert `fsUuid` und Label |
| `pool.btrfs.status` | `btrfs filesystem show`, `btrfs filesystem usage -b`, `btrfs device stats`: Mitglieder, fehlende Geräte, Fehlerzähler, genutzt/frei/Rohkapazität, Profile |
| `pool.btrfs.mountdegraded` | `mount -o degraded` für diesen einen Pool (Audit-Eintrag, kein Dauerzustand) |
| `pool.btrfs.replace` | `btrfs replace start -B <alt|devid> /dev/neu /srv/pools/<name>`; Fortschritt per `btrfs replace status` |
| `pool.btrfs.add` / `.remove` | `btrfs device add` bzw. `device remove`; danach Balance (siehe unten) |
| `pool.btrfs.convert` | `btrfs balance start -dconvert=<p> -mconvert=<p>` (Profilwechsel, etwa single → raid1) |
| `pool.btrfs.scrub` | `btrfs scrub start/status/cancel` |
| `pool.btrfs.balance` | `btrfs balance start -dusage=<n> -musage=<n>`, Status und Abbruch |

Langläufer (replace, add/remove, convert, scrub, balance) laufen wie SnapRAID in einer transienten
systemd-Unit; die API begleitet sie als Job und pollt den Status. Die Unit hält eine
`flock`-Sperre je Pool, damit nie zwei Wartungsläufe gleichzeitig laufen.

Pfade und Geräte werden nur aus dem Soll-Zustand abgeleitet, nie aus freien Eingaben der API.

## 6. API (`internal/api/storage.go`)

- `POST /api/v1/pools` bekommt `type`, `profile`, `compress`. Für btrfs gilt: Mitglieder dürfen
  **leere oder unverwaltete** Platten sein, die Platten werden dabei gelöscht.
- **Bestätigung wie beim Formatieren:** `POST /api/v1/pools/preview` (nur btrfs) liefert je Platte
  Modell, Seriennummer, Größe und vorhandene Dateisysteme sowie ein einmaliges Token. Anlegen verlangt
  Token plus die Eingabe des Poolnamens. Verändert sich eine Platte seit der Vorschau (Gerät, Größe),
  schlägt das Anlegen fehl, wie bei `PreviewFormat`/`Format`.
- Für btrfs-Pools gesperrt: SnapRAID-Endpunkte, `createPolicy`/`minFree`.
- Neu: `GET /pools/{name}/btrfs` (Status), `POST /pools/{name}/btrfs/{scrub|balance|convert}`,
  `POST …/replace`, `POST …/devices` (hinzufügen), `DELETE …/devices/{id}`,
  `POST …/mount-degraded`.
- `PoolView` bekommt `Type`, `Profile`, `Compress`, `Degraded`, `MissingDevices`,
  `RawBytes` und `UsableBytes`. `SizeBytes`/`FreeBytes` stammen bei btrfs aus
  `filesystem usage` statt `statfs`, weil `statfs` bei Mischgrößen und RAID1 irreführt.

## 7. Betrieb und Überwachung

- **Fehlerzähler:** `btrfs device stats` pro Mitglied (write/read/flush/corruption/generation). Werte
  > 0 lösen eine Benachrichtigung über das bestehende Notify-System aus (Beginn: nur Anzeige und
  Ereignis, kein Abschalten).
- **Scrub-Timer:** monatlich je btrfs-Pool (`betternas-btrfs-scrub@<name>.timer`), Ergebnis in den
  Einstellungen/Status, Fehler → Benachrichtigung. Zeitplan in der GUI einstellbar, analog SnapRAID.
- **Balance:** nur manuell oder per Hinweis, wenn Platten sehr ungleich belegt sind (nach Hinzufügen
  einer Platte). Nie automatisch.
- **Degradiert:** Pool-Zustand `degraded` wenn Mitglieder fehlen; Karte auf der Startseite und
  Ereignis `pools.changed`.
- **SMART, Spindown, Benchmark, Identifizieren** arbeiten pro Platte und ändern sich nicht. Spindown
  ist bei btrfs-RAID1 praktisch wirkungslos, weil Schreibzugriffe immer alle Spiegel treffen; die GUI
  weist darauf hin.
- **Plattenersatz:** der bestehende SnapRAID-Ersetzen-Assistent (`replace.go`) bleibt für mergerfs.
  Für btrfs gibt es einen eigenen Ablauf mit `btrfs replace`; die Ersatzplatte darf nicht kleiner sein
  als die alte (der Assistent prüft das wie bisher).

**Stand Etappe 2 (umgesetzt):**
- `pool.btrfs.status` liefert Platten (mit Fehlerzählern), fehlende Platten (auch bei nicht eingehängtem
  Pool über `btrfs filesystem show <uuid>`), tatsächliche Profile, Rohgröße, nutzbar und frei sowie die
  letzte oder laufende Prüfung. Der Manager ergänzt die BetterNAS-Kennung je Platte und zählt
  Mitglieder, die btrfs nicht mehr auflistet, als fehlend. `GET /pools` trägt das als `btrfs` am Pool;
  `GET /pools/{name}/btrfs` liefert nur den Status.
- Prüfung: `POST /pools/{name}/btrfs/scrub` mit `{"action":"start"|"cancel"}` (startet die Dienst-Unit
  `betternas-btrfs-scrub@<pool>.service`, die auch der Timer nutzt). Der Timer
  `betternas-btrfs-scrub@<pool>.timer` läuft **fest monatlich**; der Agent schaltet ihn mit `Apply` ein
  und beim Löschen des Pools aus. Ein einstellbarer Zeitplan kommt mit der GUI (Etappe 4).
- Monitor-Befunde: `pool-degraded:<pool>` (kritisch), `pool-errors:<pool>` (Warnung, solange die
  Fehlerzähler > 0 sind) und `pool-scrub:<pool>` (kritisch bei nicht reparierbaren Fehlern eines
  beendeten Laufs). Die Fehlerzähler lassen sich erst in Etappe 3 zurücksetzen (`btrfs device stats -z`).

**Stand Etappe 3 (umgesetzt):**
- Agent: `pool.btrfs.replace.start|status`, `device.add`, `resize`, `op.start|status` (Balance, Platte
  entfernen, Profilwechsel in der transienten Unit `betternas-btrfs-op-<pool>.service` mit
  `RemainAfterExit`, Fortschritt aus `btrfs balance status`), `mountdegraded`, `resetstats`.
  Nie zwei Wartungen gleichzeitig (Agent prüft Unit und `btrfs replace status`, der Manager reserviert
  den Pool).
- Degradiert einhängen geschieht per `mount -o degraded` statt über die Unit: Die Unit wartet auf die
  Geräte-Unit, die erst „fertig" ist, wenn alle Platten da sind. Es gilt bis zum nächsten Neustart.
- Eine nicht mehr gelistete fehlende Platte hat vor dem degradierten Einhängen keine devid; Ersetzen
  verlangt deshalb zuerst „Degradiert einhängen" (danach zeigt `btrfs filesystem show` die devid).
  Entfernen einer fehlenden Platte geht über `device remove missing`.
- API: `POST /pools/{n}/btrfs/replace` (`oldDiskId`, `diskId`, Token, `confirmName`), `POST …/devices`
  (Hinzufügen, optional `balance`), `DELETE …/devices/{id}?confirm=<pool>`, `POST …/balance`
  (`usage`), `POST …/convert` (`profile`, bei weniger Redundanz `confirmName`),
  `POST …/mount-degraded`, `POST …/reset-errors`. Die langen Abläufe sind Jobs, die die
  Speicher-Sperre nur für die Änderung des Soll-Zustands halten.
- Entfernen ist nur erlaubt, solange die Plattenzahl über dem Minimum des Profils bleibt.

## 8. GUI

- Pool-Dialog: erste Auswahl „Typ" mit zwei Karten. Gut erklärt, mit Warnung vor Datenverlust bei
  btrfs.
  - **Flexibel (mergerfs):** wie bisher, beliebige Platten, Dateien bleiben auf einer Platte lesbar,
    optional SnapRAID.
  - **Redundant (btrfs):** Profil wählen (nur sinnvolle je nach Plattenzahl), Mitglieder aus leeren
    oder unverwalteten Platten, nutzbare Kapazität live berechnet, Kompression (aus / zstd), dann
    Bestätigung mit Poolname.
- Pool-Karte: Profil-Badge, Zustand (OK / degradiert / Wartung läuft mit Fortschritt), nutzbar statt
  roh, Fehlerzähler.
- Pool-Seite für btrfs: Mitglieder mit Fehlerzählern, Aktionen Scrub, Balance, Platte ersetzen,
  Platte hinzufügen/entfernen, Profil ändern, „Degradiert einhängen".
- Texte alle deutsch, wie im Rest der GUI.

**Stand Etappe 4 (umgesetzt):**
- `PoolDialog`: beim Anlegen Typ „Flexibel (mergerfs)“ oder „Redundant (btrfs)“. Bei btrfs nur freie, nicht
  eingerichtete Platten; Profil wird ab zwei Platten automatisch auf RAID1 gestellt (Mindestplatten je
  Profil sperren Auswahl), nutzbare Kapazität live, Warnung bei ungleich großen Platten und vor dem Löschen
  (mit Nennung vorhandener Dateisysteme), Bestätigung durch Eintippen des Poolnamens. `PoolsView` holt
  dann die Vorschau (Token) und legt an.
- Pool-Karte (`BtrfsPanel`): Profil, Zustand (In Ordnung / Fehler gezählt / Wartung läuft / Unvollständig),
  Fortschritt von Plattenersatz und Wartung, Fehlerzähler je Platte, Ergebnis der letzten Prüfung,
  „Jetzt prüfen“ und Menü „Wartung“ (ersetzen, hinzufügen, entfernen, Balance, Schutz ändern, Fehlerzähler
  zurücksetzen); bei unvollständigem Pool „Degradiert einhängen“. `BtrfsActionDialog` bündelt die
  Dialoge. Lange Aufträge schließen den Dialog sofort und melden das Ergebnis als Hinweis.
- Prüfzeitplan: `GET/PUT /api/v1/btrfs/scrub-schedule` (aus, wöchentlich, monatlich, vierteljährlich, Uhrzeit;
  gilt für alle btrfs-Pools, gespeichert in `settings`, im Soll-Zustand als `State.ScrubSchedule`, vom
  Agent als Drop-in der Timer-Vorlage umgesetzt) und Dialog „Prüfzeitplan“ in der Pool-Übersicht.

## 9. Bewusst später

- Übernahme eines vorhandenen Mehrplatten-btrfs (Import).
- Subvolumes pro Freigabe und Snapshots (z. B. stündlich, wöchentlich) als Schutz vor Löschen.
- Quoten.
- mdadm- oder ZFS-Pools als weitere Typen.

## 10. Image und Paket

- `btrfs-progs` in `Depends` von `packaging/build-deb.sh` und in `image/packages/rootfs.list`.
- Kernel kommt aus trixie-backports (aktuell), btrfs ist darin enthalten; `image/tests/check-rootfs.sh`
  prüft zusätzlich `mkfs.btrfs` und `btrfs`.
- Der Installer ist nicht betroffen (Systemplatte bleibt ext4).

## 11. Tests

- Unit: `Validate` (Mindestanzahl je Profil, UUID-Konsistenz, Doppelzuordnung), Renderer
  (Golden-Test der Unit, keine Einzel-Units der Mitglieder), `writePool`/`loadPools` mit Migration,
  Parser für `btrfs filesystem show/usage` und `device stats` anhand von Beispielausgaben (testdata).
- Manager mit Fake-Agent: Anlegen mit gültigem/abgelaufenem Token, geänderte Platte, SnapRAID auf
  btrfs-Pool abgelehnt, Löschen mit Abhängigkeiten.
- Integration (wie `snapraid_integration_test.go`, Loop-Geräte, nur Linux): Pool mit 2 und 3 Loop-
  Geräten anlegen, ein Gerät entfernen → degradiert, ersetzen, Scrub, Profilwechsel.

## 12. Umsetzungsreihenfolge

1. **Grundlage:** Migration, State/Validate, Renderer, `btrfs-progs` im Paket, Agent `pool.btrfs.create`,
   Manager/API Anlegen und Löschen, Tests. (Pools nutzbar, aber ohne Wartung.)
2. **Status und Überwachung:** `pool.btrfs.status`, Degradiert-Erkennung, Fehlerzähler, Scrub-Timer,
   Benachrichtigungen.
3. **Wartung:** replace, add/remove, balance, convert, mount-degraded.
4. **GUI:** Pool-Dialog mit Typauswahl, Pool-Seite, Karten und Hinweise.
   (Kann parallel zu 2 und 3 beginnen, sobald die API aus 1 steht.)

Jede Etappe wird einzeln ausgerollt und auf dem Testgerät (192.168.1.65) mit Loop-Geräten bzw.
einer freien Platte geprüft.

## 13. Entscheidungen (2026-11-20)

1. Kompression ist standardmäßig **aus**, im Dialog schaltbar (aus / zstd:1 / zstd:3).
2. `raid1c3` wird angeboten (ab drei Platten).
3. Mischgrößen sind erlaubt. Die nutzbare Kapazität wird live berechnet, bei starker Ungleichheit
   erscheint ein Hinweis.
4. Keine Konvertierung vorhandener mergerfs-Platten. Daten sichern, Platte freigeben, Pool neu anlegen.
   Eine Migration wäre ein eigener Entwurf.

Offene Fragen gibt es keine mehr.
