//go:build integration

package agent

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

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
