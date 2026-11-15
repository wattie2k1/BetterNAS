package agent

import (
	"bytes"
	"context"
	"time"

	"github.com/betternas/betternas/internal/rpc"
)

// UdevSource liefert ein Signal je Änderung an Blockgeräten.
type UdevSource interface {
	Events(ctx context.Context) (<-chan struct{}, error)
}

// isBlockUevent erkennt Kernel-Uevents ("ACTION@DEVPATH\0KEY=VALUE\0…") für Blockgeräte,
// die für die Datenträgerliste relevant sind (add, remove, change).
func isBlockUevent(msg []byte) bool {
	fields := bytes.Split(msg, []byte{0})
	if len(fields) < 2 || !bytes.Contains(fields[0], []byte("@")) {
		return false
	}
	var action, subsystem string
	for _, f := range fields[1:] {
		if v, ok := bytes.CutPrefix(f, []byte("ACTION=")); ok {
			action = string(v)
		}
		if v, ok := bytes.CutPrefix(f, []byte("SUBSYSTEM=")); ok {
			subsystem = string(v)
		}
	}
	return subsystem == "block" && (action == "add" || action == "remove" || action == "change")
}

// DiskEvents fasst Ereignisse zusammen: Nach dem ersten Ereignis wird delay gewartet und dann
// genau ein "disk.changed" gemeldet. Endet mit ctx oder wenn emit fehlschlägt.
func DiskEvents(ctx context.Context, src UdevSource, delay time.Duration, emit func(rpc.Event) error) error {
	ch, err := src.Events(ctx)
	if err != nil {
		return err
	}
	var timer <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-ch:
			if !ok {
				return nil
			}
			if timer == nil {
				timer = time.After(delay)
			}
		case <-timer:
			timer = nil
			if err := emit(rpc.Event{Event: "disk.changed"}); err != nil {
				return err
			}
		}
	}
}
