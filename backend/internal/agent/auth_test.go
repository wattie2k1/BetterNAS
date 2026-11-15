package agent

import (
	"context"
	"testing"

	"github.com/betternas/betternas/internal/sysexec"
)

func TestVerifyPasswordInvokesUnixChkpwd(t *testing.T) {
	f := &sysexec.FakeRunner{Responses: map[string]sysexec.Result{}}
	ok, err := VerifyPassword(context.Background(), f, "alice", "geheim")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(f.Calls) != 1 || f.Calls[0] != "/usr/sbin/unix_chkpwd alice nonull" || string(f.Stdins[0]) != "geheim\x00" {
		t.Fatalf("calls %v stdin %q", f.Calls, f.Stdins)
	}
	f.Responses["/usr/sbin/unix_chkpwd alice nonull"] = sysexec.Result{ExitCode: 7}
	ok, err = VerifyPassword(context.Background(), f, "alice", "falsch")
	if err != nil || ok {
		t.Fatalf("wrong password: ok=%v err=%v", ok, err)
	}
}

func TestVerifyRejectsBadUsername(t *testing.T) {
	for _, u := range []string{"../x", "", "-rf", "Root", "a b"} {
		f := &sysexec.FakeRunner{}
		ok, err := VerifyPassword(context.Background(), f, u, "pw")
		if ok || err != nil || len(f.Calls) != 0 {
			t.Fatalf("%q: ok=%v err=%v calls=%v", u, ok, err, f.Calls)
		}
	}
}
