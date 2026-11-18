# Einstellungen Etappe 5 – Sicherheit + Wartung

Annahmen (Nutzer sagte nur „setze Phase 5 um“; KVM-Phase 5 ist längst fertig → gemeint ist Etappe 5 des Einstellungs-Plans):

**Anmeldung** (`/einstellungen/anmeldung`): Sitzungsdauer (1–720 h), Login-Sperre (Fehlversuche 3–20, Fenster 1–60 min), 2FA-Pflicht für alle Admins, aktive Sitzungen (Benutzer, angelegt, IP, aktuelle markiert) beenden / alle anderen beenden.
- Policy in `settings` (`auth.policy`), wirkt sofort (Store-TTL, RateLimiter live umstellbar).
- 2FA-Pflicht: `RequireSession` lässt ohne aktives 2FA nur `/api/v1/auth/*` durch (403 `mfa_required`); `Me.totpRequired`; Router leitet auf `/profil`.
- Migration 0009: `sessions.created_at`, `sessions.ip`. Sitzungs-ID = Präfix des Token-Hashes.

**Protokolle** (`/einstellungen/protokolle`): Audit-Log (letzte 200), API-/Agent-Log (journalctl über Agent `logs.read`, feste Unit-Liste, 50–2000 Zeilen), Download als Datei, Support-Paket (tar.gz: Systeminfo, Journale, Audit, lsblk/df/ip …; OHNE `settings`/Zugangsdaten).

**Über** (`/einstellungen/ueber`): Version, Kernel, Laufzeit, Lizenz GPL-3.0, Links; Werkseinstellungen mit Wort `ZURÜCKSETZEN`: Marker `restore/factory-reset` + `system.restart-api`; beim Start `backup.ApplyFactoryReset` verschiebt die DB nach `.pre-reset-<zeit>` (Setup-Assistent erscheint, Daten auf Platten/Docker/Systembenutzer bleiben).

Nicht Teil: Firewall, SSH (Etappe 6).

Tests: Go (Policy, Sessions, RateLimiter, mfa_required, Logs-Whitelist, Support-Paket, Reset), Vitest je Bereich. Ausrollen als 0.7.0+dev9.
