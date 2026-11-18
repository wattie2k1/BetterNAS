package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
	"github.com/betternas/betternas/internal/vm"
)

const (
	nvramOld = "/var/lib/libvirt/qemu/nvram/homeassistant_VARS.fd"
	nvramNew = "/var/lib/libvirt/qemu/nvram/ha_VARS.fd"
	diskNew  = "/srv/vms/disks/ha.qcow2"
)

func renameEnv(t *testing.T, state string) (Deps, *sysexec.FakeRunner, string) {
	t.Helper()
	d, f, root := configEnv(t, state)
	writeFile(t, root, haosDisk)
	writeFile(t, root, nvramOld)
	f.Responses[v+"snapshot-list homeassistant"] = sysexec.Result{Stdout: []byte("")}
	return d, f, root
}

func exists(root, p string) bool {
	_, err := os.Stat(filepath.Join(root, p))
	return err == nil
}

func TestVMRenameWithFiles(t *testing.T) {
	d, f, root := renameEnv(t, "shut off")
	if err := VMRename(context.Background(), d, "homeassistant", vm.Rename{NewName: "ha", RenameFiles: true}); err != nil {
		t.Fatal(err)
	}
	if !exists(root, diskNew) || !exists(root, nvramNew) || exists(root, haosDisk) || exists(root, nvramOld) {
		t.Fatal("Dateien nicht umbenannt")
	}
	assertOrder(t, f, v+"define /dev/stdin", v+"domrename homeassistant ha")
	i := f.Index(v + "define /dev/stdin")
	if x := string(f.Stdins[i]); !strings.Contains(x, diskNew) || !strings.Contains(x, nvramNew) || strings.Contains(x, "homeassistant.qcow2") {
		t.Fatal(x)
	}
}

func TestVMRenameWithoutFiles(t *testing.T) {
	d, f, root := renameEnv(t, "shut off")
	if err := VMRename(context.Background(), d, "homeassistant", vm.Rename{NewName: "ha"}); err != nil {
		t.Fatal(err)
	}
	if !exists(root, haosDisk) || exists(root, diskNew) {
		t.Fatal("Dateien dürfen nicht angefasst werden")
	}
	if f.Called(v+"define") || !f.Called(v+"domrename homeassistant ha") {
		t.Fatal(f.Calls)
	}
}

func TestVMRenameRollback(t *testing.T) {
	d, f, root := renameEnv(t, "shut off")
	f.Responses[v+"domrename"] = sysexec.Result{ExitCode: 1, Stderr: []byte("error: boom")}
	err := VMRename(context.Background(), d, "homeassistant", vm.Rename{NewName: "ha", RenameFiles: true})
	if code(err) != apierr.Internal {
		t.Fatalf("%v", err)
	}
	if !exists(root, haosDisk) || !exists(root, nvramOld) || exists(root, diskNew) || exists(root, nvramNew) {
		t.Fatal("Dateien müssen zurückbenannt sein")
	}
	// Die ursprüngliche Konfiguration wird wiederhergestellt (zweites define).
	n := 0
	for _, c := range f.Calls {
		if c == v+"define /dev/stdin" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("%d define-Aufrufe: %v", n, f.Calls)
	}
}

func TestVMRenameRejects(t *testing.T) {
	ctx := context.Background()
	d, f, root := renameEnv(t, "running")
	if code(VMRename(ctx, d, "homeassistant", vm.Rename{NewName: "ha"})) != apierr.Conflict {
		t.Fatal("laufende VM")
	}
	d, f, root = renameEnv(t, "shut off")
	if code(VMRename(ctx, d, "homeassistant", vm.Rename{NewName: "homeassistant"})) != apierr.InvalidParams {
		t.Fatal("gleicher Name")
	}
	if code(VMRename(ctx, d, "homeassistant", vm.Rename{NewName: "Falsch"})) != apierr.InvalidParams {
		t.Fatal("Name ungültig")
	}
	f.Responses[v+"dominfo ha"] = sysexec.Result{Stdout: []byte("Name: ha\nState: shut off\nAutostart: disable\n")}
	if code(VMRename(ctx, d, "homeassistant", vm.Rename{NewName: "ha"})) != apierr.Conflict {
		t.Fatal("Name vergeben")
	}
	delete(f.Responses, v+"dominfo ha")
	writeFile(t, root, diskNew)
	if code(VMRename(ctx, d, "homeassistant", vm.Rename{NewName: "ha", RenameFiles: true})) != apierr.Conflict {
		t.Fatal("Zieldatei existiert")
	}
	if f.Called(v+"domrename") || !exists(root, haosDisk) {
		t.Fatal("nichts darf verändert werden")
	}
}

func TestVMRenameSnapshots(t *testing.T) {
	d, f, root := renameEnv(t, "shut off")
	f.Responses[v+"snapshot-list homeassistant"] = sysexec.Result{Stdout: []byte(snapTable)}
	req := vm.Rename{NewName: "ha", RenameFiles: true}
	err := VMRename(context.Background(), d, "homeassistant", req)
	if code(err) != apierr.Conflict || !strings.Contains(err.Error(), "s1") || !exists(root, haosDisk) {
		t.Fatalf("%v", err)
	}
	// Ohne Dateien sind Snapshots kein Hindernis.
	if err := VMRename(context.Background(), d, "homeassistant", vm.Rename{NewName: "ha"}); err != nil {
		t.Fatal(err)
	}
	// Mit Bestätigung werden sie gelöscht.
	d, f, _ = renameEnv(t, "shut off")
	f.Responses[v+"snapshot-list homeassistant"] = sysexec.Result{Stdout: []byte(snapTable)}
	req.DeleteSnapshots = true
	if err := VMRename(context.Background(), d, "homeassistant", req); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, v+"snapshot-delete homeassistant s1", v+"define /dev/stdin", v+"domrename")
}
