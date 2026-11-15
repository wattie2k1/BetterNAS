package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/rpc"
)

// ForwardAgentEvents abonniert die Ereignisse des Agents und leitet sie an den Hub weiter.
// Bei Verbindungsabbruch wird mit wachsendem Abstand (1 s bis 30 s) neu verbunden.
func ForwardAgentEvents(ctx context.Context, c *rpc.Client, hub *events.Hub) {
	delay := time.Second
	for ctx.Err() == nil {
		ch, err := c.Subscribe(ctx, "events.subscribe")
		if err == nil {
			delay = time.Second
			for ev := range ch {
				if ev.Event == "disk.changed" {
					hub.Publish(events.Message{Type: "disks.changed"})
				}
			}
		} else {
			slog.Debug("agent-ereignisse nicht verfügbar", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, 30*time.Second)
	}
}
