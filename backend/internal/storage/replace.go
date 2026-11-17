package storage

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/snapraid"
)

func gb(n uint64) string { return fmt.Sprintf("%.1f GB", float64(n)/1e9) }

// validateReplace prüft alle Voraussetzungen, bevor irgendetwas geändert wird.
func (m *Manager) validateReplace(ctx context.Context, pool, oldID, newID string) (snapraid.PoolConfig, error) {
	if oldID == newID {
		return snapraid.PoolConfig{}, invalid("Alte und neue Platte sind dieselbe.")
	}
	if err := m.poolExists(ctx, pool); err != nil {
		return snapraid.PoolConfig{}, err
	}
	all, err := m.loadSnapraid(ctx)
	if err != nil {
		return snapraid.PoolConfig{}, err
	}
	cfg, ok := all[pool]
	if !ok {
		return snapraid.PoolConfig{}, apierr.New(apierr.Conflict, "SnapRAID ist für diesen Pool nicht eingerichtet.")
	}
	if !slices.Contains(cfg.Data, oldID) {
		return snapraid.PoolConfig{}, invalid("Die Platte " + oldID + " ist keine Datenplatte dieses Pools.")
	}
	known, err := m.loadDisks(ctx)
	if err != nil {
		return snapraid.PoolConfig{}, err
	}
	nk, ok := known[newID]
	if !ok || nk.Role != RoleData {
		return snapraid.PoolConfig{}, invalid("Die Ersatzplatte muss als Datenplatte formatiert sein (Datenträger → Formatieren → Datenplatte).")
	}
	pools, err := m.loadPools(ctx)
	if err != nil {
		return snapraid.PoolConfig{}, err
	}
	if p := poolOf(pools, newID); p != "" {
		return snapraid.PoolConfig{}, apierr.New(apierr.Conflict, "Die Ersatzplatte gehört bereits zu Pool "+p+".").With("pool", p)
	}
	if _, err := m.liveDisk(ctx, newID); err != nil {
		if apierr.From(err).Code == apierr.NotFound {
			return snapraid.PoolConfig{}, invalid("Die Ersatzplatte ist nicht angeschlossen.")
		}
		return snapraid.PoolConfig{}, err
	}
	if ok := known[oldID]; nk.Size < ok.Size {
		return snapraid.PoolConfig{}, invalid(fmt.Sprintf("Die Ersatzplatte (%s) ist kleiner als die zu ersetzende (%s).", gb(nk.Size), gb(ok.Size)))
	}
	st, err := m.SnapraidStatus(ctx, pool)
	if err != nil {
		return snapraid.PoolConfig{}, err
	}
	if st.Recovery != nil {
		return snapraid.PoolConfig{}, apierr.New(apierr.Conflict, "Für diesen Pool ist noch eine Wiederherstellung offen (Platte "+st.Recovery.Disk+"). Zuerst wiederholen oder abbrechen.")
	}
	return cfg, nil
}

func (m *Manager) auditReplace(ctx context.Context, actor, action, pool string, err error, details map[string]any) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: action, Target: pool, Result: result, Details: details})
	m.hub.Publish(events.Message{Type: "pools.changed"})
	m.hub.Publish(events.Message{Type: "disks.changed"})
}

// ReplaceDisk ersetzt eine Datenplatte eines SnapRAID-Pools durch eine neue und stellt deren Dateien per fix wieder her.
// Die neue Platte führt den SnapRAID-Namen der alten weiter.
func (m *Manager) ReplaceDisk(ctx context.Context, actor, pool, oldID, newID string) (string, error) {
	if _, err := m.validateReplace(ctx, pool, oldID, newID); err != nil {
		return "", err
	}
	return m.jobs.Start("snapraid.replace", pool, actor, func(ctx context.Context, log jobs.Logger) (err error) {
		name := oldID
		defer func() {
			m.auditReplace(ctx, actor, "snapraid.replace", pool, err, map[string]any{"old": oldID, "new": newID, "name": name})
		}()
		var cfg snapraid.PoolConfig
		if err := func() error {
			m.mu.Lock()
			defer m.mu.Unlock()
			var verr error
			if cfg, verr = m.validateReplace(ctx, pool, oldID, newID); verr != nil {
				return verr
			}
			name = cfg.Name(oldID)
			return m.swapDisk(ctx, log, cfg, pool, oldID, newID, name)
		}(); err != nil {
			return err
		}
		log.SetProgress(40)
		return m.fixAndSync(ctx, log, pool, name)
	}), nil
}

