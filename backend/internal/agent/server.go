package agent

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/shares"
	"github.com/betternas/betternas/internal/snapraid"
	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/sysexec"
)

// Deps sind die Abhängigkeiten der Agent-Operationen.
type Deps struct {
	Runner sysexec.Runner
	FS     sysexec.FS
	// Stream startet langlaufende Prozesse (Logs, Shell); nil schaltet diese Sitzungen ab.
	Stream    sysexec.Streamer
	AllowLoop bool
	// Udev liefert Änderungen an Blockgeräten; nil schaltet events.subscribe ab.
	Udev UdevSource
}

// mutate serialisiert alle ändernden Operationen (Formatieren, Storage-Apply).
var mutate sync.Mutex

// Register registriert alle Agent-Methoden.
func Register(s *rpc.Server, d Deps) {
	s.Handle("auth.verify", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.Unmarshal(p, &req); err != nil {
			return nil, apierr.New(apierr.InvalidParams, "Ungültige Parameter")
		}
		ok, err := VerifyPassword(ctx, d.Runner, req.Username, req.Password)
		if err != nil {
			return nil, err
		}
		return map[string]bool{"ok": ok}, nil
	})
	s.Handle("system.info", func(context.Context, json.RawMessage) (any, error) {
		return SystemInfo(d)
	})
	registerEvents(s, d)
	s.Handle("disk.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return ListDisks(ctx, d)
	})
	s.Handle("disk.smart", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Device string `json:"device"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return Smart(ctx, d, req.Device)
	})
	s.Handle("disk.format", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req FormatRequest
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		mutate.Lock()
		defer mutate.Unlock()
		return Format(ctx, d, req)
	})
	s.Handle("account.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return ListAccounts(ctx, d)
	})
	// mutating registriert eine ändernde Methode unter der gemeinsamen Sperre.
	mutating := func(method string, fn func(ctx context.Context, p json.RawMessage) error) {
		s.Handle(method, func(ctx context.Context, p json.RawMessage) (any, error) {
			mutate.Lock()
			defer mutate.Unlock()
			if err := fn(ctx, p); err != nil {
				return nil, err
			}
			return map[string]bool{"ok": true}, nil
		})
	}
	mutating("user.create", func(ctx context.Context, p json.RawMessage) error {
		var req UserCreate
		if err := decode(p, &req); err != nil {
			return err
		}
		return CreateUser(ctx, d, req)
	})
	mutating("user.update", func(ctx context.Context, p json.RawMessage) error {
		var req UserUpdate
		if err := decode(p, &req); err != nil {
			return err
		}
		return UpdateUser(ctx, d, req)
	})
	mutating("user.password", func(ctx context.Context, p json.RawMessage) error {
		var req struct{ Name, Password string }
		if err := decode(p, &req); err != nil {
			return err
		}
		return SetUserPassword(ctx, d, req.Name, req.Password)
	})
	mutating("user.delete", func(ctx context.Context, p json.RawMessage) error {
		var req struct{ Name string }
		if err := decode(p, &req); err != nil {
			return err
		}
		return DeleteUser(ctx, d, req.Name)
	})
	mutating("group.create", func(ctx context.Context, p json.RawMessage) error {
		var req struct{ Name string }
		if err := decode(p, &req); err != nil {
			return err
		}
		return CreateGroup(ctx, d, req.Name)
	})
	mutating("group.delete", func(ctx context.Context, p json.RawMessage) error {
		var req struct{ Name string }
		if err := decode(p, &req); err != nil {
			return err
		}
		return DeleteGroup(ctx, d, req.Name)
	})
	mutating("group.members", func(ctx context.Context, p json.RawMessage) error {
		var req struct {
			Name    string   `json:"name"`
			Members []string `json:"members"`
		}
		if err := decode(p, &req); err != nil {
			return err
		}
		return SetGroupMembers(ctx, d, req.Name, req.Members)
	})
	s.Handle("share.apply", func(ctx context.Context, p json.RawMessage) (any, error) {
		var st shares.State
		if err := decode(p, &st); err != nil {
			return nil, err
		}
		mutate.Lock()
		defer mutate.Unlock()
		return ApplyShares(ctx, d, st)
	})
	s.Handle("updates.check", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return CheckUpdates(ctx, d)
	})
	s.Handle("updates.install", func(ctx context.Context, _ json.RawMessage) (any, error) {
		unit, err := InstallUpdates(ctx, d)
		if err != nil {
			return nil, err
		}
		return map[string]string{"unit": unit}, nil
	})
	s.Handle("updates.status", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Unit string `json:"unit"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return UpdateStatus(ctx, d, req.Unit)
	})
	s.Handle("updates.channel", func(ctx context.Context, _ json.RawMessage) (any, error) {
		ch, err := UpdateChannel(ctx, d)
		if err != nil {
			return nil, err
		}
		return map[string]string{"channel": ch}, nil
	})
	s.Handle("updates.setchannel", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Channel string `json:"channel"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return struct{}{}, SetUpdateChannel(ctx, d, req.Channel)
	})
	s.Handle("snapraid.apply", func(ctx context.Context, p json.RawMessage) (any, error) {
		var st snapraid.State
		if err := decode(p, &st); err != nil {
			return nil, err
		}
		mutate.Lock()
		defer mutate.Unlock()
		return ApplySnapraid(ctx, d, st)
	})
	s.Handle("snapraid.start", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Pool   string `json:"pool"`
			Action string `json:"action"`
			Force  bool   `json:"force"`
			Disk   string `json:"disk"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		unit, err := StartSnapraidDisk(ctx, d, req.Pool, req.Action, req.Force, req.Disk)
		if err != nil {
			return nil, err
		}
		return map[string]string{"unit": unit}, nil
	})
	s.Handle("snapraid.recovery.begin", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Pool string `json:"pool"`
			Disk string `json:"disk"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		if err := RecoveryBegin(ctx, d, req.Pool, req.Disk); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
	s.Handle("snapraid.recovery.cancel", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Pool string `json:"pool"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		if err := RecoveryCancel(ctx, d, req.Pool); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
	s.Handle("snapraid.status", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Pool string `json:"pool"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return SnapraidStatus(ctx, d, req.Pool)
	})
	registerDocker(s, d)
	registerDockerOps(s, d)
	registerDockerUpdates(s, d)
	registerDockerResources(s, d)
	registerVM(s, d)
	registerBridge(s, d)
	registerNetwork(s, d)
	registerVMOps(s, d)
	registerVMConfig(s, d)
	registerSpindown(s, d)
	registerSystem(s, d)
	s.Handle("storage.apply", func(ctx context.Context, p json.RawMessage) (any, error) {
		var st storage.State
		if err := decode(p, &st); err != nil {
			return nil, err
		}
		mutate.Lock()
		defer mutate.Unlock()
		return Apply(ctx, d, st)
	})
}

// registerEvents registriert das Ereignis-Abonnement (1 s Entprellung).
func registerEvents(s *rpc.Server, d Deps) {
	if d.Udev == nil {
		return
	}
	s.HandleStream("events.subscribe", func(ctx context.Context, emit func(rpc.Event) error) error {
		return DiskEvents(ctx, d.Udev, time.Second, emit)
	})
}

func decode(p json.RawMessage, v any) error {
	if err := json.Unmarshal(p, v); err != nil {
		return apierr.New(apierr.InvalidParams, "Ungültige Parameter")
	}
	return nil
}
