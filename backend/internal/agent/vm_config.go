package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/vm"
)

// VMConfig liefert die Konfiguration einer VM aus ihrer inaktiven Domain-XML.
func VMConfig(ctx context.Context, d Deps, name string) (vm.Config, error) {
	if err := checkDomain(name); err != nil {
		return vm.Config{}, err
	}
	info, exists, err := domainInfo(ctx, d, name)
	if err != nil {
		return vm.Config{}, err
	}
	if !exists {
		return vm.Config{}, apierr.New(apierr.NotFound, "VM nicht gefunden")
	}
	res, err := virshOK(ctx, d, nil, "Die VM-Konfiguration konnte nicht gelesen werden", "dumpxml", "--inactive", name)
	if err != nil {
		return vm.Config{}, err
	}
	c, err := vm.ParseConfig(res.Stdout)
	if err != nil {
		return vm.Config{}, apierr.New(apierr.Internal, "Die VM-Konfiguration ist nicht lesbar").With("detail", err.Error())
	}
	c.Name, c.State, c.Autostart = name, info.State, info.Autostart
	return c, nil
}

// VMConfigure ändert die Konfiguration einer ausgeschalteten VM. Ändert sich Secure Boot, wird die
// UEFI-Variablen-Datei verworfen und beim nächsten Start neu aus der Vorlage erzeugt.
func VMConfigure(ctx context.Context, d Deps, name string, u vm.ConfigUpdate) error {
	if err := checkDomain(name); err != nil {
		return err
	}
	if err := u.Validate(); err != nil {
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
		return apierr.New(apierr.Conflict, "Die VM muss zum Ändern ausgeschaltet sein")
	}
	if u.Media != "" && !d.FS.Exists(vm.ISOPath(u.Media)) {
		return apierr.New(apierr.NotFound, "ISO-Datei nicht gefunden: "+u.Media)
	}
	res, err := virshOK(ctx, d, nil, "Die VM-Konfiguration konnte nicht gelesen werden", "dumpxml", "--inactive", name)
	if err != nil {
		return err
	}
	if cur, perr := vm.ParseConfig(res.Stdout); perr == nil {
		if cur.Network != u.Network {
			if err := checkNetwork(ctx, d, u.Network); err != nil {
				return err
			}
		}
		if u.ExtraNICs != nil {
			known := map[string]string{}
			for _, n := range cur.ExtraNICs {
				known[n.MAC] = n.Network
			}
			for _, n := range *u.ExtraNICs {
				if old, ok := known[strings.ToLower(n.MAC)]; ok && old == n.Network {
					continue
				}
				if err := checkNetwork(ctx, d, n.Network); err != nil {
					return err
				}
			}
		}
	}
	xml, nvram, err := vm.ApplyConfig(string(res.Stdout), u)
	if err != nil {
		return err
	}
	if _, err := virshOK(ctx, d, []byte(xml), "Die Konfiguration konnte nicht gespeichert werden", "define", "/dev/stdin"); err != nil {
		return err
	}
	if nvram != "" && d.FS.Exists(nvram) {
		if err := d.FS.Remove(nvram); err != nil {
			return apierr.New(apierr.Internal, "Die alte UEFI-Variablen-Datei konnte nicht entfernt werden").With("detail", err.Error())
		}
	}
	return nil
}

func registerVMConfig(s *rpc.Server, d Deps) {
	s.Handle("vm.config", func(ctx context.Context, p json.RawMessage) (any, error) {
		var r struct {
			Name string `json:"name"`
		}
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		return VMConfig(ctx, d, r.Name)
	})
	s.Handle("vm.configure", func(ctx context.Context, p json.RawMessage) (any, error) {
		var r struct {
			Name string `json:"name"`
			vm.ConfigUpdate
		}
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		return struct{}{}, VMConfigure(ctx, d, r.Name, r.ConfigUpdate)
	})
}