// swapDisk merkt die Wiederherstellung vor, tauscht Speicher- und SnapRAID-Konfiguration und danach die DB.
// Bei einem Fehler wird der Marker zurückgenommen und der alte Zustand wiederhergestellt.
func (m *Manager) swapDisk(ctx context.Context, log jobs.Logger, cfg snapraid.PoolConfig, pool, oldID, newID, name string) error {
	log.Printf("Merke Wiederherstellung von Platte %s vor (Sync und Prüfung pausieren) …", name)
	if err := m.agent.Call(ctx, "snapraid.recovery.begin", map[string]string{"pool": pool, "disk": name}, nil); err != nil {
		return err
	}
	oldSt, _, err := m.currentState(ctx)
	if err != nil {
		m.cancelMarker(ctx, pool)
		return err
	}
	all, err := m.loadSnapraid(ctx)
	if err != nil {
		m.cancelMarker(ctx, pool)
		return err
	}
	// neuer Speicherzustand: Branch tauschen, alte Platte aus den Mounts nehmen
	newSt := State{Pools: slices.Clone(oldSt.Pools), Disks: slices.Clone(oldSt.Disks)}
	for i, p := range newSt.Pools {
		if p.Name == pool {
			newSt.Pools[i].Branches = replaceIn(p.Branches, oldID, newID)
		}
	}
	newSt.Disks = slices.DeleteFunc(newSt.Disks, func(d DiskMount) bool { return d.DiskID == oldID })
	// neue SnapRAID-Konfiguration: neue Platte unter dem alten Namen
	newCfg := cfg
	newCfg.Data = replaceIn(cfg.Data, oldID, newID)
	newCfg.Names = maps.Clone(cfg.Names)
	if newCfg.Names == nil {
		newCfg.Names = map[string]string{}
	}
	delete(newCfg.Names, oldID)
	if name != newID {
		newCfg.Names[newID] = name
	}
	if len(newCfg.Names) == 0 {
		newCfg.Names = nil
	}
	rollback := func() {
		_ = m.agent.Call(ctx, "storage.apply", oldSt, nil)
		_ = m.applySnapraid(ctx, snapraidState(all, "", nil))
		m.cancelMarker(ctx, pool)
	}
	log.Printf("Binde Ersatzplatte %s ein …", newID)
	if err := m.agent.Call(ctx, "storage.apply", newSt, nil); err != nil {
		m.cancelMarker(ctx, pool)
		return err
	}
	if err := m.applySnapraid(ctx, snapraidState(all, pool, &newCfg)); err != nil {
		rollback()
		return err
	}
	if err := m.commitTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE pool_branches SET disk_id = ? WHERE pool_name = ? AND disk_id = ?`, newID, pool, oldID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM snapraid_alias WHERE pool_name = ? AND disk_id IN (?, ?)`, pool, oldID, newID); err != nil {
			return err
		}
		if name != newID {
			if _, err := tx.ExecContext(ctx, `INSERT INTO snapraid_alias(pool_name, disk_id, name) VALUES (?,?,?)`, pool, newID, name); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM disks WHERE id = ?`, oldID)
		return err
	}); err != nil {
		rollback()
		return err
	}
	return nil
}

func replaceIn(list []string, old, new string) []string {
	out := slices.Clone(list)
	if i := slices.Index(out, old); i >= 0 {
		out[i] = new
	}
	return out
}

func (m *Manager) cancelMarker(ctx context.Context, pool string) {
	_ = m.agent.Call(ctx, "snapraid.recovery.cancel", map[string]string{"pool": pool}, nil)
}

// fixAndSync stellt die Dateien wieder her und synchronisiert danach. Der Marker fällt erst bei erfolgreichem fix weg.
func (m *Manager) fixAndSync(ctx context.Context, log jobs.Logger, pool, name string) error {
	before, err := m.SnapraidStatus(ctx, pool)
	if err != nil {
		return err
	}
	log.Printf("Stelle die Dateien von Platte %s wieder her (das kann viele Stunden dauern) …", name)
	if err := m.startAndWait(ctx, log, pool, "fix", false, name, before); err != nil {
		return fmt.Errorf("%w – die Wiederherstellung ist offen; unter „Pools“ kann sie wiederholt oder abgebrochen werden", err)
	}
	log.SetProgress(80)
	before, err = m.SnapraidStatus(ctx, pool)
	if err != nil {
		return err
	}
	log.Printf("Synchronisiere den Pool …")
	return m.startAndWait(ctx, log, pool, "sync", false, "", before)
}

// RetryFix wiederholt eine fehlgeschlagene Wiederherstellung für die im Marker genannte Platte.
func (m *Manager) RetryFix(ctx context.Context, actor, pool string) (string, error) {
	st, err := m.SnapraidStatus(ctx, pool)
	if err != nil {
		return "", err
	}
	if !st.Enabled {
		return "", apierr.New(apierr.Conflict, "SnapRAID ist für diesen Pool nicht eingerichtet.")
	}
	if st.Recovery == nil {
		return "", apierr.New(apierr.Conflict, "Für diesen Pool ist keine Wiederherstellung offen.")
	}
	name := st.Recovery.Disk
	return m.jobs.Start("snapraid.fix", pool, actor, func(ctx context.Context, log jobs.Logger) (err error) {
		defer func() { m.auditReplace(ctx, actor, "snapraid.fix", pool, err, map[string]any{"name": name}) }()
		return m.fixAndSync(ctx, log, pool, name)
	}), nil
}

// CancelRecovery bricht eine offene Wiederherstellung ab; Sync und Prüfung laufen danach wieder.
// Nicht wiederhergestellte Dateien der alten Platte bleiben verloren.
func (m *Manager) CancelRecovery(ctx context.Context, actor, pool string) error {
	if err := m.poolExists(ctx, pool); err != nil {
		return err
	}
	err := m.agent.Call(ctx, "snapraid.recovery.cancel", map[string]string{"pool": pool}, nil)
	m.auditReplace(ctx, actor, "snapraid.recovery.cancel", pool, err, nil)
	return err
}
