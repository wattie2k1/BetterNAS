package storage

import (
	"context"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

func (m *Manager) loadSpares(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	rows, err := m.db.QueryContext(ctx, `SELECT disk_id FROM disk_spare`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
}

// spareEligible: nur eine Platte, die gerade nirgends benutzt wird, kann Ersatzplatte sein
// (nicht zugewiesen oder als Datenplatte formatiert, aber in keinem Pool).
func spareEligible(role, pool string, system bool) bool {
	return !system && pool == "" && (role == "unassigned" || role == RoleData)
}

// SetSpare markiert eine Platte als Ersatzplatte (Hot-Spare) oder hebt die Markierung auf.
// Die Platte wird dadurch nicht in Betrieb genommen: sie bleibt unbenutzt, bis man sie bei einem Ausfall einsetzt.
func (m *Manager) SetSpare(ctx context.Context, actor, id string, on bool) (err error) {
	views, err := m.ListDisks(ctx)
	if err != nil {
		return err
	}
	var v *DiskView
	for i := range views {
		if views[i].ID == id {
			v = &views[i]
		}
	}
	if v == nil {
		return apierr.New(apierr.NotFound, "Datenträger nicht gefunden")
	}
	if on && (v.State != "online" || !spareEligible(v.Role, v.Pool, v.System)) {
		return apierr.New(apierr.Conflict, "Als Ersatzplatte taugt nur eine angeschlossene Platte, die in keinem Pool und keiner Parität benutzt wird.")
	}
	defer func() {
		result := "ok"
		if err != nil {
			result = "error"
		}
		_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "disk.spare", Target: id, Result: result, Details: map[string]any{"spare": on}})
	}()
	if on {
		_, err = m.db.ExecContext(ctx, `INSERT OR IGNORE INTO disk_spare(disk_id) VALUES (?)`, id)
	} else {
		_, err = m.db.ExecContext(ctx, `DELETE FROM disk_spare WHERE disk_id = ?`, id)
	}
	if err != nil {
		return err
	}
	m.hub.Publish(events.Message{Type: "disks.changed"})
	return nil
}
