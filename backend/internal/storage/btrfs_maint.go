package storage

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
)

// Wartung eines btrfs-Pools. Plattenersatz, Hinzufügen, Entfernen, Balance und Profilwechsel laufen als Jobs,
// die die Speicher-Sperre nur für die Änderung des Soll-Zustands halten (nicht während btrfs arbeitet).

func (m *Manager) isBtrfsBusy(pool string) bool {
	m.tmu.Lock()
	defer m.tmu.Unlock()
	return m.btrfsBusy[pool]
}

// beginBtrfs reserviert den Pool für eine Wartung; release gibt ihn wieder frei.
func (m *Manager) beginBtrfs(pool string) (release func(), err error) {
	m.tmu.Lock()
	defer m.tmu.Unlock()
	if m.btrfsBusy[pool] {
		return nil, apierr.New(apierr.Conflict, "Auf dem Pool läuft bereits eine Wartung.")
	}
	m.btrfsBusy[pool] = true
	return func() {
		m.tmu.Lock()
		delete(m.btrfsBusy, pool)
		m.tmu.Unlock()
	}, nil
}

// runMaint startet eine Wartung als Job ohne Speicher-Sperre, mit Audit und Ereignis am Ende.
func (m *Manager) runMaint(kind, pool, actor string, release func(), details map[string]any, fn func(ctx context.Context, log jobs.Logger) error) string {
	return m.jobs.Start(kind, pool, actor, func(ctx context.Context, log jobs.Logger) (err error) {
		defer release()
		defer func() {
			result := "ok"
			if err != nil {
				result = "error"
			}
			_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: kind, Target: pool, Result: result, Details: details})
			m.hub.Publish(events.Message{Type: "pools.changed"})
		}()
		return fn(ctx, log)
	})
}

// BtrfsDiskInput ist die Eingabe zum Ersetzen, Hinzufügen oder Entfernen einer Platte.
type BtrfsDiskInput struct {
	DiskID       string `json:"diskId"`                 // Entfernen: Mitglied; Hinzufügen: neue Platte
	OldDiskID    string `json:"oldDiskId,omitempty"`    // Ersetzen: Mitglied, das ersetzt wird
	ConfirmToken string `json:"confirmToken,omitempty"` // aus POST /pools/preview (neue Platte)
	ConfirmName  string `json:"confirmName,omitempty"`  // Poolname
	Balance      bool   `json:"balance,omitempty"`      // Hinzufügen: danach die Daten gleichmäßig verteilen
}

// maintPool lädt den btrfs-Pool und seinen Zustand und verlangt, dass er eingehängt ist.
func (m *Manager) maintPool(ctx context.Context, name string) (Pool, BtrfsStatus, error) {
	p, err := m.findBtrfsPool(ctx, name)
	if err != nil {
		return Pool{}, BtrfsStatus{}, err
	}
	st, err := m.btrfsStatus(ctx, p)
	if err != nil {
		return Pool{}, BtrfsStatus{}, err
	}
	if !st.Mounted {
		return Pool{}, BtrfsStatus{}, apierr.New(apierr.Conflict, "Der Pool ist nicht eingehängt. Bei fehlenden Platten zuerst „Degradiert einhängen“.")
	}
	if st.Op != nil && st.Op.Running || st.Replace != nil && st.Replace.State == "running" {
		return Pool{}, BtrfsStatus{}, apierr.New(apierr.Conflict, "Auf dem Pool läuft bereits eine Wartung.")
	}
	return p, st, nil
}

