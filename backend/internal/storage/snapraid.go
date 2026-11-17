package storage

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/snapraid"
)

// SnapraidInput sind die Eingaben zum Einrichten (und Ändern) von SnapRAID für einen Pool.
type SnapraidInput struct {
	Parity []string `json:"parity"`
	snapraid.Settings
}

const startTimeout = 30 * time.Second

// loadSnapraid liefert die SnapRAID-Konfiguration aller Pools mit SnapRAID (Daten = Pool-Branches).
func (m *Manager) loadSnapraid(ctx context.Context) (map[string]snapraid.PoolConfig, error) {
	pools, err := m.loadPools(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]snapraid.PoolConfig{}
	rows, err := m.db.QueryContext(ctx, `SELECT pool_name, sync_schedule, scrub_schedule, scrub_percent, scrub_older_days, delete_threshold FROM snapraid`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c snapraid.PoolConfig
		if err := rows.Scan(&c.Pool, &c.SyncSchedule, &c.ScrubSchedule, &c.ScrubPercent, &c.ScrubOlderDays, &c.DeleteThreshold); err != nil {
			rows.Close()
			return nil, err
		}
		c.Parity = []string{}
		out[c.Pool] = c
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	prows, err := m.db.QueryContext(ctx, `SELECT pool_name, disk_id FROM snapraid_parity ORDER BY pool_name, level`)
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var pool, disk string
		if err := prows.Scan(&pool, &disk); err != nil {
			return nil, err
		}
		if c, ok := out[pool]; ok {
			c.Parity = append(c.Parity, disk)
			out[pool] = c
		}
	}
	if err := prows.Err(); err != nil {
		return nil, err
	}
	aliases, err := m.loadAliases(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range pools {
		if c, ok := out[p.Name]; ok {
			c.Data = p.Branches
			c.Names = namesFor(aliases[p.Name], p.Branches)
			out[p.Name] = c
		}
	}
	return out, nil
}

// loadAliases liefert je Pool: Platten-ID → SnapRAID-Name.
func (m *Manager) loadAliases(ctx context.Context) (map[string]map[string]string, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT pool_name, disk_id, name FROM snapraid_alias`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]string{}
	for rows.Next() {
		var pool, disk, name string
		if err := rows.Scan(&pool, &disk, &name); err != nil {
			return nil, err
		}
		if out[pool] == nil {
			out[pool] = map[string]string{}
		}
		out[pool][disk] = name
	}
	return out, rows.Err()
}

// namesFor behält nur Aliase von Platten, die aktuell Datenplatten des Pools sind (leer → nil).
func namesFor(all map[string]string, data []string) map[string]string {
	var out map[string]string
	for _, id := range data {
		if n, ok := all[id]; ok {
			if out == nil {
				out = map[string]string{}
			}
			out[id] = n
		}
	}
	return out
}

// snapraidState macht aus der Konfiguration einen sortierten Agent-Zustand; skip wird ausgelassen.
func snapraidState(cfg map[string]snapraid.PoolConfig, skip string, add *snapraid.PoolConfig) snapraid.State {
	st := snapraid.State{Pools: []snapraid.PoolConfig{}}
	for name, c := range cfg {
		if name != skip {
			st.Pools = append(st.Pools, c)
		}
	}
	if add != nil {
		st.Pools = append(st.Pools, *add)
	}
	slices.SortFunc(st.Pools, func(a, b snapraid.PoolConfig) int { return strings.Compare(a.Pool, b.Pool) })
	return st
}

// parityPools liefert Paritätsplatte → Pool.
func (m *Manager) parityPools(ctx context.Context) (map[string]string, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT disk_id, pool_name FROM snapraid_parity`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var disk, pool string
		if err := rows.Scan(&disk, &pool); err != nil {
			return nil, err
		}
		out[disk] = pool
	}
	return out, rows.Err()
}

func (m *Manager) applySnapraid(ctx context.Context, st snapraid.State) error {
	return m.agent.Call(ctx, "snapraid.apply", st, nil)
}

