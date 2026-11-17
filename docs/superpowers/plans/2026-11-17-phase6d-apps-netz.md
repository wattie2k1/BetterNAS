# Phase 6d – App-Vorlagen, Compose-Schema im Editor, Bridge br0 (Opt-in) – Plan

> REQUIRED SUB-SKILL: superpowers:executing-plans (inline, TDD).

**Goal:** (A) Docker-Stacks lassen sich aus Vorlagen (Jellyfin, Nextcloud, Home Assistant, Vaultwarden, Pi-hole, Syncthing …) mit Pfad-/Port-/Passwort-Eingaben anlegen. (B) Der Compose-Editor validiert gegen das Compose-Spec-JSON-Schema (`monaco-yaml`). (C) Eine Bridge `br0` für VMs lässt sich als Opt-in einschalten, mit automatischem Rückfall.

**Entscheidungen**
- **A rein im Frontend** (`frontend/src/utils/appTemplates.ts`): Vorlage = Metadaten + Felder (`text|number|path|secret`) + `render(values) → {compose, env}`. Pfade liegen standardmäßig unter `/srv/appdata/<stack>/…`, Medienpfade wählt man aus den Pools (`/srv/pools/<pool>`) oder tippt sie. Geheimnisse werden im Browser zufällig erzeugt (`crypto.getRandomValues`) und landen in der `.env`. Der Stack wird danach wie jeder andere im Stack-Dialog bearbeitet. Kein neuer Backend-Code.
- **B:** `monaco-yaml` mit mitgeliefertem `compose-spec.json` (offline, kein Abruf zur Laufzeit), eigener Worker, Chunk bleibt lazy.
- **C:** Agent `network.bridge.{status,enable,confirm,disable}`: schreibt systemd-networkd-Dateien (`05-bnas-br0.netdev`, `05-bnas-br0-port.network`, `05-bnas-br0.network` mit der bisherigen Konfiguration), ersetzt `10-betternas.network` der Schnittstelle, **gleiche MAC** für br0, aktiviert einen transienten Rückfall-Timer (`systemd-run --on-active=120`), der Dateien zurückspielt und networkd neu startet, wenn nicht binnen 2 min bestätigt wird. `confirm` (aus der GUI nach dem Neuladen) stoppt den Timer. VM-Netz `bridge:br0` (`<interface type='bridge'>`), nur wählbar wenn br0 existiert. Nur eine Schnittstelle, kein WLAN, keine Bridge wenn eine VM `direct:<if>` nutzt.
- **Nicht in 6d:** statische IP/VLAN/Bonding, weitere Bridges, Auslieferung/Ausrollen von br0 auf dem Testgerät (nur eno1 → Aussperrrisiko, nur Unit-Tests).

## Tasks
- [ ] 1 App-Vorlagen (utils + Dialog + Einbindung in Stacks-Ansicht)
- [ ] 2 monaco-yaml + Compose-Schema
- [ ] 3 Bridge: Rendern/Validieren (Go, Unit-Tests)
- [ ] 4 Bridge: Agent, API, VM-XML/Netzliste
- [ ] 5 Bridge: GUI (Seite „Netzwerk“ bzw. VM-Seite) und Wizard
- [ ] 6 Tests, Spec §8, Memory, Ausrollen (0.5.0-rc5, ohne br0 zu aktivieren)