// newDisk prüft Token, Poolname und Platte für eine neue Platte (wird gelöscht).
func (m *Manager) newDisk(ctx context.Context, pool, diskID, token, confirmName string) (BtrfsDisk, Disk, error) {
	if confirmName != pool {
		return BtrfsDisk{}, Disk{}, invalid("Zur Bestätigung den Poolnamen eingeben")
	}
	tok, ok := m.takePoolToken(token)
	if !ok || m.now().After(tok.expires) {
		return BtrfsDisk{}, Disk{}, apierr.New(apierr.Conflict, "Die Bestätigung ist abgelaufen. Bitte die Vorschau erneut öffnen.")
	}
	if len(tok.disks) != 1 || tok.disks[0].DiskID != diskID {
		return BtrfsDisk{}, Disk{}, apierr.New(apierr.Conflict, "Die Auswahl hat sich seit der Vorschau verändert. Bitte erneut prüfen.")
	}
	d, err := m.liveDisk(ctx, diskID)
	if err != nil {
		return BtrfsDisk{}, Disk{}, err
	}
	if d.Device != tok.disks[0].Device || d.SizeBytes != tok.disks[0].SizeBytes {
		return BtrfsDisk{}, Disk{}, apierr.New(apierr.Conflict, "Der Datenträger hat sich seit der Vorschau verändert. Bitte erneut prüfen.")
	}
	if err := m.checkUnassigned(ctx, d); err != nil {
		return BtrfsDisk{}, Disk{}, err
	}
	m.tmu.Lock()
	delete(m.poolTokens, token)
	m.tmu.Unlock()
	return tok.disks[0], d, nil
}

