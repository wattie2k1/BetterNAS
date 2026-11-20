package storage

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/jobs"
)

// BtrfsDisk beschreibt eine Platte so, wie der Nutzer sie bestätigt hat.
type BtrfsDisk struct {
	DiskID    string `json:"diskId"`
	Device    string `json:"device"`
	SizeBytes uint64 `json:"sizeBytes"`
}

// BtrfsCreateRequest ist die Anfrage an den Agent: Platten löschen und ein btrfs anlegen.
type BtrfsCreateRequest struct {
	Name    string      `json:"name"`
	Profile string      `json:"profile"`
	Disks   []BtrfsDisk `json:"disks"`
}

// BtrfsCreateResult ist die Antwort des Agents.
type BtrfsCreateResult struct {
	FSUUID string `json:"fsUuid"`
	Label  string `json:"label"`
}

// BtrfsLabel ist die Bezeichnung des Dateisystems eines btrfs-Pools.
func BtrfsLabel(pool string) string { return "bnas-" + pool }

// PoolPreview zeigt, was beim Anlegen eines btrfs-Pools gelöscht wird.
type PoolPreview struct {
	Disks        []FormatPreview `json:"disks"`
	ConfirmToken string          `json:"confirmToken"`
	ExpiresAt    time.Time       `json:"expiresAt"`
}

type poolToken struct {
	disks   []BtrfsDisk
	expires time.Time
}

func (m *Manager) takePoolToken(token string) (poolToken, bool) {
	m.tmu.Lock()
	defer m.tmu.Unlock()
	t, ok := m.poolTokens[token]
	return t, ok
}

// rejectBtrfs lehnt eine Änderung ab, wenn der Pool ein btrfs-Pool ist.
func (m *Manager) rejectBtrfs(ctx context.Context, name, msg string) error {
	pools, err := m.loadPools(ctx)
	if err != nil {
		return err
	}
	for _, p := range pools {
		if p.Name == name && p.IsBtrfs() {
			return apierr.New(apierr.Conflict, msg)
		}
	}
	return nil
}

// PreviewBtrfsPool prüft, ob die Platten für einen neuen btrfs-Pool gelöscht werden dürfen,
// und liefert ein einmaliges Bestätigungs-Token für alle zusammen.
func (m *Manager) PreviewBtrfsPool(ctx context.Context, ids []string) (PoolPreview, error) {
	if len(ids) == 0 {
		return PoolPreview{}, invalid("Keine Datenträger ausgewählt")
	}
	out := PoolPreview{Disks: []FormatPreview{}}
	var disks []BtrfsDisk
	for i, id := range ids {
		if slices.Contains(ids[:i], id) {
			return PoolPreview{}, invalid("Datenträger doppelt ausgewählt: " + id)
		}
		d, err := m.liveDisk(ctx, id)
		if err != nil {
			return PoolPreview{}, err
		}
		if err := m.checkUnassigned(ctx, d); err != nil {
			return PoolPreview{}, err
		}
		disks = append(disks, BtrfsDisk{DiskID: d.ID, Device: d.Device, SizeBytes: d.SizeBytes})
		out.Disks = append(out.Disks, FormatPreview{DiskID: d.ID, Device: d.Device, Model: d.Model, Serial: d.Serial,
			Transport: d.Transport, SizeBytes: d.SizeBytes, Partitions: d.Partitions})
	}
	out.ExpiresAt = m.now().Add(5 * time.Minute)
	out.ConfirmToken = newToken()
	m.tmu.Lock()
	m.poolTokens[out.ConfirmToken] = poolToken{disks: disks, expires: out.ExpiresAt}
	m.tmu.Unlock()
	return out, nil
}

// checkBtrfsInput prüft Name, Profil, Kompression und Plattenzahl (ohne Zustand).
func checkBtrfsInput(in PoolInput) error {
	if !PoolNameRe.MatchString(in.Name) {
		return invalid("Poolname: nur Kleinbuchstaben, Ziffern und Bindestrich, beginnend mit Buchstabe, max. 32 Zeichen")
	}
	min := BtrfsMinDisks(in.Profile)
	if min == 0 {
		return invalid("Unbekanntes btrfs-Profil: " + in.Profile)
	}
	if len(in.Branches) < min {
		return invalid(fmt.Sprintf("Das Profil %s braucht mindestens %d Datenträger", in.Profile, min))
	}
	if !slices.Contains(BtrfsCompress, in.Compress) {
		return invalid("Unbekannte Kompression: " + in.Compress)
	}
	return nil
}

