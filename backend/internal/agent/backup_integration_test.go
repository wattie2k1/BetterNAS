//go:build integration

// Integrationstest für Sicherungen in einen frei gewählten Ordner mit echtem tar, install und chown.
// Braucht root und den Benutzer/die Gruppe betternas (vom Paket angelegt).
package agent

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/backup"
	"github.com/betternas/betternas/internal/sysexec"
)

func TestBackupTargetReal(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	if _, err := exec.Command("getent", "group", "betternas").Output(); err != nil {
		t.Skip("Gruppe betternas fehlt")
	}
	ctx := context.Background()
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/")}
	useLegacyPaths(t) // appdata = /srv/appdata

	base := "/srv/dockerdata/bnasit-backup"
	_ = os.RemoveAll(base)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	if err := os.MkdirAll("/srv/dockerdata", 0o755); err != nil {
		t.Fatal(err)
	}
	// ein kleiner Test-appdata-Ordner statt der echten Daten
	app := "/srv/appdata/bnasit-backup-src"
	_ = os.RemoveAll(app)
	t.Cleanup(func() { _ = os.RemoveAll(app) })
	if err := os.MkdirAll(app+"/app1", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app+"/app1/config.yml", []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(app+"/app1/config.yml", 1234, 100); err != nil {
		t.Fatal(err)
	}

	dir := base + "/neu/sicherung" // wird angelegt
	tg, err := BackupPrepare(ctx, d, dir)
	if err != nil || tg.Dir != dir || tg.Free <= 0 || tg.Appdata != "/srv/appdata" {
		t.Fatalf("%+v %v", tg, err)
	}
	if _, err := os.Stat(dir + "/.betternas-probe"); err == nil {
		t.Fatal("Probe-Datei blieb liegen")
	}

	// Konfigurations-Sicherung aus dem Staging übernehmen
	if err := os.MkdirAll(backupStage, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "betternas-backup-20261120-020000.tar.gz"
	src := backupStage + name
	if err := os.WriteFile(src, []byte("archiv"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := BackupStore(ctx, d, src, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); err == nil {
		t.Fatal("Staging-Datei blieb liegen")
	}
	stat := func(p string) string {
		out, err := exec.Command("stat", "-c", "%U:%G %a", p).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v %s", p, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if got := stat(dir + "/" + name); got != "root:betternas 640" {
		t.Fatalf("Besitzer/Rechte: %s", got)
	}
	// Die API (Benutzer betternas) muss lesen können
	if out, err := exec.Command("runuser", "-u", "betternas", "--", "cat", dir+"/"+name).CombinedOutput(); err != nil || string(out) != "archiv" {
		t.Fatalf("betternas kann nicht lesen: %v %s", err, out)
	}

	// appdata: Quelle auf den Testordner umbiegen
	p := *curPaths.Load()
	p.Appdata = app
	curPaths.Store(&p)
	res, err := BackupAppdata(ctx, d, dir)
	if err != nil || !strings.HasPrefix(res.Name, "betternas-appdata-") || res.Size <= 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if got := stat(dir + "/" + res.Name); got != "root:betternas 640" {
		t.Fatalf("Besitzer/Rechte appdata: %s", got)
	}
	out, err := exec.Command("tar", "--numeric-owner", "-tvzf", dir+"/"+res.Name).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "app1/config.yml") || !strings.Contains(string(out), "1234/100") {
		t.Fatalf("Archivinhalt (Besitzer 1234/100 erwartet): %v\n%s", err, out)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 2 {
		t.Fatalf("unerwartete Dateien: %v", ents)
	}

	// Zielordner innerhalb von appdata wird ausgeschlossen
	inner := app + "/sicherung"
	if _, err := BackupPrepare(ctx, d, inner); err != nil {
		t.Fatal(err)
	}
	r2, err := BackupAppdata(ctx, d, inner)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := exec.Command("tar", "-tzf", inner+"/"+r2.Name).CombinedOutput(); strings.Contains(string(out), r2.Name) || strings.Contains(string(out), "sicherung/") {
		t.Fatalf("Archiv enthält sich selbst:\n%s", out)
	}

	// Aufräumen: je Art nur die neueste behalten, dann löschen
	for _, n := range []string{"betternas-backup-20261119-020000.tar.gz", "betternas-backup-20261118-020000.tar.gz"} {
		if err := os.WriteFile(dir+"/"+n, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if err := BackupPrune(ctx, d, dir, 1); err != nil {
		t.Fatal(err)
	}
	l, err := backup.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, i := range l {
		kinds[i.Kind]++
	}
	if kinds[backup.KindConfig] != 1 || kinds[backup.KindAppdata] != 1 || l[len(l)-1].Name == "" {
		t.Fatalf("%+v", l)
	}
	if err := BackupDelete(ctx, d, dir, res.Name); err != nil {
		t.Fatal(err)
	}
	if err := BackupDelete(ctx, d, dir, res.Name); err == nil {
		t.Fatal("zweites Löschen müsste fehlschlagen")
	}
}
