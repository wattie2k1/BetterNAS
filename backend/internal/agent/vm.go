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
	"github.com/betternas/betternas/internal/sysexec"
	"github.com/betternas/betternas/internal/vm"
)

const virshURI = "qemu:///system"

func virsh(ctx context.Context, d Deps, stdin []byte, args ...string) (sysexec.Result, error) {
	return d.Runner.Run(ctx, stdin, "virsh", append([]string{"-c", virshURI}, args...)...)
}

// virshOK führt virsh aus und macht aus einem Fehlschlag einen apierr mit stderr als Detail.
func virshOK(ctx context.Context, d Deps, stdin []byte, msg string, args ...string) (sysexec.Result, error) {
	res, err := virsh(ctx, d, stdin, args...)
	if err != nil {
		return res, apierr.New(apierr.Internal, "libvirt ist nicht verfügbar")
	}
	if res.ExitCode != 0 {
		return res, apierr.New(apierr.Internal, msg).With("detail", strings.TrimSpace(string(res.Stderr)))
	}
	return res, nil
}

func checkDomain(name string) error {
	if !vm.DomainRe.MatchString(name) {
		return apierr.New(apierr.InvalidParams, "Ungültiger VM-Name")
	}
	return nil
}

// VMStatus meldet, ob libvirt installiert und erreichbar ist und ob KVM zur Verfügung steht.
func VMStatus(ctx context.Context, d Deps) (vm.Status, error) {
	var st vm.Status
	if res, err := d.Runner.Run(ctx, nil, "virsh", "--version"); err != nil || res.ExitCode != 0 {
		return st, nil
	}
	st.Installed = true
	st.KVM = d.FS.Exists("/dev/kvm")
	if res, err := virsh(ctx, d, nil, "list", "--name"); err == nil && res.ExitCode == 0 {
		st.Active = true
	}
	return st, nil
}

func domainInfo(ctx context.Context, d Deps, name string) (vm.VM, bool, error) {
	res, err := virsh(ctx, d, nil, "dominfo", name)
	if err != nil {
		return vm.VM{}, false, apierr.New(apierr.Internal, "libvirt ist nicht verfügbar")
	}
	if res.ExitCode != 0 {
		return vm.VM{}, false, nil
	}
	v, err := vm.ParseDomInfo(res.Stdout)
	return v, err == nil, err
}

func domainDisks(ctx context.Context, d Deps, name string) []string {
	res, err := virsh(ctx, d, nil, "dumpxml", "--inactive", name)
	if err != nil || res.ExitCode != 0 {
		return []string{}
	}
	disks, err := vm.ParseDisks(res.Stdout)
	if err != nil {
		return []string{}
	}
	return disks
}

// ListVMs liefert alle definierten VMs. Ist libvirt nicht erreichbar, ist die Liste leer.
func ListVMs(ctx context.Context, d Deps) ([]vm.VM, error) {
	list := []vm.VM{}
	res, err := virsh(ctx, d, nil, "list", "--all", "--name")
	if err != nil || res.ExitCode != 0 {
		return list, nil
	}
	names, _ := vm.ParseNetworks(res.Stdout) // gleiches Format: ein Name je Zeile
	for _, name := range names {
		if !vm.DomainRe.MatchString(name) {
			continue
		}
		v, ok, err := domainInfo(ctx, d, name)
		if err != nil || !ok {
			continue
		}
		v.Name = name
		v.Disks = domainDisks(ctx, d, name)
		list = append(list, v)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// VMNetworks liefert die aktiven libvirt-Netze.
func VMNetworks(ctx context.Context, d Deps) ([]string, error) {
	res, err := virsh(ctx, d, nil, "net-list", "--name")
	if err != nil || res.ExitCode != 0 {
		return []string{}, nil
	}
	return vm.ParseNetworks(res.Stdout)
}

// VMISOs liefert die ISO- und qcow2-Dateinamen unter /srv/vms/iso.
func VMISOs(_ context.Context, d Deps) ([]string, error) {
	list := []string{}
	for _, pattern := range []string{"*.iso", "*.qcow2"} {
		files, err := d.FS.Glob(path.Join(vm.ISODir, pattern))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if b := path.Base(f); vm.ISORe.MatchString(b) || vm.ImageRe.MatchString(b) {
				list = append(list, b)
			}
		}
	}
	sort.Strings(list)
	return list, nil
}

// VMCreate legt Disk und Domain an. Schlägt das Definieren fehl, wird die neue Disk wieder entfernt.
func VMCreate(ctx context.Context, d Deps, spec vm.Spec, start bool) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	mutate.Lock()
	defer mutate.Unlock()
	if _, exists, err := domainInfo(ctx, d, spec.Name); err != nil {
		return err
	} else if exists {
		return apierr.New(apierr.Conflict, "Eine VM mit diesem Namen existiert bereits")
	}
	disk := vm.DiskPath(spec.Name)
	if d.FS.Exists(disk) {
		return apierr.New(apierr.Conflict, "Die Festplattendatei existiert bereits")
	}
	for _, iso := range []string{spec.ISO, spec.DriverISO} {
		if iso != "" && !d.FS.Exists(vm.ISOPath(iso)) {
			return apierr.New(apierr.NotFound, "ISO-Datei nicht gefunden: "+iso)
		}
	}
	nets, err := VMNetworks(ctx, d)
	if err != nil {
		return err
	}
	found := false
	for _, n := range nets {
		found = found || n == spec.Network
	}
	if !found {
		return apierr.New(apierr.InvalidParams, "Das Netzwerk ist nicht aktiv: "+spec.Network)
	}
	xml, err := vm.BuildXML(spec, d.FS.Exists("/dev/kvm"))
	if err != nil {
		return err
	}
	if err := d.FS.MkdirAll(vm.DiskDir, 0o755); err != nil {
		return err
	}
	// Ein fertiges Image wird kopiert (das Original bleibt unverändert); sonst entsteht eine leere Disk.
	diskArgs := []string{"create", "-f", "qcow2", disk, strconv.Itoa(spec.DiskGiB) + "G"}
	if spec.IsImage() {
		diskArgs = []string{"convert", "-O", "qcow2", vm.ISOPath(spec.ISO), disk}
	}
	if _, err := run(ctx, d.Runner, nil, "qemu-img", diskArgs...); err != nil {
		return apierr.New(apierr.Internal, "Die Festplatte konnte nicht angelegt werden").With("detail", err.Error())
	}
	if _, err := virshOK(ctx, d, []byte(xml), "Die VM konnte nicht angelegt werden", "define", "/dev/stdin"); err != nil {
		_ = d.FS.Remove(disk)
		return err
	}
	if start {
		_, err = virshOK(ctx, d, nil, "Die VM konnte nicht gestartet werden", "start", spec.Name)
		return err
	}
	return nil
}

