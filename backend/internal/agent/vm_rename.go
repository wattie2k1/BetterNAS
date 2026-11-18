package agent

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/vm"
)

// VMRename benennt eine ausgeschaltete VM um. Mit RenameFiles ziehen die von BetterNAS angelegten
// Festplatten-Dateien und die UEFI-Variablen mit (sonst blieben sie unter dem alten Namen liegen und
// würden mit einer später neu angelegten VM gleichen Namens kollidieren). Schlägt ein Schritt fehl,
// werden alle vorherigen zurückgenommen.
func VMRename(ctx context.Context, d Deps, name string, req vm.Rename) error {
	if err := checkDomain(name); err != nil {
		return err
	}
	if err := req.Validate(); err != nil {
		return err
	}
	if req.NewName == name {
		return apierr.New(apierr.InvalidParams, "Der neue Name ist gleich dem alten")
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
		return apierr.New(apierr.Conflict, "Die VM muss zum Umbenennen ausgeschaltet sein")
	}
	if _, taken, err := domainInfo(ctx, d, req.NewName); err != nil {
		return err
	} else if taken {
		return apierr.New(apierr.Conflict, "Eine VM mit diesem Namen existiert bereits")
	}
	res, err := virshOK(ctx, d, nil, "Die VM-Konfiguration konnte nicht gelesen werden", "dumpxml", "--inactive", name)
	if err != nil {
		return err
	}
	origXML := string(res.Stdout)

	var moves map[string]string
	if req.RenameFiles {
		disks, _ := vm.ParseDisks(res.Stdout)
		moves = vm.FileMoves(name, req.NewName, disks, vm.NvramFile(origXML))
		for _, to := range moves {
			if d.FS.Exists(to) {
				return apierr.New(apierr.Conflict, "Die Datei existiert bereits: "+to)
			}
		}
		if len(moves) > 0 {
			if err := clearSnapshots(ctx, d, name, req.DeleteSnapshots, "zum Umbenennen mit den Dateien"); err != nil {
				return err
			}
		}
	}

	froms := make([]string, 0, len(moves))
	for from := range moves {
		froms = append(froms, from)
	}
	sort.Strings(froms)
	var done []string
	undo := func() {
		for i := len(done) - 1; i >= 0; i-- {
			_ = d.FS.Rename(moves[done[i]], done[i])
		}
	}
	for _, from := range froms {
		if err := d.FS.Rename(from, moves[from]); err != nil {
			undo()
			return apierr.New(apierr.Internal, "Die Datei konnte nicht umbenannt werden: "+from).With("detail", err.Error())
		}
		done = append(done, from)
	}
	if len(moves) > 0 {
		if _, err := virshOK(ctx, d, []byte(vm.ApplyMoves(origXML, moves)), "Die Konfiguration konnte nicht gespeichert werden", "define", "/dev/stdin"); err != nil {
			undo()
			return err
		}
	}
	if _, err := virshOK(ctx, d, nil, "Das Umbenennen ist fehlgeschlagen", "domrename", name, req.NewName); err != nil {
		if len(moves) > 0 {
			_, _ = virshOK(ctx, d, []byte(origXML), "", "define", "/dev/stdin")
		}
		undo()
		return err
	}
	if info.Autostart {
		if now, ok, _ := domainInfo(ctx, d, req.NewName); ok && !now.Autostart {
			_, _ = virshOK(ctx, d, nil, "Autostart konnte nicht übernommen werden", "autostart", req.NewName)
		}
	}
	return nil
}

func registerVMRename(s *rpc.Server, d Deps) {
	s.Handle("vm.rename", func(ctx context.Context, p json.RawMessage) (any, error) {
		var r struct {
			Name string `json:"name"`
			vm.Rename
		}
		if err := decode(p, &r); err != nil {
			return nil, err
		}
		return struct{}{}, VMRename(ctx, d, r.Name, r.Rename)
	})
}
