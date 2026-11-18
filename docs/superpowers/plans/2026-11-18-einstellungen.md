# Einstellungen – Plan

Ziel: Ein Dock-Eintrag **Einstellungen** (`/einstellungen`), unter dem alle Einstellmöglichkeiten von BetterNAS an einem Ort liegen. Links eine Gruppenliste, rechts der jeweilige Bereich. Die drei bisherigen Einzeleinträge im Dock (Benachrichtigungen, Sicherung, Updates) wandern dorthin; die alten Pfade `/system/*` leiten um.

## Struktur

| Gruppe | Bereich | Inhalt | Stand |
|---|---|---|---|
| **System** | Allgemein | Hostname, Zeitzone, NTP-Server/Uhrzeit, Sprache der Oberfläche, Systeminfo (Version, Kernel, Laufzeit) | neu |
| | Netzwerk | Schnittstellen mit IP/Gateway/DNS (Anzeige), DHCP ↔ statische IP, DNS-Server, Suchdomäne, MTU, Bridge `br0` (aus dem VM-Dialog hierher) | neu + vorhandene Bridge |
| | Webzugriff | HTTPS-Zertifikat anzeigen (Ablauf, Fingerabdruck), eigenes Zertifikat hochladen, selbstsigniert neu erzeugen, Port, HTTP→HTTPS-Umleitung | neu |
| | Energie | Spindown-Zeit (bisher in Datenträger), Zeitplan für Herunterfahren/Aufwecken, Neustart/Herunterfahren-Buttons | teils vorhanden |
| **Dienste** | Dateidienste | SMB global (Arbeitsgruppe, Mindest-Protokoll, Gastzugriff), NFS (Aktiv, erlaubte Netze), mDNS/Avahi-Name, Time-Machine-Ziel | neu |
| | Docker | `data-root`, Log-Rotation, Registry-Mirror, Docker-Dienst starten/stoppen, Aufräum-Zeitplan (`prune`) | neu |
| | Virtualisierung | libvirt-Speicherpfade (VM-Disks, ISO), Standard-Netz, Dienststatus | neu |
| **Sicherheit** | Anmeldung | Sitzungsdauer, Login-Sperre/Rate-Limit, 2FA-Pflicht für Admins, aktive Sitzungen beenden | neu |
| | Firewall | einfache Regeln je Dienst (SSH, Web, SMB, NFS) und erlaubte Netze | neu (später) |
| | SSH | Dienst an/aus, Port, Passwort-Login erlauben, Schlüssel je Benutzer | neu (später) |
| **Wartung** | Benachrichtigungen | Mail/ntfy, Testnachricht, Verlauf | vorhanden → verschieben |
| | Sicherung | Konfig-Backup, Zeitplan, Restore | vorhanden → verschieben |
| | Updates | Kanal, Prüfen, Installieren | vorhanden → verschieben |
| | Protokolle | Audit-Log, API-/Agent-Log einsehen und als Datei laden, Support-Paket erzeugen | neu |
| | Über | Version, Lizenz (GPL-3.0), Links, Zurücksetzen auf Werkseinstellungen | neu |

## Etappen

**Etappe 1 – Rahmen (jetzt).** `SettingsView` mit Gruppenliste + `RouterView`; Routen `/einstellungen/{benachrichtigungen,sicherung,updates,netzwerk}`; Dock-Eintrag „Einstellungen“ ersetzt die drei Einzeleinträge; Umleitungen von `/system/*`; Dashboard-Link angepasst. „Netzwerk“ zeigt zunächst die Bridge-Verwaltung. Nur Frontend, Tests für Navigation.

**Etappe 2 – Netzwerk (umgesetzt: IPv4 DHCP/statisch, Gateway, DNS, Suchdomänen).** Umsetzung als systemd-networkd-Drop-in `10-bnas-ip.conf` neben der verwalteten `.network`-Datei (bei aktiver Bridge an `br0`), Sicherung + Rückfall-Timer `bnas-net-rollback`, Bestätigung in der GUI. MTU, Hostname und IPv6 statisch folgen später. Ursprünglicher Entwurf: Neues Paket-Verhalten in `netcfg` (systemd-networkd, wie Bridge): Schnittstellenliste lesen (`ip -j addr`, `resolvectl`), DHCP/statisch schreiben mit **Rückfall-Timer und Bestätigung** (Muster der Bridge, 120 s), DNS/Suchdomäne, Hostname. Agent-RPC `network.*`, API `/api/v1/network/*`, GUI-Formular mit Warnung vor Aussperrung. Kein Test auf dem Testgerät mit echter Umstellung ohne Absprache (nur `eno1`).

**Etappe 3 – Allgemein, Webzugriff, Energie (umgesetzt: Hostname inkl. /etc/hosts, Zeitzone, NTP an/aus und eigene NTP-Server, Zertifikat anzeigen/hochladen/neu erzeugen ohne Neustart per `GetCertificate`, Neustart/Herunterfahren mit Bestätigungswort). Nicht umgesetzt: Port und HTTP→HTTPS-Umleitung, Spindown bleibt pro Datenträger in der Datenträger-Ansicht, Zeitplan für Herunterfahren.** Ursprünglicher Entwurf: Hostname/Zeitzone/NTP über `hostnamectl`/`timedatectl`; Zertifikat anzeigen/hochladen/neu erzeugen (API muss das Zertifikat neu laden, ohne Neustart per `GetCertificate`); Energie (Spindown hierher verschieben, Datenträger-Seite verlinkt).

**Etappe 4 – Dienste (umgesetzt: Übersicht der Dienste mit Start/Stopp/Neustart über feste Liste, SMB global (Arbeitsgruppe, Beschreibung, Mindestprotokoll) über `betternas.d/global.conf`, Docker (Log-Größe/-Anzahl, Registry-Mirrors, Live-Restore) in der `daemon.json` mit `dockerd --validate`. Nicht umgesetzt: NFS-Globals, libvirt-Pfade und Aufräum-Zeitplan; der Docker-`data-root` wird nur angezeigt).** Ursprünglicher Entwurf: SMB-/NFS-Globals, Docker- und libvirt-Einstellungen, jeweils mit Dienststatus. Änderungen an `daemon.json` nur, wenn sie von BetterNAS verwaltet ist (Kennzeichnung wie bei networkd).

**Etappe 5 – Sicherheit + Wartung.** Sitzungs-/Login-Optionen, 2FA-Pflicht, Protokolle, Support-Paket, „Über“, Werkseinstellungen (mit Wort-Bestätigung wie beim Restore). Firewall und SSH als eigene Etappe 6, weil Aussperrrisiko.

## Regeln für alle Bereiche

- Jede Einstellung ist in der Konfigurations-DB (`settings`) oder in einer von BetterNAS verwalteten Datei mit Kopfzeile abgelegt, damit das Konfig-Backup sie mitnimmt; Systemdateien werden nie ohne Kennzeichnung überschrieben.
- Riskante Änderungen (Netzwerk, Webzugriff, Firewall, SSH) bekommen Vorschau, Bestätigung und automatischen Rückfall.
- Nur Admins sehen Einstellungen (Rollenprüfung im API, nicht nur im Menü).
- Jeder Bereich ist eine eigene Route mit eigener Komponente unter `views/settings/`, damit er einzeln testbar bleibt.
