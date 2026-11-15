package agent

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/sysexec"
)

// Deps sind die Abhängigkeiten der Agent-Operationen.
type Deps struct {
	Runner    sysexec.Runner
	FS        sysexec.FS
	AllowLoop bool
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
}
