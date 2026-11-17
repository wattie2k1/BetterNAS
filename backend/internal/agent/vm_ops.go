package agent

import (
	"context"
	"encoding/json"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/vm"
)

// VMInterfaces liefert die physischen Netzwerkschnittstellen (ohne WLAN), die für ein LAN-Netz (macvtap) taugen.
func VMInterfaces(_ context.Context, d Deps) ([]string, error) {
	list := []string{}
	devs, err := d.FS.Glob("/sys/class/net/*/device")
	if err != nil {
		return nil, err
	}
	for _, dev := range devs {
		name := path.Base(path.Dir(dev))
		if !vm.InterfaceRe.MatchString(name) || d.FS.Exists("/sys/class/net/"+name+"/wireless") {
			continue
		}
		list = append(list, name)
	}
	sort.Strings(list)
	return list, nil
}

// checkNetwork prüft, dass ein libvirt-Netz aktiv bzw. die Schnittstelle eines LAN-Netzes vorhanden ist.
func checkNetwork(ctx context.Context, d Deps, network string) error {
	var have []string
	var err error
	if ifc, ok := vm.DirectInterface(network); ok {
		if have, err = VMInterfaces(ctx, d); err != nil {
			return err
		}
		network = ifc
	} else if have, err = VMNetworks(ctx, d); err != nil {
		return err
	}
	for _, n := range have {
		if n == network {
			return nil
		}
	}
	return apierr.New(apierr.InvalidParams, "Das Netzwerk ist nicht verfügbar: "+network)
}

func checkSnapshot(name, snap string) error {
	if err := checkDomain(name); err != nil {
		return err
	}
	if !vm.SnapshotRe.MatchString(snap) {
		return apierr.New(apierr.InvalidParams, "Ungültiger Snapshot-Name")
	}
	return nil
}

// VMSnapshots liefert die Snapshots einer VM.
func VMSnapshots(ctx context.Context, d Deps, name string) ([]vm.Snapshot, error) {
	if err := checkDomain(name); err != nil {
		return nil, err
	}
	res, err := virshOK(ctx, d, nil, "Snapshots konnten nicht gelesen werden", "snapshot-list", name)
	if err != nil {
		return nil, err
	}
	return vm.ParseSnapshots(res.Stdout), nil
}

// VMSnapshotCreate legt einen Snapshot an.
func VMSnapshotCreate(ctx context.Context, d Deps, name, snap string) error {
	if err := checkSnapshot(name, snap); err != nil {
		return err
	}
	mutate.Lock()
	defer mutate.Unlock()
	for _, s := range mustSnapshots(ctx, d, name) {
		if s.Name == snap {
			return apierr.New(apierr.Conflict, "Ein Snapshot mit diesem Namen existiert bereits")
		}
	}
	_, err := virshOK(ctx, d, nil, "Der Snapshot konnte nicht angelegt werden", "snapshot-create-as", name, snap, "--atomic")
	return err
}

func mustSnapshots(ctx context.Context, d Deps, name string) []vm.Snapshot {
	list, _ := VMSnapshots(ctx, d, name)
	return list
}

// VMSnapshotRevert setzt die VM auf einen Snapshot zurück.
func VMSnapshotRevert(ctx context.Context, d Deps, name, snap string) error {
	if err := checkSnapshot(name, snap); err != nil {
		return err
	}
	mutate.Lock()
	defer mutate.Unlock()
	_, err := virshOK(ctx, d, nil, "Das Zurücksetzen ist fehlgeschlagen", "snapshot-revert", name, snap)
	return err
}

// VMSnapshotDelete löscht einen Snapshot.
func VMSnapshotDelete(ctx context.Context, d Deps, name, snap string) error {
	if err := checkSnapshot(name, snap); err != nil {
		return err
	}
	mutate.Lock()
	defer mutate.Unlock()
	_, err := virshOK(ctx, d, nil, "Der Snapshot konnte nicht gelöscht werden", "snapshot-delete", name, snap)
	return err
}

// VMClone klont eine ausgeschaltete VM (virt-clone) mit neuen Festplatten unter /srv/vms/disks.
func VMClone(ctx context.Context, d Deps, name, newName string) error {
	if err := checkDomain(name); err != nil {
		return err
	}
	if !vm.NameRe.MatchString(newName) {
		return apierr.New(apierr.InvalidParams, "Ungültiger Name der Kopie (erlaubt: Kleinbuchstaben, Ziffern, - und _, bis 40 Zeichen)")
	}
	mutate.Lock()
	defer mutate.Unlock()
	src, exists, err := domainInfo(ctx, d, name)
	if err != nil {
		return err
	}
	if !exists {
		return apierr.New(apierr.NotFound, "VM nicht gefunden")
	}
	if src.State != "shutoff" {
		return apierr.New(apierr.Conflict, "Die VM muss zum Klonen ausgeschaltet sein")
	}
	if _, taken, err := domainInfo(ctx, d, newName); err != nil {
		return err
	} else if taken {
		return apierr.New(apierr.Conflict, "Eine VM mit diesem Namen existiert bereits")
	}
	disks := domainDisks(ctx, d, name)
	if len(disks) == 0 {
		return apierr.New(apierr.Conflict, "Die VM hat keine Festplatte zum Klonen")
	}
	args := []string{"--connect", virshURI, "--original", name, "--name", newName}
	var created []string
	for i, f := range disks {
		if !strings.HasPrefix(path.Clean(f), vm.DiskDir+"/") {
			return apierr.New(apierr.Conflict, "Die Festplatte liegt nicht unter "+vm.DiskDir+" und wird nicht geklont")
		}
		target := vm.DiskPath(newName)
		if i > 0 {
			target = path.Join(vm.DiskDir, newName+"-"+strconv.Itoa(i+1)+".qcow2")
		}
		if d.FS.Exists(target) {
			return apierr.New(apierr.Conflict, "Die Festplattendatei existiert bereits")
		}
		args = append(args, "--file", target)
		created = append(created, target)
	}
	if _, err := run(ctx, d.Runner, nil, "virt-clone", args...); err != nil {
		for _, f := range created {
			_ = d.FS.Remove(f)
		}
		return apierr.New(apierr.Internal, "Das Klonen ist fehlgeschlagen").With("detail", err.Error())
	}
	return nil
}

