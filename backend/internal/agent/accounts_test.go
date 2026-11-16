package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
)

const passwdFixture = `root:x:0:0:root:/root:/bin/bash
wattie:x:1000:1000:André:/home/wattie:/bin/bash
anna:x:10000:100:Anna:/nonexistent:/usr/sbin/nologin
betternas:x:995:988::/var/lib/betternas:/usr/sbin/nologin
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
`
const groupFixture = `root:x:0:
sudo:x:27:wattie
docker:x:990:wattie
users:x:100:
wattie:x:1000:
betternas-admin:x:988:wattie
familie:x:10000:anna,wattie
kinder:x:10001:
`

func accountsEnv(t *testing.T) (Deps, *sysexec.FakeRunner) {
	t.Helper()
	root := t.TempDir()
	fs := sysexec.NewRootFS(root)
	if err := fs.WriteFile("/etc/passwd", []byte(passwdFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile("/etc/group", []byte(groupFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &sysexec.FakeRunner{Responses: map[string]sysexec.Result{}}
	return Deps{Runner: f, FS: fs}, f
}

func hasCall(f *sysexec.FakeRunner, want string) bool {
	for _, c := range f.Calls {
		if c == want {
			return true
		}
	}
	return false
}

func TestCreateUserCommands(t *testing.T) {
	d, f := accountsEnv(t)
	err := CreateUser(context.Background(), d, UserCreate{Name: "bob", FullName: "Bob B", Password: "geheim123", Groups: []string{"kinder"}, Admin: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"useradd -K UID_MIN=10000 -K UID_MAX=59999 -M -d /nonexistent -s /usr/sbin/nologin -N -g users -c Bob B -G kinder,betternas-admin bob",
		"chpasswd",
		"smbpasswd -a -s bob",
	}
	for _, w := range want {
		if !hasCall(f, w) {
			t.Errorf("Aufruf fehlt: %q\nvorhanden: %q", w, f.Calls)
		}
	}
	if string(f.Stdins[f.Index("chpasswd")]) != "bob:geheim123\n" {
		t.Errorf("chpasswd-stdin: %q", f.Stdins[f.Index("chpasswd")])
	}
	if string(f.Stdins[f.Index("smbpasswd")]) != "geheim123\ngeheim123\n" {
		t.Errorf("smbpasswd-stdin: %q", f.Stdins[f.Index("smbpasswd")])
	}
}

func TestCreateUserRollsBackOnSmbpasswdFailure(t *testing.T) {
	d, f := accountsEnv(t)
	f.Responses["smbpasswd -a -s bob"] = sysexec.Result{ExitCode: 1, Stderr: []byte("kaputt")}
	if err := CreateUser(context.Background(), d, UserCreate{Name: "bob", Password: "geheim123"}); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if !hasCall(f, "userdel bob") {
		t.Fatalf("userdel fehlt: %q", f.Calls)
	}
}

func TestCreateUserRejectsExistingSystemName(t *testing.T) {
	d, f := accountsEnv(t)
	for _, name := range []string{"docker", "users", "anna", "familie"} {
		err := CreateUser(context.Background(), d, UserCreate{Name: name, Password: "geheim123"})
		if code(err) != apierr.Conflict && code(err) != apierr.InvalidParams {
			t.Errorf("%s: %v", name, err)
		}
	}
	if f.Called("useradd") {
		t.Fatal("useradd darf nicht aufgerufen werden")
	}
	if code(CreateUser(context.Background(), d, UserCreate{Name: "dave", Password: "geheim123", Groups: []string{"docker"}})) != apierr.InvalidParams {
		t.Fatal("Systemgruppe als Zielgruppe muss abgelehnt werden")
	}
}

func TestUpdateUserKeepsForeignGroups(t *testing.T) {
	d, f := accountsEnv(t)
	if err := UpdateUser(context.Background(), d, UserUpdate{Name: "wattie", FullName: "André W", Groups: []string{"kinder"}, Admin: false}); err != nil {
		t.Fatal(err)
	}
	// sudo und docker bleiben, betternas-admin und familie entfallen, kinder kommt dazu.
	want := "usermod -c André W -G sudo,docker,kinder wattie"
	if !hasCall(f, want) {
		t.Fatalf("erwartet %q, bekommen %q", want, f.Calls)
	}
}

func TestUpdateUserAddsAdmin(t *testing.T) {
	d, f := accountsEnv(t)
	if err := UpdateUser(context.Background(), d, UserUpdate{Name: "anna", Groups: []string{"familie"}, Admin: true}); err != nil {
		t.Fatal(err)
	}
	if !hasCall(f, "usermod -c Anna -G familie,betternas-admin anna") && !hasCall(f, "usermod -c  -G familie,betternas-admin anna") {
		t.Fatalf("%q", f.Calls)
	}
}

func TestDeleteRefusesSystemAccounts(t *testing.T) {
	d, f := accountsEnv(t)
	for _, name := range []string{"root", "betternas", "nobody"} {
		if code(DeleteUser(context.Background(), d, name)) != apierr.Forbidden {
			t.Errorf("%s: forbidden erwartet", name)
		}
	}
	if code(DeleteUser(context.Background(), d, "unbekannt")) != apierr.NotFound {
		t.Error("not_found erwartet")
	}
	if f.Called("userdel") {
		t.Fatal("userdel darf nicht aufgerufen werden")
	}
}

func TestDeleteUserCommands(t *testing.T) {
	d, f := accountsEnv(t)
	if err := DeleteUser(context.Background(), d, "anna"); err != nil {
		t.Fatal(err)
	}
	if f.Index("smbpasswd -x anna") < 0 || f.Index("userdel anna") < f.Index("smbpasswd -x anna") {
		t.Fatalf("%q", f.Calls)
	}
}

func TestGroupOpsOnlyManagedRange(t *testing.T) {
	d, f := accountsEnv(t)
	ctx := context.Background()
	if code(DeleteGroup(ctx, d, "docker")) != apierr.Forbidden {
		t.Error("docker löschen")
	}
	if code(SetGroupMembers(ctx, d, "docker", []string{"anna"})) != apierr.Forbidden {
		t.Error("docker Mitglieder")
	}
	if code(CreateGroup(ctx, d, "docker")) != apierr.Conflict && code(CreateGroup(ctx, d, "docker")) != apierr.InvalidParams {
		t.Error("docker anlegen")
	}
	if err := CreateGroup(ctx, d, "freunde"); err != nil {
		t.Fatal(err)
	}
	if !hasCall(f, "groupadd -K GID_MIN=10000 -K GID_MAX=59999 freunde") {
		t.Fatalf("%q", f.Calls)
	}
	if err := SetGroupMembers(ctx, d, "kinder", []string{"anna", "wattie"}); err != nil {
		t.Fatal(err)
	}
	if !hasCall(f, "gpasswd -M anna,wattie kinder") {
		t.Fatalf("%q", f.Calls)
	}
	if code(SetGroupMembers(ctx, d, "kinder", []string{"root"})) != apierr.InvalidParams {
		t.Error("Systembenutzer als Mitglied")
	}
	if err := DeleteGroup(ctx, d, "kinder"); err != nil || !hasCall(f, "groupdel kinder") {
		t.Fatal(err, f.Calls)
	}
}

func TestPasswordNeverInArgs(t *testing.T) {
	d, f := accountsEnv(t)
	ctx := context.Background()
	_ = CreateUser(ctx, d, UserCreate{Name: "bob", Password: "streng-geheim-1"})
	_ = SetUserPassword(ctx, d, "anna", "streng-geheim-2")
	for _, c := range f.Calls {
		if strings.Contains(c, "streng-geheim") {
			t.Errorf("Passwort in Argumenten: %q", c)
		}
	}
	if code(SetUserPassword(ctx, d, "root", "streng-geheim-3")) != apierr.Forbidden {
		t.Error("root-Passwort darf nicht gesetzt werden")
	}
}

func TestListAccounts(t *testing.T) {
	d, _ := accountsEnv(t)
	acc, err := ListAccounts(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if len(acc.Users) != 2 || len(acc.Groups) != 2 {
		t.Fatalf("%+v", acc)
	}
}
