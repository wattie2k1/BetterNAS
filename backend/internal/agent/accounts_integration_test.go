//go:build integration

package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/shares"
	"github.com/betternas/betternas/internal/sysexec"
)

func TestAccountsLifecycle(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	ctx := context.Background()
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/")}

	if out, _ := exec.Command("sh", "-c", "getent passwd | cut -d: -f1; getent group | cut -d: -f1").Output(); strings.Contains(string(out), "bnasit") {
		t.Fatal("Abbruch: Konten mit Präfix bnasit existieren bereits")
	}
	t.Cleanup(func() {
		_ = exec.Command("smbpasswd", "-x", "bnasit-u").Run()
		_ = exec.Command("userdel", "bnasit-u").Run()
		_ = exec.Command("groupdel", "bnasit-g").Run()
	})

	if err := CreateGroup(ctx, d, "bnasit-g"); err != nil {
		t.Fatal(err)
	}
	if err := CreateUser(ctx, d, UserCreate{Name: "bnasit-u", FullName: "Integrationstest", Password: "erstes-passwort", Groups: []string{"bnasit-g"}}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := VerifyPassword(ctx, d.Runner, "bnasit-u", "erstes-passwort"); !ok {
		t.Fatal("Passwort wird nicht akzeptiert")
	}
	if !strings.Contains(sh(t, "pdbedit", "-L"), "bnasit-u") {
		t.Fatal("nicht in der Samba-Datenbank")
	}
	groups := sh(t, "id", "-nG", "bnasit-u")
	if !strings.Contains(groups, "users") || !strings.Contains(groups, "bnasit-g") {
		t.Fatalf("Gruppen: %s", groups)
	}
	if got := sh(t, "id", "-u", "bnasit-u"); got < "10000" || len(got) != 5 {
		t.Fatalf("UID außerhalb des Bereichs: %s", got)
	}

	if err := SetUserPassword(ctx, d, "bnasit-u", "zweites-passwort"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := VerifyPassword(ctx, d.Runner, "bnasit-u", "erstes-passwort"); ok {
		t.Fatal("altes Passwort gilt noch")
	}
	if ok, _ := VerifyPassword(ctx, d.Runner, "bnasit-u", "zweites-passwort"); !ok {
		t.Fatal("neues Passwort gilt nicht")
	}

	if err := UpdateUser(ctx, d, UserUpdate{Name: "bnasit-u", FullName: "Neu", Groups: []string{}, Admin: true}); err != nil {
		t.Fatal(err)
	}
	if g := sh(t, "id", "-nG", "bnasit-u"); strings.Contains(g, "bnasit-g") || !strings.Contains(g, "betternas-admin") {
		t.Fatalf("nach Update: %s", g)
	}
	if err := SetGroupMembers(ctx, d, "bnasit-g", []string{"bnasit-u"}); err != nil {
		t.Fatal(err)
	}
	if err := SetGroupMembers(ctx, d, "bnasit-g", nil); err != nil {
		t.Fatal(err)
	}
	if err := DeleteUser(ctx, d, "bnasit-u"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteGroup(ctx, d, "bnasit-g"); err != nil {
		t.Fatal(err)
	}
	if shOK("getent", "passwd", "bnasit-u") || shOK("getent", "group", "bnasit-g") {
		t.Fatal("Konto oder Gruppe noch vorhanden")
	}
}

func TestSharesLifecycle(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	if _, err := exec.LookPath("smbclient"); err != nil {
		t.Skip("smbclient fehlt (apt install smbclient)")
	}
	ctx := context.Background()
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/")}
	if paths, _ := d.FS.Glob("/etc/samba/betternas.d/*.conf"); len(paths) > 1 {
		t.Skipf("übersprungen: Es gibt bereits Freigaben (%d Dateien)", len(paths))
	}
	if out, _ := exec.Command("sh", "-c", "getent passwd | cut -d: -f1").Output(); strings.Contains(string(out), "bnasit") {
		t.Fatal("Abbruch: Konten mit Präfix bnasit existieren bereits")
	}

	backing, err := os.MkdirTemp("/var/tmp", "bnasit-share")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(backing, 0o755); err != nil { // MkdirTemp legt 0700 an; Samba-Benutzer müssen hineinkommen
		t.Fatal(err)
	}
	pool := "/srv/pools/bnasit"
	if err := os.MkdirAll(pool, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = ApplyShares(ctx, d, shares.State{})
		for _, u := range []string{"bnasit-rw", "bnasit-ro"} {
			_ = exec.Command("smbpasswd", "-x", u).Run()
			_ = exec.Command("userdel", u).Run()
		}
		_ = exec.Command("umount", pool).Run()
		os.Remove(pool)
		os.RemoveAll(backing)
	})
	sh(t, "mergerfs", "-o", "allow_other,fsname=bnasit", backing, pool)

	for _, u := range []string{"bnasit-rw", "bnasit-ro"} {
		if err := CreateUser(ctx, d, UserCreate{Name: u, Password: "test-passwort-" + u}); err != nil {
			t.Fatal(err)
		}
	}
	st := shares.State{Shares: []shares.Share{{
		Name: "bnasit", Pool: "bnasit", Subdir: "daten", Browsable: true, Recycle: true,
		ACL: []shares.ACL{{Type: "user", Name: "bnasit-rw", Access: "rw"}, {Type: "user", Name: "bnasit-ro", Access: "ro"}},
	}}}
	if _, err := ApplyShares(ctx, d, st); err != nil {
		t.Fatal(err)
	}

	smb := func(user, cmd string) (string, error) {
		out, err := exec.Command("smbclient", "//127.0.0.1/bnasit", "-U", user+"%test-passwort-"+user, "-c", cmd).CombinedOutput()
		return string(out), err
	}
	if out, err := smb("bnasit-rw", "put /etc/hostname a.txt"); err != nil {
		t.Fatalf("Schreiben als bnasit-rw: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(backing, "daten", "a.txt")); err != nil {
		t.Fatal("Datei liegt nicht im Pool")
	}
	if out, err := smb("bnasit-ro", "put /etc/hostname b.txt"); err == nil || !strings.Contains(out, "NT_STATUS_ACCESS_DENIED") {
		t.Fatalf("bnasit-ro darf nicht schreiben: %v\n%s", err, out)
	}
	if out, err := smb("bnasit-ro", "ls"); err != nil || !strings.Contains(out, "a.txt") {
		t.Fatalf("bnasit-ro soll lesen können: %v\n%s", err, out)
	}
	if out, err := smb("bnasit-rw", "del a.txt"); err != nil {
		t.Fatalf("löschen: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(backing, "daten", ".papierkorb", "bnasit-rw", "a.txt")); err != nil {
		t.Fatalf("Datei nicht im Papierkorb: %v", err)
	}
	if out, err := smb("nobody", "ls"); err == nil {
		t.Fatalf("anonymer Zugriff muss scheitern:\n%s", out)
	}

	if _, err := ApplyShares(ctx, d, shares.State{}); err != nil {
		t.Fatal(err)
	}
	if out := sh(t, "testparm", "-s"); strings.Contains(out, "[bnasit]") {
		t.Fatal("Freigabe nach dem Entfernen noch in der Konfiguration")
	}
}
