package agent

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/vm"
)

func imageInfo(ctx context.Context, d Deps, file string) (virtual, actual int64, err error) {
	// -U: auch bei laufender VM lesen (Datei ist dann gesperrt).
	out, err := run(ctx, d.Runner, nil, "qemu-img", "info", "-U", "--output=json", file)
	if err != nil {
		return 0, 0, err
	}
	return vm.ParseImageInfo(out)
}

// VMDisks liefert die Festplatten einer VM mit virtueller und belegter Größe.
func VMDisks(ctx context.Context, d Deps, name string) ([]vm.Disk, error) {
	if err := checkDomain(name); err != nil {
		return nil, err
	}
	if _, exists, err := domainInfo(ctx, d, name); err != nil {
		return nil, err
	} else if !exists {
		return nil, apierr.New(apierr.NotFound, "VM nicht gefunden")
	}
	list := []vm.Disk{}
	for _, f := range domainDisks(ctx, d, name) {
		disk := vm.Disk{Path: f}
		if virtual, actual, err := imageInfo(ctx, d, f); err == nil {
			disk.VirtualSize, disk.UsedBytes = virtual, actual
			disk.SizeGiB = int((virtual + vm.GiB - 1) / vm.GiB)
		}
		list = append(list, disk)
	}
	return list, nil
}

// clearSnapshots verlangt bei vorhandenen Snapshots die Bestätigung (allow) und löscht sie dann.
// Interne qcow2-Snapshots vertragen sich weder mit dem Vergrößern noch mit geänderten Festplatten.
func clearSnapshots(ctx context.Context, d Deps, name string, allow bool, purpose string) error {
	snaps := mustSnapshots(ctx, d, name)
	if len(snaps) == 0 {
		return nil
	}
	if !allow {
		names := make([]string, len(snaps))
		for i, s := range snaps {
			names[i] = s.Name
		}
		return apierr.New(apierr.Conflict, "Die VM hat "+strconv.Itoa(len(snaps))+" Snapshot(s) ("+strings.Join(names, ", ")+"); sie müssen "+purpose+" gelöscht werden")
	}
	for _, s := range snaps {
		if _, err := virshOK(ctx, d, nil, "Der Snapshot „"+s.Name+"“ konnte nicht gelöscht werden", "snapshot-delete", name, s.Name); err != nil {
			return err
		}
	}
	return nil
}

// VMDiskResize vergrößert eine Festplatte, bei ausgeschalteter VM per qemu-img, bei laufender per blockresize. Interne qcow2-Snapshots verhindern das
// Vergrößern; sie werden nur mit DeleteSnapshots gelöscht, sonst meldet der Agent einen Konflikt.
func VMDiskResize(ctx context.Context, d Deps, name string, req vm.DiskResize) error {
	if err := checkDomain(name); err != nil {
		return err
	}
	if err := req.Validate(); err != nil {
		return err
	}
	mutate.Lock()
	defer mutate.Unlock()
	info, exists, err := domainInfo(ctx, d, name)
	if err != nil {
		return err
	}
	if !exists {
		return apierr.New(apierr.NotFound, "VM nicht gefunden")
	}
	live := info.State == "running" || info.State == "paused"
	if !live && info.State != "shutoff" {
		return apierr.New(apierr.Conflict, "Die VM muss zum Vergrößern der Festplatte laufen oder ausgeschaltet sein")
	}
	owned := false
	for _, f := range domainDisks(ctx, d, name) {
		if f == req.Path {
			owned = true
		}
	}
	if !owned {
		return apierr.New(apierr.NotFound, "Die Festplatte gehört nicht zu dieser VM")
	}
	virtual, _, err := imageInfo(ctx, d, req.Path)
	if err != nil {
		return apierr.New(apierr.Internal, "Die Größe der Festplatte konnte nicht gelesen werden").With("detail", err.Error())
	}
	if int64(req.SizeGiB)*vm.GiB <= virtual {
		return apierr.New(apierr.InvalidParams, "Die neue Größe muss über der aktuellen liegen (Verkleinern ist nicht möglich)")
	}
	if err := clearSnapshots(ctx, d, name, req.DeleteSnapshots, "zum Vergrößern"); err != nil {
		return err
	}
	if live {
		// Laufende VM: QEMU vergrößert das Image im Betrieb, der Gast sieht die neue Größe sofort.
		_, err := virshOK(ctx, d, nil, "Die Festplatte konnte nicht vergrößert werden", "blockresize", name, req.Path, strconv.Itoa(req.SizeGiB)+"G")
		return err
	}
	if _, err := run(ctx, d.Runner, nil, "qemu-img", "resize", req.Path, strconv.Itoa(req.SizeGiB)+"G"); err != nil {
		return apierr.New(apierr.Internal, "Die Festplatte konnte nicht vergrößert werden").With("detail", err.Error())
	}
	return nil
}

// shutoffDomain prüft, dass die VM existiert und ausgeschaltet ist.
func shutoffDomain(ctx context.Context, d Deps, name, action string) error {
	info, exists, err := domainInfo(ctx, d, name)
	if err != nil {
		return err
	}
	if !exists {
		return apierr.New(apierr.NotFound, "VM nicht gefunden")
	}
	if info.State != "shutoff" {
		return apierr.New(apierr.Conflict, "Die VM muss zum "+action+" ausgeschaltet sein")
	}
	return nil
}

