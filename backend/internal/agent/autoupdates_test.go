package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

const autoLogDE = `2026-11-21 06:59:39,913 INFO Skript für automatische Systemaktualisierung wird gestartet
2026-11-21 06:59:40,819 INFO Es wurden keine Pakete gefunden, von denen eine automatische Systemaktualisierung durchgeführt werden kann und kein ausstehendes automatisches Entfernen.
2026-11-22 06:15:39,909 INFO Skript für automatische Systemaktualisierung wird gestartet
2026-11-22 06:15:48,816 INFO Pakete, welche aktualisiert werden: base-files libc6 sudo
2026-11-22 06:16:39,594 INFO Alle Systemaktualisierungen installiert
2026-11-23 06:15:39,909 INFO Skript für automatische Systemaktualisierung wird gestartet
2026-11-23 06:15:40,000 ERROR Irgendein späterer Fehler ohne Pakete
`

func TestParseAutoUpgradeLog(t *testing.T) {
	r := ParseAutoUpgradeLog(autoLogDE, time.UTC)
	if r == nil || r.At != "2026-11-22T06:15:48Z" || r.Result != "ok" || strings.Join(r.Packages, ",") != "base-files,libc6,sudo" {
		t.Fatalf("%+v", r)
	}
	if ParseAutoUpgradeLog("2026-11-21 06:59:39,913 INFO Skript gestartet\n", time.UTC) != nil {
		t.Fatal("ohne Pakete kein Lauf")
	}
	en := "2026-11-22 06:15:48,816 INFO Packages that will be upgraded: a b\n2026-11-22 06:16:00,000 ERROR Installation failed\n"
	if r := ParseAutoUpgradeLog(en, time.UTC); r == nil || r.Result != "failed" || len(r.Packages) != 2 {
		t.Fatalf("%+v", r)
	}
	unknown := "2026-11-22 06:15:48,816 INFO Pakete, welche aktualisiert werden: a\n"
	if r := ParseAutoUpgradeLog(unknown, time.UTC); r == nil || r.Result != "unknown" {
		t.Fatalf("%+v", r)
	}
}

func TestAutoUpdatesSwitch(t *testing.T) {
	d, _ := updatesEnv(t)
	ctx := context.Background()
	if a, _ := GetAutoUpdates(ctx, d); a.Enabled {
		t.Fatal("ohne Konfiguration aus")
	}
	_ = d.FS.WriteFile(autoDebianPath, []byte("APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n"), 0o644)
	if a, _ := GetAutoUpdates(ctx, d); !a.Enabled {
		t.Fatal("Debian-Vorgabe an")
	}
	if err := SetAutoUpdates(ctx, d, false); err != nil {
		t.Fatal(err)
	}
	if a, _ := GetAutoUpdates(ctx, d); a.Enabled {
		t.Fatal("Drop-in muss die Vorgabe übersteuern")
	}
	if err := SetAutoUpdates(ctx, d, true); err != nil {
		t.Fatal(err)
	}
	if a, _ := GetAutoUpdates(ctx, d); !a.Enabled {
		t.Fatal("wieder an")
	}
	_ = d.FS.WriteFile(autoLogPath, []byte(autoLogDE), 0o644)
	if a, _ := GetAutoUpdates(ctx, d); a.LastRun == nil || a.LastRun.Result != "ok" {
		t.Fatalf("%+v", a.LastRun)
	}
}
