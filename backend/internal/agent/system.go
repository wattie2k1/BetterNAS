package agent

import (
	"context"
	"encoding/json"

	"github.com/betternas/betternas/internal/rpc"
)

// RestartAPI startet die API in 2 s neu (verzögert, damit die Antwort an den Browser noch hinausgeht).
func RestartAPI(ctx context.Context, d Deps) error {
	_, err := run(ctx, d.Runner, nil, "systemd-run", "--on-active=2", "--unit=betternas-api-restart", "systemctl", "restart", "betternas-api.service")
	return err
}

func registerSystem(s *rpc.Server, d Deps) {
	s.Handle("system.restart-api", func(ctx context.Context, _ json.RawMessage) (any, error) {
		if err := RestartAPI(ctx, d); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
}
