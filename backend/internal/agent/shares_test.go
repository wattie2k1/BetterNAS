package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/shares"
	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/sysexec"
)

func sharesEnv(t *testing.T) (Deps, *sysexec.FakeRunner) {
	t.Helper()
	root := t.TempDir()
	fs := sysexec.NewRootFS(root)
	if err := fs.WriteFile("/etc/samba/smb.conf", []byte(storage.ManagedHeader+"\n[global]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &sysexec.FakeRunner{Responses: map[string]sysexec.Result{}}
	return Deps{Runner: f, FS: fs}, f
}

func oneShare() shares.State {
	return shares.State{Shares: []shares.Share{{
		Name: "Filme", Pool: "media", Subdir: "Filme", Browsable: true,
		ACL: []shares.ACL{{Type: "user", Name: "anna", Access: "rw"}},
	}}}
}

func countCalls(f *sysexec.FakeRunner, prefix string) int {
	n := 0
	for _, c := range f.Calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func TestApplySharesCommands(t *testing.T) {
	d, f := sharesEnv(t)
	res, err := ApplyShares(context.Background(), d, oneShare())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changed) != 2 {
		t.Fatalf("changed: %v", res.Changed)
	}
	assertOrder(t, f,
		"findmnt -t fuse.mergerfs --mountpoint /srv/pools/media",
		"install -d -m 2775 -o root -g users /srv/pools/media/Filme",
		"testparm -s",
		"systemctl is-active smbd",
		"smbcontrol smbd reload-config",
	)
	b, err := d.FS.ReadFile("/etc/samba/betternas.d/filme.conf")
	if err != nil || !strings.Contains(string(b), "path = /srv/pools/media/Filme") {
		t.Fatalf("filme.conf: %v %s", err, b)
	}
}

func TestApplySharesUnchangedDoesNothingElse(t *testing.T) {
	d, f := sharesEnv(t)
	if _, err := ApplyShares(context.Background(), d, oneShare()); err != nil {
		t.Fatal(err)
	}
	before := countCalls(f, "smbcontrol")
	res, err := ApplyShares(context.Background(), d, oneShare())
	if err != nil || len(res.Changed) != 0 {
		t.Fatalf("%v %v", res, err)
	}
	if countCalls(f, "smbcontrol") != before {
		t.Fatal("ohne Änderung darf Samba nicht neu laden")
	}
}

func TestApplySharesRefusesUnmountedPool(t *testing.T) {
	d, f := sharesEnv(t)
	f.Responses["findmnt -t fuse.mergerfs --mountpoint /srv/pools/media"] = sysexec.Result{ExitCode: 1}
	_, err := ApplyShares(context.Background(), d, oneShare())
	if code(err) != apierr.InvalidParams || !strings.Contains(apierr.From(err).Message, "media") {
		t.Fatalf("%v", err)
	}
	if f.Called("install") || d.FS.Exists("/etc/samba/betternas.d/filme.conf") {
		t.Fatal("es darf nichts angelegt werden")
	}
}

func TestApplySharesRollsBackOnTestparmFailure(t *testing.T) {
	d, f := sharesEnv(t)
	if _, err := ApplyShares(context.Background(), d, oneShare()); err != nil {
		t.Fatal(err)
	}
	old, _ := d.FS.ReadFile("/etc/samba/betternas.d/filme.conf")
	reloads := countCalls(f, "smbcontrol")

	changed := oneShare()
	changed.Shares[0].Guest = true
	changed.Shares = append(changed.Shares, shares.Share{Name: "Neu", Pool: "media", Subdir: "Neu", ACL: []shares.ACL{{Type: "user", Name: "anna", Access: "ro"}}})
	f.Responses["testparm -s"] = sysexec.Result{ExitCode: 1, Stderr: []byte("Unknown parameter")}
	_, err := ApplyShares(context.Background(), d, changed)
	if code(err) != apierr.InvalidParams || apierr.From(err).Details["testparm"] == nil {
		t.Fatalf("%v", err)
	}
	now, _ := d.FS.ReadFile("/etc/samba/betternas.d/filme.conf")
	if string(now) != string(old) {
		t.Fatalf("filme.conf nicht zurückgesetzt:\n%s", now)
	}
	if d.FS.Exists("/etc/samba/betternas.d/neu.conf") {
		t.Fatal("neu.conf muss wieder entfernt sein")
	}
	inc, _ := d.FS.ReadFile("/etc/samba/betternas.d/shares.conf")
	if strings.Contains(string(inc), "neu.conf") {
		t.Fatal("shares.conf nicht zurückgesetzt")
	}
	if countCalls(f, "smbcontrol") != reloads {
		t.Fatal("Samba darf nach Fehler nicht neu geladen werden")
	}
}

func TestApplySharesRefusesForeignSmbConf(t *testing.T) {
	d, f := sharesEnv(t)
	_ = d.FS.WriteFile("/etc/samba/smb.conf", []byte("[global]\n   workgroup = X\n"), 0o644)
	_, err := ApplyShares(context.Background(), d, oneShare())
	if code(err) != apierr.Conflict {
		t.Fatalf("%v", err)
	}
	if f.Called("install") || d.FS.Exists("/etc/samba/betternas.d/shares.conf") {
		t.Fatal("es darf nichts geschrieben werden")
	}
}

func TestApplySharesRemovesOnlyManagedFiles(t *testing.T) {
	d, _ := sharesEnv(t)
	if _, err := ApplyShares(context.Background(), d, oneShare()); err != nil {
		t.Fatal(err)
	}
	_ = d.FS.WriteFile("/etc/samba/betternas.d/fremd.conf", []byte("[fremd]\n"), 0o644)
	if _, err := ApplyShares(context.Background(), d, shares.State{}); err != nil {
		t.Fatal(err)
	}
	if d.FS.Exists("/etc/samba/betternas.d/filme.conf") {
		t.Fatal("verwaltete Datei muss entfernt werden")
	}
	if !d.FS.Exists("/etc/samba/betternas.d/fremd.conf") {
		t.Fatal("fremde Datei darf nicht entfernt werden")
	}
}

func TestApplySharesSkipsReloadWhenSmbdInactive(t *testing.T) {
	d, f := sharesEnv(t)
	f.Responses["systemctl is-active smbd"] = sysexec.Result{ExitCode: 3}
	if _, err := ApplyShares(context.Background(), d, oneShare()); err != nil {
		t.Fatal(err)
	}
	if f.Called("smbcontrol") {
		t.Fatal("smbcontrol ohne laufenden smbd")
	}
}
