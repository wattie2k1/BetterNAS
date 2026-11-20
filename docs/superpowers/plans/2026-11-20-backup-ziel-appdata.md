# Sicherung: frei wählbarer Speicherort + appdata

Ziel: (1) Der Ort der Sicherungen ist frei wählbar (bisher fest `/var/lib/betternas/backups` auf der Systemplatte).
(2) Optional wird zusätzlich der appdata-Ordner (Einstellungen der Docker-Apps) gesichert.

## Entscheidungen
- `Config` bekommt `dir` ("" = Systemplatte, Standard, wie bisher) und `appdata` (bool, Standard aus).
- Der Ordner wird wie die Docker-Speicherorte geprüft (`docker.CleanRoot`: absolut, sichere Zeichen, ≥ 2 Ebenen, keine Systemverzeichnisse, nicht /srv/pools|disks|parity|vms selbst). Netzwerk-Mounts sind erlaubt (typisches Sicherungsziel).
- Die API läuft sandboxed (`ProtectSystem=strict`) und schreibt nur in ihr Zustandsverzeichnis. Schreiben/Löschen/Aufräumen im Zielordner macht deshalb der Agent (root). Er legt den Ordner `root:betternas 0750`, Dateien `0640` an, damit die API Liste, Download und Wiederherstellung direkt lesen kann (kein Umkopieren großer Archive).
- Konfig-Sicherung: API erzeugt das Archiv wie bisher (Staging `<state>/staging`), der Agent verschiebt es in den Zielordner. Standardort bleibt ohne Agent.
- appdata-Sicherung: eigenes Archiv `betternas-appdata-JJJJMMTT-hhmmss.tar.gz`, vom Agenten mit `tar -czp --numeric-owner` direkt in den Zielordner geschrieben (`.part` → `mv`), Platzprüfung vorab, Zielordner wird ausgeschlossen, wenn er unter appdata liegt. Dauert → als Job (`backup.appdata`). Nächtlich zusammen mit der Konfiguration, wenn aktiviert.
- Aufbewahrung (`keep`) gilt je Art (Konfiguration / appdata) getrennt.
- Wiederherstellung per GUI nur für die Konfiguration. appdata: Download + Hinweis auf `tar -xzpf` (Container vorher stoppen). GUI-Restore für appdata bewusst später.
- Beim Wechsel des Ordners bleiben vorhandene Sicherungen im alten Ordner (Hinweis in der GUI).

## Schritte
1. backup-Paket: Config (`dir`, `appdata`), `Kind`, Namensmuster für appdata, List/Prune/Delete/Open für beide Arten (Tests).
2. Agent: `backup.prepare|store|delete|prune|appdata` (`agent/backup.go`, Tests mit FakeRunner).
3. Manager: dynamischer Ordner, Create über Staging+Agent, `CreateAppdata` als Job, Planer sichert beides, Prune je Art (Tests).
4. API: Config-Felder, `POST /api/v1/backups/appdata` (Job), Info-Feld `kind` (Tests).
5. Frontend: Abschnitt „Speicherort“ (FolderPicker), Schalter appdata, Knopf „appdata jetzt sichern“, Art-Spalte, Restore nur für Konfiguration (Vitest).
6. Ausrollen, auf dem Testgerät real prüfen (Ordner wählen, beide Arten, Download, Aufräumen).