// checkParity prüft die Paritätsplatten gegen Datenplatten und andere Pools.
func (m *Manager) checkParity(ctx context.Context, pool string, data, parity []string) error {
	known, err := m.loadDisks(ctx)
	if err != nil {
		return err
	}
	used, err := m.parityPools(ctx)
	if err != nil {
		return err
	}
	var largest uint64
	for _, id := range data {
		largest = max(largest, known[id].Size)
	}
	for _, id := range parity {
		k, ok := known[id]
		switch {
		case !ok || k.Role != RoleParity:
			return invalid("Der Datenträger " + id + " ist keine Paritätsplatte")
		case used[id] != "" && used[id] != pool:
			return apierr.New(apierr.Conflict, "Die Paritätsplatte "+id+" gehört bereits zu Pool "+used[id]).With("pool", used[id])
		case k.Size < largest:
			return invalid(fmt.Sprintf("Die Paritätsplatte %s ist kleiner als die größte Datenplatte des Pools", id))
		}
	}
	return nil
}

// prepareEnable validiert die Eingaben und liefert die neue Pool-Konfiguration.
func (m *Manager) prepareEnable(ctx context.Context, pool string, in SnapraidInput) (snapraid.PoolConfig, map[string]snapraid.PoolConfig, error) {
	pools, err := m.loadPools(ctx)
	if err != nil {
		return snapraid.PoolConfig{}, nil, err
	}
	i := slices.IndexFunc(pools, func(p Pool) bool { return p.Name == pool })
	if i < 0 {
		return snapraid.PoolConfig{}, nil, apierr.New(apierr.NotFound, "Pool nicht gefunden")
	}
	aliases, err := m.loadAliases(ctx)
	if err != nil {
		return snapraid.PoolConfig{}, nil, err
	}
	cfg := snapraid.PoolConfig{Pool: pool, Data: pools[i].Branches, Parity: in.Parity, Names: namesFor(aliases[pool], pools[i].Branches), Settings: in.Settings}.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return snapraid.PoolConfig{}, nil, err
	}
	if err := m.checkParity(ctx, pool, cfg.Data, cfg.Parity); err != nil {
		return snapraid.PoolConfig{}, nil, err
	}
	all, err := m.loadSnapraid(ctx)
	return cfg, all, err
}

