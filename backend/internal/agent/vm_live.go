package agent

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/vm"
)

// VMLive ändert eine laufende (oder pausierte) VM im Betrieb: Arbeitsspeicher (bis zum Maximum),
// vCPUs (bis zum Maximum), USB-Geräte und das eingelegte Medium. Alles wird zugleich in die gespeicherte
// Konfiguration übernommen (`--live --config`). Schlägt ein Schritt fehl, bleiben die vorherigen bestehen;
// die Fehlermeldung nennt den fehlgeschlagenen Schritt.
func VMLive(ctx context.Context, d Deps, name string, u vm.LiveUpdate) error {
	if err := checkDomain(name); err != nil {
		return err
	}
	if err := u.Validate(); err != nil {
		return err
	}
	if u.Empty() {
		return nil
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
	if info.State != "running" && info.State != "paused" {
		return apierr.New(apierr.Conflict, "Die VM läuft nicht – diese Einstellungen werden bei ausgeschalteter VM über „Speichern“ übernommen")
	}
	liveRes, err := virshOK(ctx, d, nil, "Die laufende Konfiguration konnte nicht gelesen werden", "dumpxml", name)
	if err != nil {
		return err
	}
	live, err := vm.ParseConfig(liveRes.Stdout)
	if err != nil {
		return apierr.New(apierr.Internal, "Die VM-Konfiguration ist nicht lesbar").With("detail", err.Error())
	}
	persRes, err := virshOK(ctx, d, nil, "Die VM-Konfiguration konnte nicht gelesen werden", "dumpxml", "--inactive", name)
	if err != nil {
		return err
	}
	pers, err := vm.ParseConfig(persRes.Stdout)
	if err != nil {
		return apierr.New(apierr.Internal, "Die VM-Konfiguration ist nicht lesbar").With("detail", err.Error())
	}

	// Prüfen vor dem ersten Eingriff, damit eine Ablehnung nichts halb ändert.
	if u.MemoryMiB != nil && *u.MemoryMiB > live.MaxMemoryMiB {
		return apierr.New(apierr.InvalidParams, "Der Arbeitsspeicher liegt über dem Maximum der laufenden VM ("+strconv.Itoa(live.MaxMemoryMiB)+" MiB). VM ausschalten und das Maximum erhöhen.")
	}
	if u.VCPUs != nil && *u.VCPUs > live.MaxVCPUs {
		return apierr.New(apierr.InvalidParams, "Die vCPUs liegen über dem Maximum der laufenden VM ("+strconv.Itoa(live.MaxVCPUs)+"). VM ausschalten und das Maximum erhöhen.")
	}
	cdrom := ""
	if u.Media != nil && *u.Media != pers.Media {
		if cdrom = vm.CDROMTarget(string(persRes.Stdout)); cdrom == "" {
			return apierr.New(apierr.Conflict, "Die VM hat kein CD-Laufwerk; es lässt sich nur bei ausgeschalteter VM hinzufügen")
		}
		if *u.Media != "" && !d.FS.Exists(vm.ISOPath(*u.Media)) {
			return apierr.New(apierr.NotFound, "ISO-Datei nicht gefunden: "+*u.Media)
		}
	}

	if u.MemoryMiB != nil && *u.MemoryMiB != live.MemoryMiB {
		if _, err := virshOK(ctx, d, nil, "Der Arbeitsspeicher konnte nicht geändert werden", "setmem", name, strconv.Itoa(*u.MemoryMiB*1024), "--live", "--config"); err != nil {
			return err
		}
	}
	if u.VCPUs != nil && *u.VCPUs != live.VCPUs {
		if _, err := virshOK(ctx, d, nil, "Die vCPUs konnten nicht geändert werden", "setvcpus", name, strconv.Itoa(*u.VCPUs), "--live", "--config"); err != nil {
			return err
		}
	}
	if u.USB != nil {
		add, remove := vm.DiffUSB(pers.USB, *u.USB)
		for _, dev := range remove {
			if _, err := virshOK(ctx, d, []byte(vm.USBHostdevXML(dev)), "Das USB-Gerät "+dev.Vendor+":"+dev.Product+" konnte nicht entzogen werden", "detach-device", name, "/dev/stdin", "--live", "--config"); err != nil {
				return err
			}
		}
		for _, dev := range add {
			if _, err := virshOK(ctx, d, []byte(vm.USBHostdevXML(dev)), "Das USB-Gerät "+dev.Vendor+":"+dev.Product+" konnte nicht zugewiesen werden (ist es angeschlossen?)", "attach-device", name, "/dev/stdin", "--live", "--config"); err != nil {
				return err
			}
		}
	}
	if cdrom != "" {
		if *u.Media == "" {
			_, err = virshOK(ctx, d, nil, "Das Medium konnte nicht ausgeworfen werden", "change-media", name, cdrom, "--eject", "--force", "--live", "--config")
		} else {
			_, err = virshOK(ctx, d, nil, "Das Medium konnte nicht gewechselt werden", "change-media", name, cdrom, vm.ISOPath(*u.Media), "--update", "--live", "--config")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func registerVMLive(s *rpc.Server, d Deps) {
	s.Handle("vm.live", func(ctx context.Context, p json.RawMessage) (any, error) {
		var r struct {
			Name string `json:"name"`
			vm.LiveUpdate
		}
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		return struct{}{}, VMLive(ctx, d, r.Name, r.LiveUpdate)
	})
}