// commitMembers schreibt neue Mitglieder, entfernte Platten und das Profil in den Soll-Zustand.
func (m *Manager) commitMembers(ctx context.Context, log jobs.Logger, pool string, branches []string, add []Disk, remove []string, fsUUID, profile string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, _, err := m.currentState(ctx)
	if err != nil {
		return err
	}
	for i, p := range st.Pools {
		if p.Name == pool {
			st.Pools[i].Branches = branches
			if profile != "" {
				st.Pools[i].Profile = profile
			}
		}
	}
	st.Disks = slices.DeleteFunc(st.Disks, func(d DiskMount) bool { return slices.Contains(remove, d.DiskID) })
	for _, d := range add {
		st.Disks = append(st.Disks, DiskMount{DiskID: d.ID, FSUUID: fsUUID, FSType: "btrfs", Role: RoleData})
	}
	if err := st.Validate(); err != nil {
		return err
	}
	return m.applyAndCommit(ctx, log, st, func(tx *sql.Tx) error {
		for _, d := range add {
			if _, err := tx.ExecContext(ctx, `INSERT INTO disks(id, role, fs_type, fs_uuid, label, model, serial, size_bytes, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, d.ID, RoleData, "btrfs", fsUUID, BtrfsLabel(pool), d.Model, d.Serial, d.SizeBytes, m.now().UTC().Format(time.RFC3339)); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM pool_branches WHERE pool_name = ?`, pool); err != nil {
			return err
		}
		for i, b := range branches {
			if _, err := tx.ExecContext(ctx, `INSERT INTO pool_branches(pool_name, disk_id, position) VALUES (?,?,?)`, pool, b, i); err != nil {
				return err
			}
		}
		if profile != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE pools SET profile = ? WHERE name = ?`, profile, pool); err != nil {
				return err
			}
		}
		for _, id := range remove {
			if _, err := tx.ExecContext(ctx, `DELETE FROM disks WHERE id = ?`, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// waitOp wartet, bis die Wartungs-Unit des Pools fertig ist, und meldet Fortschritt und Fehler.
func (m *Manager) waitOp(ctx context.Context, log jobs.Logger, pool string) error {
	for {
		var op BtrfsOp
		if err := m.agent.Call(ctx, "pool.btrfs.op.status", map[string]string{"name": pool}, &op); err != nil {
			return err
		}
		switch {
		case op.Running:
			if op.Progress >= 0 {
				log.SetProgress(op.Progress)
			}
			time.Sleep(m.pollEvery)
		case op.Result == "failed":
			msg := "Die Wartung ist fehlgeschlagen."
			if op.Message != "" {
				msg += " " + op.Message
			}
			return apierr.New(apierr.Conflict, msg)
		default:
			return nil
		}
	}
}

func (m *Manager) startOp(ctx context.Context, req BtrfsOpRequest) error {
	return m.agent.Call(ctx, "pool.btrfs.op.start", req, nil)
}

// waitReplace wartet, bis der Plattenersatz fertig ist.
func (m *Manager) waitReplace(ctx context.Context, log jobs.Logger, pool string) error {
	for {
		var r BtrfsReplace
		if err := m.agent.Call(ctx, "pool.btrfs.replace.status", map[string]string{"name": pool}, &r); err != nil {
			return err
		}
		switch r.State {
		case "running":
			log.SetProgress(int(r.Percent))
			time.Sleep(m.pollEvery)
		case "finished":
			if r.WriteErrs > 0 || r.ReadErrs > 0 {
				log.Printf("Hinweis: %d Schreibfehler, %d nicht lesbare Blöcke beim Kopieren.", r.WriteErrs, r.ReadErrs)
			}
			return nil
		case "canceled":
			return apierr.New(apierr.Conflict, "Der Plattenersatz wurde abgebrochen.")
		default:
			return apierr.New(apierr.Internal, "Der Plattenersatz wurde nicht gestartet.")
		}
	}
}

// deviceOf liefert die btrfs-devid eines Mitglieds. Fehlt das Mitglied, ist es die einzige fehlende Platte.
func deviceOf(st BtrfsStatus, diskID string) (devID int, missing bool, err error) {
	for _, d := range st.Devices {
		if d.DiskID == diskID && !d.Missing {
			return d.DevID, false, nil
		}
	}
	var gone []BtrfsDevice
	for _, d := range st.Devices {
		if d.Missing {
			gone = append(gone, d)
		}
	}
	if len(gone) == 1 {
		return gone[0].DevID, true, nil
	}
	if st.Missing == 1 && len(gone) == 0 {
		return 0, true, nil // btrfs listet die fehlende Platte nicht mehr auf: nur „missing“ entfernen, nicht ersetzen
	}
	return 0, false, apierr.New(apierr.Conflict, "Die Platte wurde im Pool nicht gefunden.")
}

// ReplaceBtrfsDisk ersetzt ein Mitglied (auch eine fehlende Platte) durch eine neue; die neue Platte wird gelöscht.
func (m *Manager) ReplaceBtrfsDisk(ctx context.Context, actor, pool string, in BtrfsDiskInput) (string, error) {
	p, st, err := m.maintPool(ctx, pool)
	if err != nil {
		return "", err
	}
	if !slices.Contains(p.Branches, in.OldDiskID) {
		return "", invalid("Die Platte " + in.OldDiskID + " gehört nicht zum Pool.")
	}
	devID, _, err := deviceOf(st, in.OldDiskID)
	if err != nil {
		return "", err
	}
	if devID == 0 {
		return "", apierr.New(apierr.Conflict, "Die fehlende Platte lässt sich nicht eindeutig bestimmen. Bitte den Pool zuerst degradiert einhängen.")
	}
	target, nd, err := m.newDisk(ctx, pool, in.DiskID, in.ConfirmToken, in.ConfirmName)
	if err != nil {
		return "", err
	}
	release, err := m.beginBtrfs(pool)
	if err != nil {
		return "", err
	}
	branches := slices.Clone(p.Branches)
	branches[slices.Index(branches, in.OldDiskID)] = in.DiskID
	return m.runMaint("pool.replace", pool, actor, release, map[string]any{"old": in.OldDiskID, "new": in.DiskID},
		func(ctx context.Context, log jobs.Logger) error {
			log.Printf("Ersetze %s durch %s (wird gelöscht) …", in.OldDiskID, in.DiskID)
			req := BtrfsReplaceRequest{Name: pool, FSUUID: p.FSUUID, DevID: devID, Target: target}
			if err := m.agent.Call(ctx, "pool.btrfs.replace.start", req, nil); err != nil {
				return err
			}
			if err := m.waitReplace(ctx, log, pool); err != nil {
				return err
			}
			// Die neue Platte kann größer sein als die alte: ganz nutzen.
			if err := m.agent.Call(ctx, "pool.btrfs.resize", BtrfsResizeRequest{Name: pool, DevID: devID}, nil); err != nil {
				log.Printf("Hinweis: Die Platte konnte nicht auf volle Größe gebracht werden: %v", err)
			}
			return m.commitMembers(ctx, log, pool, branches, []Disk{nd}, []string{in.OldDiskID}, p.FSUUID, "")
		}), nil
}

// AddBtrfsDisk fügt dem Pool eine Platte hinzu (sie wird gelöscht); auf Wunsch verteilt eine Balance die Daten neu.
func (m *Manager) AddBtrfsDisk(ctx context.Context, actor, pool string, in BtrfsDiskInput) (string, error) {
	p, st, err := m.maintPool(ctx, pool)
	if err != nil {
		return "", err
	}
	if st.Degraded {
		return "", apierr.New(apierr.Conflict, "Der Pool ist unvollständig. Bitte zuerst die fehlende Platte ersetzen.")
	}
	target, nd, err := m.newDisk(ctx, pool, in.DiskID, in.ConfirmToken, in.ConfirmName)
	if err != nil {
		return "", err
	}
	release, err := m.beginBtrfs(pool)
	if err != nil {
		return "", err
	}
	branches := append(slices.Clone(p.Branches), in.DiskID)
	return m.runMaint("pool.adddisk", pool, actor, release, map[string]any{"disk": in.DiskID, "balance": in.Balance},
		func(ctx context.Context, log jobs.Logger) error {
			log.Printf("Füge %s dem Pool hinzu (wird gelöscht) …", in.DiskID)
			if err := m.agent.Call(ctx, "pool.btrfs.device.add", BtrfsAddRequest{Name: pool, Target: target}, nil); err != nil {
				return err
			}
			if err := m.commitMembers(ctx, log, pool, branches, []Disk{nd}, nil, p.FSUUID, ""); err != nil {
				return err
			}
			if !in.Balance {
				return nil
			}
			log.Printf("Verteile die Daten gleichmäßig auf alle Platten …")
			if err := m.startOp(ctx, BtrfsOpRequest{Name: pool, Op: OpBalance}); err != nil {
				return err
			}
			return m.waitOp(ctx, log, pool)
		}), nil
}

// RemoveBtrfsDisk nimmt ein Mitglied aus dem Pool; btrfs verschiebt vorher dessen Daten auf die übrigen Platten.
// Eine fehlende Platte wird ohne Verschieben entfernt (die Redundanz stellt btrfs aus den übrigen Kopien her).
func (m *Manager) RemoveBtrfsDisk(ctx context.Context, actor, pool string, in BtrfsDiskInput) (string, error) {
	p, st, err := m.maintPool(ctx, pool)
	if err != nil {
		return "", err
	}
	if in.ConfirmName != pool {
		return "", invalid("Zur Bestätigung den Poolnamen eingeben")
	}
	if !slices.Contains(p.Branches, in.DiskID) {
		return "", invalid("Die Platte " + in.DiskID + " gehört nicht zum Pool.")
	}
	if min := BtrfsMinDisks(p.Profile); len(p.Branches)-1 < min {
		return "", invalid(fmt.Sprintf("Das Profil %s braucht mindestens %d Datenträger. Ändere zuerst das Profil.", p.Profile, min))
	}
	devID, missing, err := deviceOf(st, in.DiskID)
	if err != nil {
		return "", err
	}
	release, err := m.beginBtrfs(pool)
	if err != nil {
		return "", err
	}
	branches := slices.DeleteFunc(slices.Clone(p.Branches), func(b string) bool { return b == in.DiskID })
	return m.runMaint("pool.removedisk", pool, actor, release, map[string]any{"disk": in.DiskID},
		func(ctx context.Context, log jobs.Logger) error {
			log.Printf("Entferne %s aus dem Pool …", in.DiskID)
			if err := m.startOp(ctx, BtrfsOpRequest{Name: pool, Op: OpRemove, DevID: devID, Missing: missing}); err != nil {
				return err
			}
			if err := m.waitOp(ctx, log, pool); err != nil {
				return err
			}
			return m.commitMembers(ctx, log, pool, branches, nil, []string{in.DiskID}, p.FSUUID, "")
		}), nil
}

// BalanceBtrfs verteilt die Daten neu. usage 0 = alles, sonst nur Blöcke bis zu dieser Füllung (1–100 %).
func (m *Manager) BalanceBtrfs(ctx context.Context, actor, pool string, usage int) (string, error) {
	if usage < 0 || usage > 100 {
		return "", invalid("Füllgrad muss zwischen 0 und 100 liegen")
	}
	if _, _, err := m.maintPool(ctx, pool); err != nil {
		return "", err
	}
	release, err := m.beginBtrfs(pool)
	if err != nil {
		return "", err
	}
	return m.runMaint("pool.balance", pool, actor, release, map[string]any{"usage": usage},
		func(ctx context.Context, log jobs.Logger) error {
			log.Printf("Starte Balance …")
			if err := m.startOp(ctx, BtrfsOpRequest{Name: pool, Op: OpBalance, Usage: usage}); err != nil {
				return err
			}
			return m.waitOp(ctx, log, pool)
		}), nil
}

// redundancy ordnet die Profile nach der Zahl der Platten, die ausfallen dürfen.
func redundancy(profile string) int {
	switch profile {
	case "raid1", "raid10":
		return 1
	case "raid1c3":
		return 2
	}
	return 0
}

// ConvertBtrfsProfile wechselt das Profil (z. B. single → raid1). Eine Verringerung der Redundanz braucht die Bestätigung mit dem Poolnamen.
func (m *Manager) ConvertBtrfsProfile(ctx context.Context, actor, pool, profile, confirmName string) (string, error) {
	p, st, err := m.maintPool(ctx, pool)
	if err != nil {
		return "", err
	}
	min := BtrfsMinDisks(profile)
	switch {
	case min == 0:
		return "", invalid("Unbekanntes btrfs-Profil: " + profile)
	case profile == p.Profile:
		return "", apierr.New(apierr.Conflict, "Der Pool hat dieses Profil schon.")
	case len(p.Branches) < min:
		return "", invalid(fmt.Sprintf("Das Profil %s braucht mindestens %d Datenträger", profile, min))
	case st.Degraded:
		return "", apierr.New(apierr.Conflict, "Der Pool ist unvollständig. Bitte zuerst die fehlende Platte ersetzen.")
	case redundancy(profile) < redundancy(p.Profile) && confirmName != pool:
		return "", invalid("Das Profil bietet weniger Ausfallsicherheit. Zur Bestätigung den Poolnamen eingeben.")
	}
	release, err := m.beginBtrfs(pool)
	if err != nil {
		return "", err
	}
	meta := BtrfsMetadataProfile(profile, len(p.Branches))
	return m.runMaint("pool.convert", pool, actor, release, map[string]any{"from": p.Profile, "to": profile},
		func(ctx context.Context, log jobs.Logger) error {
			log.Printf("Wechsle das Profil von %s auf %s …", p.Profile, profile)
			if err := m.startOp(ctx, BtrfsOpRequest{Name: pool, Op: OpConvert, Profile: profile, Metadata: meta}); err != nil {
				return err
			}
			if err := m.waitOp(ctx, log, pool); err != nil {
				return err
			}
			return m.commitMembers(ctx, log, pool, p.Branches, nil, nil, p.FSUUID, profile)
		}), nil
}

// MountBtrfsDegraded hängt einen unvollständigen Pool bis zum nächsten Neustart ein, damit man ihn reparieren kann.
func (m *Manager) MountBtrfsDegraded(ctx context.Context, actor, pool string) error {
	p, err := m.findBtrfsPool(ctx, pool)
	if err != nil {
		return err
	}
	err = m.agent.Call(ctx, "pool.btrfs.mountdegraded", BtrfsMountRequest{Name: pool, FSUUID: p.FSUUID, Compress: p.Compress}, nil)
	m.auditSimple(ctx, actor, "pool.mountdegraded", pool, err)
	if err == nil {
		m.hub.Publish(events.Message{Type: "pools.changed"})
	}
	return err
}

// ResetBtrfsErrors setzt die Fehlerzähler der Platten auf null (nach dem Beheben der Ursache).
func (m *Manager) ResetBtrfsErrors(ctx context.Context, actor, pool string) error {
	if _, err := m.findBtrfsPool(ctx, pool); err != nil {
		return err
	}
	err := m.agent.Call(ctx, "pool.btrfs.resetstats", map[string]string{"name": pool}, nil)
	m.auditSimple(ctx, actor, "pool.resetstats", pool, err)
	if err == nil {
		m.hub.Publish(events.Message{Type: "pools.changed"})
	}
	return err
}

func (m *Manager) auditSimple(ctx context.Context, actor, action, target string, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: action, Target: target, Result: result})
}