// EnableSnapraid richtet SnapRAID für einen Pool ein oder ändert die Einstellungen.
func (m *Manager) EnableSnapraid(ctx context.Context, actor, pool string, in SnapraidInput) (string, error) {
	if _, _, err := m.prepareEnable(ctx, pool, in); err != nil {
		return "", err
	}
	return m.run("snapraid.enable", pool, actor, "pools.changed", map[string]any{"parity": in.Parity},
		func(ctx context.Context, log jobs.Logger) error {
			cfg, all, err := m.prepareEnable(ctx, pool, in)
			if err != nil {
				return err
			}
			log.Printf("Richte SnapRAID für Pool %s ein …", pool)
			log.SetProgress(50)
			if err := m.applySnapraid(ctx, snapraidState(all, pool, &cfg)); err != nil {
				return err
			}
			return m.commitTx(ctx, func(tx *sql.Tx) error {
				if err := clearSnapraid(ctx, tx, pool); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO snapraid(pool_name, sync_schedule, scrub_schedule, scrub_percent, scrub_older_days, delete_threshold) VALUES (?,?,?,?,?,?)`,
					pool, cfg.SyncSchedule, cfg.ScrubSchedule, cfg.ScrubPercent, cfg.ScrubOlderDays, cfg.DeleteThreshold); err != nil {
					return err
				}
				for i, id := range cfg.Parity {
					if _, err := tx.ExecContext(ctx, `INSERT INTO snapraid_parity(pool_name, disk_id, level) VALUES (?,?,?)`, pool, id, i+1); err != nil {
						return err
					}
				}
				_, err := tx.ExecContext(ctx, `UPDATE pools SET snapraid_enabled = 1 WHERE name = ?`, pool)
				return err
			})
		}), nil
}

func clearSnapraid(ctx context.Context, tx *sql.Tx, pool string) error {
	for _, q := range []string{`DELETE FROM snapraid_parity WHERE pool_name = ?`, `DELETE FROM snapraid WHERE pool_name = ?`} {
		if _, err := tx.ExecContext(ctx, q, pool); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) commitTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// DisableSnapraid schaltet SnapRAID für einen Pool ab (Timer weg; Paritätsdateien bleiben liegen).
func (m *Manager) DisableSnapraid(ctx context.Context, actor, pool string) (string, error) {
	if err := m.poolExists(ctx, pool); err != nil {
		return "", err
	}
	return m.run("snapraid.disable", pool, actor, "pools.changed", nil, func(ctx context.Context, log jobs.Logger) error {
		all, err := m.loadSnapraid(ctx)
		if err != nil {
			return err
		}
		log.Printf("Schalte SnapRAID für Pool %s ab …", pool)
		if err := m.applySnapraid(ctx, snapraidState(all, pool, nil)); err != nil {
			return err
		}
		return m.commitTx(ctx, func(tx *sql.Tx) error {
			if err := clearSnapraid(ctx, tx, pool); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `UPDATE pools SET snapraid_enabled = 0 WHERE name = ?`, pool)
			return err
		})
	}), nil
}

// snapraidFor liefert die Einstellungen eines Pools als Eingabe (nil = nicht aktiv).
func snapraidInputOf(c snapraid.PoolConfig, ok bool) *SnapraidInput {
	if !ok {
		return nil
	}
	return &SnapraidInput{Parity: c.Parity, Settings: c.Settings}
}

// SnapraidStatus liefert den Zustand von SnapRAID für einen Pool.
func (m *Manager) SnapraidStatus(ctx context.Context, pool string) (snapraid.Status, error) {
	if err := m.poolExists(ctx, pool); err != nil {
		return snapraid.Status{}, err
	}
	all, err := m.loadSnapraid(ctx)
	if err != nil {
		return snapraid.Status{}, err
	}
	var st snapraid.Status
	if err := m.agent.Call(ctx, "snapraid.status", map[string]string{"pool": pool}, &st); err != nil {
		return snapraid.Status{}, err
	}
	_, st.Enabled = all[pool]
	return st, nil
}

func runOf(st snapraid.Status, action string) *snapraid.RunState {
	switch action {
	case "sync":
		return st.LastSync
	case "scrub":
		return st.LastScrub
	}
	return nil
}

// stamp liefert den Zeitstempel des letzten Ergebnisses der Aktion (zum Erkennen eines neuen Laufs).
func stamp(st snapraid.Status, action string) time.Time {
	if action == "diff" {
		if st.DiffAt != nil {
			return *st.DiffAt
		}
		return time.Time{}
	}
	if r := runOf(st, action); r != nil {
		return r.At
	}
	return time.Time{}
}

// RunSnapraid startet sync, scrub oder diff und begleitet den Lauf als Job, bis er fertig ist.
// Der Lauf selbst steckt in einer eigenen Unit des Agents; der Job hält die Speicher-Sperre nicht.
func (m *Manager) RunSnapraid(ctx context.Context, actor, pool, action string, force bool) (string, error) {
	if action != "sync" && action != "scrub" && action != "diff" {
		return "", invalid("Unbekannte Aktion (erlaubt: sync, scrub, diff)")
	}
	before, err := m.SnapraidStatus(ctx, pool)
	if err != nil {
		return "", err
	}
	if !before.Enabled {
		return "", apierr.New(apierr.Conflict, "SnapRAID ist für diesen Pool nicht eingerichtet.")
	}
	kind := "snapraid." + action
	return m.jobs.Start(kind, pool, actor, func(ctx context.Context, log jobs.Logger) (err error) {
		defer func() {
			result := "ok"
			if err != nil {
				result = "error"
			}
			_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: kind, Target: pool, Result: result, Details: map[string]any{"force": force}})
			m.hub.Publish(events.Message{Type: "pools.changed"})
		}()
		log.Printf("Starte SnapRAID %s für Pool %s …", action, pool)
		req := map[string]any{"pool": pool, "action": action, "force": force}
		if err := m.agent.Call(ctx, "snapraid.start", req, nil); err != nil {
			return err
		}
		began, seen := time.Now(), false
		for {
			time.Sleep(m.pollEvery)
			st, err := m.SnapraidStatus(ctx, pool)
			if err != nil {
				return err
			}
			seen = seen || st.Running
			if st.Running {
				continue
			}
			if !seen && !stamp(st, action).After(stamp(before, action)) {
				if time.Since(began) > startTimeout {
					return apierr.New(apierr.Internal, "Der SnapRAID-Lauf hat nicht gestartet.")
				}
				continue
			}
			if st.Log != "" {
				log.Printf("%s", st.Log)
			}
			if action == "diff" {
				return nil
			}
			r := runOf(st, action)
			switch {
			case r == nil:
				return apierr.New(apierr.Internal, "Der SnapRAID-Lauf hat kein Ergebnis hinterlassen.")
			case r.Result == snapraid.ResultOK:
				return nil
			default:
				return apierr.New(apierr.Conflict, r.Message)
			}
		}
	}), nil
}