// createBtrfsPool legt einen btrfs-Pool an: Platten löschen, gemeinsames Dateisystem erzeugen, einhängen.
func (m *Manager) createBtrfsPool(ctx context.Context, actor string, in PoolInput) (string, error) {
	if err := checkBtrfsInput(in); err != nil {
		return "", err
	}
	if in.ConfirmName != in.Name {
		return "", invalid("Zur Bestätigung den Poolnamen eingeben")
	}
	tok, ok := m.takePoolToken(in.ConfirmToken)
	if !ok || m.now().After(tok.expires) {
		return "", apierr.New(apierr.Conflict, "Die Bestätigung ist abgelaufen. Bitte die Vorschau erneut öffnen.")
	}
	var ids []string
	for _, d := range tok.disks {
		ids = append(ids, d.DiskID)
	}
	if len(ids) != len(in.Branches) || !slices.EqualFunc(slices.Sorted(slices.Values(ids)), slices.Sorted(slices.Values(in.Branches)), func(a, b string) bool { return a == b }) {
		return "", apierr.New(apierr.Conflict, "Die Auswahl hat sich seit der Vorschau verändert. Bitte erneut prüfen.")
	}
	if err := m.poolNameFree(ctx, in.Name); err != nil {
		return "", err
	}
	var live []Disk
	for _, t := range tok.disks {
		d, err := m.liveDisk(ctx, t.DiskID)
		if err != nil {
			return "", err
		}
		if d.Device != t.Device || d.SizeBytes != t.SizeBytes {
			return "", apierr.New(apierr.Conflict, "Ein Datenträger hat sich seit der Vorschau verändert. Bitte erneut prüfen.")
		}
		if err := m.checkUnassigned(ctx, d); err != nil {
			return "", err
		}
		live = append(live, d)
	}
	m.tmu.Lock()
	delete(m.poolTokens, in.ConfirmToken)
	m.tmu.Unlock()

	return m.run("pool.create", in.Name, actor, "pools.changed", map[string]any{"type": PoolBtrfs, "profile": in.Profile, "branches": in.Branches},
		func(ctx context.Context, log jobs.Logger) error {
			if err := m.poolNameFree(ctx, in.Name); err != nil {
				return err
			}
			log.Printf("Lege btrfs-Pool %s (%s) auf %d Platten an …", in.Name, in.Profile, len(live))
			var res BtrfsCreateResult
			req := BtrfsCreateRequest{Name: in.Name, Profile: in.Profile, Disks: tok.disks}
			if err := m.agent.Call(ctx, "pool.btrfs.create", req, &res); err != nil {
				return err
			}
			st, _, err := m.currentState(ctx)
			if err != nil {
				return err
			}
			for _, d := range live {
				st.Disks = append(st.Disks, DiskMount{DiskID: d.ID, FSUUID: res.FSUUID, FSType: "btrfs", Role: RoleData})
			}
			pool := Pool{Name: in.Name, Type: PoolBtrfs, Branches: in.Branches, CreatePolicy: DefaultPolicy, MinFree: DefaultMinFree,
				Profile: in.Profile, FSUUID: res.FSUUID, Compress: in.Compress}
			st.Pools = append(st.Pools, pool)
			if err := st.Validate(); err != nil {
				return err
			}
			return m.applyAndCommit(ctx, log, st, func(tx *sql.Tx) error {
				for _, d := range live {
					if _, err := tx.ExecContext(ctx, `INSERT INTO disks(id, role, fs_type, fs_uuid, label, model, serial, size_bytes, created_at)
						VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, d.ID, RoleData, "btrfs", res.FSUUID, cmpOr(res.Label, BtrfsLabel(in.Name)), d.Model, d.Serial, d.SizeBytes, m.now().UTC().Format(time.RFC3339)); err != nil {
						return err
					}
				}
				return writePool(ctx, tx, pool, m.now(), true)
			})
		}), nil
}

// poolNameFree meldet einen Konflikt, wenn der Poolname schon vergeben ist.
func (m *Manager) poolNameFree(ctx context.Context, name string) error {
	pools, err := m.loadPools(ctx)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(pools, func(p Pool) bool { return p.Name == name }) {
		return apierr.New(apierr.Conflict, "Ein Pool mit diesem Namen existiert bereits")
	}
	return nil
}
