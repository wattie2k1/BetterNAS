package sysexec

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestRootFSConfinesPaths(t *testing.T) {
	tmp := t.TempDir()
	fs := NewRootFS(tmp)
	if err := fs.WriteFile("/etc/x", []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(tmp, "etc", "x")); err != nil || string(b) != "a" {
		t.Fatalf("got %q, %v", b, err)
	}
	if err := fs.WriteFile("/../../etc/passwd", []byte("x"), 0o644); err == nil {
		t.Fatal("expected error for path escaping root")
	}
	if err := fs.WriteFile("../etc/passwd", []byte("x"), 0o644); err == nil {
		t.Fatal("expected error for relative path escaping root")
	}
}

func TestRootFSWriteIsAtomic(t *testing.T) {
	tmp := t.TempDir()
	fs := NewRootFS(tmp)
	if err := fs.WriteFile("/d/f", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(tmp, "d"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestRootFSGlobReturnsRootRelativePaths(t *testing.T) {
	tmp := t.TempDir()
	fs := NewRootFS(tmp)
	_ = fs.WriteFile("/etc/systemd/system/a.mount", nil, 0o644)
	_ = fs.WriteFile("/etc/systemd/system/b.mount", nil, 0o644)
	got, err := fs.Glob("/etc/systemd/system/*.mount")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/etc/systemd/system/a.mount", "/etc/systemd/system/b.mount"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestFakeRunnerRecordsCalls(t *testing.T) {
	f := &FakeRunner{Responses: map[string]Result{
		"udevadm settle": {Stdout: []byte("ok")},
	}}
	ctx := context.Background()
	if _, err := f.Run(ctx, nil, "wipefs", "--all", "/dev/sdb"); err != nil {
		t.Fatal(err)
	}
	r, _ := f.Run(ctx, []byte("in"), "udevadm", "settle")
	if string(r.Stdout) != "ok" {
		t.Fatalf("response not returned: %q", r.Stdout)
	}
	want := []string{"wipefs --all /dev/sdb", "udevadm settle"}
	if !reflect.DeepEqual(f.Calls, want) {
		t.Fatalf("got %v", f.Calls)
	}
	if string(f.Stdins[1]) != "in" {
		t.Fatalf("stdin not recorded")
	}
}

func TestRunnerExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	r, err := NewRunner().Run(context.Background(), nil, "sh", "-c", "echo out; echo err >&2; exit 3")
	if err != nil {
		t.Fatal(err)
	}
	if r.ExitCode != 3 || strings.TrimSpace(string(r.Stdout)) != "out" || strings.TrimSpace(string(r.Stderr)) != "err" {
		t.Fatalf("got %+v", r)
	}
}

func TestRunnerPassesStdin(t *testing.T) {
	r, err := NewRunner().Run(context.Background(), []byte("hallo"), "cat")
	if err != nil || string(r.Stdout) != "hallo" {
		t.Fatalf("got %q %v", r.Stdout, err)
	}
}