// VMAction führt eine Aktion aus der Whitelist aus.
func VMAction(ctx context.Context, d Deps, name, action string) error {
	if err := checkDomain(name); err != nil {
		return err
	}
	if !vm.Actions[action] {
		return apierr.New(apierr.InvalidParams, "Unbekannte VM-Aktion")
	}
	args := []string{action, name}
	if action == "noautostart" {
		args = []string{"autostart", "--disable", name}
	}
	_, err := virshOK(ctx, d, nil, "Die Aktion ist fehlgeschlagen", args...)
	return err
}

// VMDelete entfernt eine ausgeschaltete VM; mit deleteDisks auch ihre Festplatten unter /srv/vms/disks.
func VMDelete(ctx context.Context, d Deps, name string, deleteDisks bool) error {
	if err := checkDomain(name); err != nil {
		return err
	}
	mutate.Lock()
	defer mutate.Unlock()
	v, exists, err := domainInfo(ctx, d, name)
	if err != nil {
		return err
	}
	if !exists {
		return apierr.New(apierr.NotFound, "VM nicht gefunden")
	}
	if v.State != "shutoff" {
		return apierr.New(apierr.Conflict, "Die VM muss zuerst ausgeschaltet werden")
	}
	disks := domainDisks(ctx, d, name)
	if _, err := virshOK(ctx, d, nil, "Die VM konnte nicht gelöscht werden", "undefine", name, "--nvram", "--tpm", "--managed-save", "--snapshots-metadata"); err != nil {
		return err
	}
	if !deleteDisks {
		return nil
	}
	for _, f := range disks {
		f = path.Clean(f)
		if strings.HasPrefix(f, vm.DiskDir+"/") && d.FS.Exists(f) {
			if err := d.FS.Remove(f); err != nil {
				return err
			}
		}
	}
	return nil
}

func registerVM(s *rpc.Server, d Deps) {
	s.Handle("vm.status", func(ctx context.Context, _ json.RawMessage) (any, error) { return VMStatus(ctx, d) })
	s.Handle("vm.list", func(ctx context.Context, _ json.RawMessage) (any, error) { return ListVMs(ctx, d) })
	s.Handle("vm.networks", func(ctx context.Context, _ json.RawMessage) (any, error) { return VMNetworks(ctx, d) })
	s.Handle("vm.isos", func(ctx context.Context, _ json.RawMessage) (any, error) { return VMISOs(ctx, d) })
	s.Handle("vm.create", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			vm.Spec
			Start bool `json:"start"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return struct{}{}, VMCreate(ctx, d, req.Spec, req.Start)
	})
	s.Handle("vm.action", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Name   string `json:"name"`
			Action string `json:"action"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return struct{}{}, VMAction(ctx, d, req.Name, req.Action)
	})
	s.Handle("vm.delete", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Name        string `json:"name"`
			DeleteDisks bool   `json:"deleteDisks"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return struct{}{}, VMDelete(ctx, d, req.Name, req.DeleteDisks)
	})
}