// VMDisplay liefert den lokalen VNC-Port einer laufenden VM.
func VMDisplay(ctx context.Context, d Deps, name string) (int, error) {
	if err := checkDomain(name); err != nil {
		return 0, err
	}
	v, exists, err := domainInfo(ctx, d, name)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, apierr.New(apierr.NotFound, "VM nicht gefunden")
	}
	if v.State != "running" && v.State != "paused" {
		return 0, apierr.New(apierr.Conflict, "Die VM läuft nicht")
	}
	res, err := virshOK(ctx, d, nil, "Die Anzeige ist nicht verfügbar", "domdisplay", name)
	if err != nil {
		return 0, err
	}
	port, ok := vm.ParseDisplayPort(res.Stdout)
	if !ok {
		return 0, apierr.New(apierr.Conflict, "Die VM hat keine VNC-Anzeige")
	}
	return port, nil
}

// VMISODelete löscht eine ISO- oder Image-Datei, solange keine VM sie noch einbindet.
func VMISODelete(ctx context.Context, d Deps, file string) error {
	if !vm.ISORe.MatchString(file) && !vm.ImageRe.MatchString(file) {
		return apierr.New(apierr.InvalidParams, "Ungültiger Dateiname")
	}
	mutate.Lock()
	defer mutate.Unlock()
	full := vm.ISOPath(file)
	if !d.FS.Exists(full) {
		return apierr.New(apierr.NotFound, "Datei nicht gefunden")
	}
	if res, err := virsh(ctx, d, nil, "list", "--all", "--name"); err == nil && res.ExitCode == 0 {
		names, _ := vm.ParseNetworks(res.Stdout)
		for _, n := range names {
			if !vm.DomainRe.MatchString(n) {
				continue
			}
			if x, err := virsh(ctx, d, nil, "dumpxml", "--inactive", n); err == nil && strings.Contains(string(x.Stdout), "'"+full+"'") {
				return apierr.New(apierr.Conflict, "Die Datei wird von der VM „"+n+"“ verwendet")
			}
		}
	}
	return d.FS.Remove(full)
}

func registerVMOps(s *rpc.Server, d Deps) {
	type nameReq struct {
		Name     string `json:"name"`
		Snapshot string `json:"snapshot"`
		NewName  string `json:"newName"`
		File     string `json:"file"`
	}
	read := func(p json.RawMessage) (nameReq, error) {
		var r nameReq
		return r, decode(p, &r)
	}
	s.Handle("vm.interfaces", func(ctx context.Context, _ json.RawMessage) (any, error) { return VMInterfaces(ctx, d) })
	s.Handle("vm.snapshots", func(ctx context.Context, p json.RawMessage) (any, error) {
		r, err := read(p)
		if err != nil {
			return nil, err
		}
		return VMSnapshots(ctx, d, r.Name)
	})
	for method, fn := range map[string]func(context.Context, Deps, string, string) error{
		"vm.snapshot.create": VMSnapshotCreate, "vm.snapshot.revert": VMSnapshotRevert, "vm.snapshot.delete": VMSnapshotDelete,
	} {
		s.Handle(method, func(ctx context.Context, p json.RawMessage) (any, error) {
			r, err := read(p)
			if err != nil {
				return nil, err
			}
			return struct{}{}, fn(ctx, d, r.Name, r.Snapshot)
		})
	}
	s.Handle("vm.clone", func(ctx context.Context, p json.RawMessage) (any, error) {
		r, err := read(p)
		if err != nil {
			return nil, err
		}
		return struct{}{}, VMClone(ctx, d, r.Name, r.NewName)
	})
	s.Handle("vm.display", func(ctx context.Context, p json.RawMessage) (any, error) {
		r, err := read(p)
		if err != nil {
			return nil, err
		}
		port, err := VMDisplay(ctx, d, r.Name)
		return map[string]int{"port": port}, err
	})
	s.Handle("vm.iso.delete", func(ctx context.Context, p json.RawMessage) (any, error) {
		r, err := read(p)
		if err != nil {
			return nil, err
		}
		return struct{}{}, VMISODelete(ctx, d, r.File)
	})
}
