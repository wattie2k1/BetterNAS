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

// VMDiskResize vergrößert eine Festplatte der ausgeschalteten VM. Interne qcow2-Snapshots verhindern das
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
	if info.State != "shutoff" {
		return apierr.New(apierr.Conflict, "Die VM muss zum Vergrößern der Festplatte ausgeschaltet sein")
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
	if snaps := mustSnapshots(ctx, d, name); len(snaps) > 0 {
		if !req.DeleteSnapshots {
			names := make([]string, len(snaps))
			for i, s := range snaps {
				names[i] = s.Name
			}
			return apierr.New(apierr.Conflict, "Die VM hat "+strconv.Itoa(len(snaps))+" Snapshot(s) ("+strings.Join(names, ", ")+"); sie müssen zum Vergrößern gelöscht werden")
		}
		for _, s := range snaps {
			if _, err := virshOK(ctx, d, nil, "Der Snapshot „"+s.Name+"“ konnte nicht gelöscht werden", "snapshot-delete", name, s.Name); err != nil {
				return err
			}
		}
	}
	if _, err := run(ctx, d.Runner, nil, "qemu-img", "resize", req.Path, strconv.Itoa(req.SizeGiB)+"G"); err != nil {
		return apierr.New(apierr.Internal, "Die Festplatte konnte nicht vergrößert werden").With("detail", err.Error())
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