// VMDiskAdd legt eine leere qcow2-Festplatte an und hängt sie an die ausgeschaltete VM.
// Es liefert den Pfad der neuen Datei.
func VMDiskAdd(ctx context.Context, d Deps, name string, req vm.DiskAdd) (string, error) {
	if err := checkDomain(name); err != nil {
		return "", err
	}
	if err := req.Validate(); err != nil {
		return "", err
	}
	mutate.Lock()
	defer mutate.Unlock()
	if err := shutoffDomain(ctx, d, name, "Hinzufügen einer Festplatte"); err != nil {
		return "", err
	}
	res, err := virshOK(ctx, d, nil, "Die VM-Konfiguration konnte nicht gelesen werden", "dumpxml", "--inactive", name)
	if err != nil {
		return "", err
	}
	xml := string(res.Stdout)
	dev, bus, err := vm.NextDiskTarget(xml)
	if err != nil {
		return "", err
	}
	file := ""
	for n := 2; n < 100; n++ {
		f := vm.ExtraDiskPath(name, n)
		if !d.FS.Exists(f) && !strings.Contains(xml, "'"+f+"'") {
			file = f
			break
		}
	}
	if file == "" {
		return "", apierr.New(apierr.Conflict, "Kein freier Dateiname für die Festplatte")
	}
	newXML, err := vm.AddDiskXML(xml, file, dev, bus)
	if err != nil {
		return "", err
	}
	if err := d.FS.MkdirAll(vm.DiskDir, 0o755); err != nil {
		return "", err
	}
	if _, err := run(ctx, d.Runner, nil, "qemu-img", "create", "-f", "qcow2", file, strconv.Itoa(req.SizeGiB)+"G"); err != nil {
		return "", apierr.New(apierr.Internal, "Die Festplatte konnte nicht angelegt werden").With("detail", err.Error())
	}
	if err := clearSnapshots(ctx, d, name, req.DeleteSnapshots, "zum Hinzufügen einer Festplatte"); err != nil {
		_ = d.FS.Remove(file)
		return "", err
	}
	if _, err := virshOK(ctx, d, []byte(newXML), "Die Festplatte konnte nicht hinzugefügt werden", "define", "/dev/stdin"); err != nil {
		_ = d.FS.Remove(file)
		return "", err
	}
	return file, nil
}

// VMDiskRemove trennt eine Festplatte von der ausgeschalteten VM, auf Wunsch wird die Datei gelöscht.
// Die Systemfestplatte (die erste) lässt sich nicht entfernen.
func VMDiskRemove(ctx context.Context, d Deps, name string, req vm.DiskRemove) error {
	if err := checkDomain(name); err != nil {
		return err
	}
	if err := req.Validate(); err != nil {
		return err
	}
	mutate.Lock()
	defer mutate.Unlock()
	if err := shutoffDomain(ctx, d, name, "Entfernen einer Festplatte"); err != nil {
		return err
	}
	res, err := virshOK(ctx, d, nil, "Die VM-Konfiguration konnte nicht gelesen werden", "dumpxml", "--inactive", name)
	if err != nil {
		return err
	}
	newXML, err := vm.RemoveDiskXML(string(res.Stdout), req.Path)
	if err != nil {
		return err
	}
	if err := clearSnapshots(ctx, d, name, req.DeleteSnapshots, "zum Entfernen einer Festplatte"); err != nil {
		return err
	}
	if _, err := virshOK(ctx, d, []byte(newXML), "Die Festplatte konnte nicht entfernt werden", "define", "/dev/stdin"); err != nil {
		return err
	}
	if req.DeleteFile && d.FS.Exists(req.Path) {
		if err := d.FS.Remove(req.Path); err != nil {
			return apierr.New(apierr.Internal, "Die Festplatte wurde getrennt, die Datei konnte aber nicht gelöscht werden").With("detail", err.Error())
		}
	}
	return nil
}

func registerVMDisk(s *rpc.Server, d Deps) {
	s.Handle("vm.disks", func(ctx context.Context, p json.RawMessage) (any, error) {
		var r struct {
			Name string `json:"name"`
		}
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		return VMDisks(ctx, d, r.Name)
	})
	s.Handle("vm.disk.add", func(ctx context.Context, p json.RawMessage) (any, error) {
		var r struct {
			Name string `json:"name"`
			vm.DiskAdd
		}
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		file, err := VMDiskAdd(ctx, d, r.Name, r.DiskAdd)
		return map[string]string{"path": file}, err
	})
	s.Handle("vm.disk.remove", func(ctx context.Context, p json.RawMessage) (any, error) {
		var r struct {
			Name string `json:"name"`
			vm.DiskRemove
		}
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		return struct{}{}, VMDiskRemove(ctx, d, r.Name, r.DiskRemove)
	})
	s.Handle("vm.disk.resize", func(ctx context.Context, p json.RawMessage) (any, error) {
		var r struct {
			Name string `json:"name"`
			vm.DiskResize
		}
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		return struct{}{}, VMDiskResize(ctx, d, r.Name, r.DiskResize)
	})
}
