package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
)

// Agent ist der Teil des Agent-Clients, den der Manager braucht.
type Agent interface {
	Call(ctx context.Context, method string, params, result any) error
}

// DependentsFunc liefert, was einen Pool nutzt (z. B. "share:filme"); ab Phase 2 Freigaben und Stacks.
type DependentsFunc func(ctx context.Context, pool string) ([]string, error)

// SmartInfo ist eine Zusammenfassung der SMART-Werte.
type SmartInfo struct {
	Passed       bool `json:"passed"`
	TemperatureC int  `json:"temperatureC"`
	PowerOnHours int  `json:"powerOnHours"`
}

// DiskView ist ein Datenträger aus Sicht der Oberfläche.
type DiskView struct {
	Disk
	Role   string     `json:"role"`  // unassigned | data | parity | system
	State  string     `json:"state"` // online | missing
	Pool   string     `json:"pool,omitempty"`
	FSType string     `json:"fsType,omitempty"`
	Smart  *SmartInfo `json:"smart,omitempty"`
}

// FormatPreview zeigt, was beim Formatieren gelöscht wird.
type FormatPreview struct {
	DiskID       string      `json:"diskId"`
	Device       string      `json:"device"`
	Model        string      `json:"model"`
	Serial       string      `json:"serial"`
	SizeBytes    uint64      `json:"sizeBytes"`
	Partitions   []Partition `json:"partitions"`
	ConfirmToken string      `json:"confirmToken"`
	ExpiresAt    time.Time   `json:"expiresAt"`
}

// FormatRequest ist die Bestätigung des Nutzers.
type FormatRequest struct {
	FSType        string `json:"fsType"`
	ConfirmSerial string `json:"confirmSerial"`
	ConfirmToken  string `json:"confirmToken"`
}

// PoolInput sind die Eingaben für Anlegen und Bearbeiten eines Pools.
type PoolInput struct {
	Name         string   `json:"name"`
	Branches     []string `json:"branches"`
	CreatePolicy string   `json:"createPolicy"`
	MinFree      string   `json:"minFree"`
}

// PoolView ist ein Pool aus Sicht der Oberfläche.
type PoolView struct {
	PoolInput
	Path      string `json:"path"`
	SizeBytes uint64 `json:"sizeBytes"`
	FreeBytes uint64 `json:"freeBytes"`
	Mounted   bool   `json:"mounted"`
}

type formatToken struct {
	diskID, device string
	size           uint64
	expires        time.Time
}

type smartEntry struct {
	info *SmartInfo
	at   time.Time
}

// Manager verwaltet Datenträger und Pools: Soll-Zustand in SQLite, Umsetzung über den Agent.
type Manager struct {
	db    *sql.DB
	agent Agent
	jobs  *jobs.Manager
	hub   *events.Hub
	deps  []DependentsFunc

	mu      sync.Mutex // serialisiert alle Änderungen
	tmu     sync.Mutex
	tokens  map[string]formatToken
	smart   map[string]smartEntry
	now     func() time.Time
	statfs  func(path string) (size, free uint64, err error)
	mounted func(path string) bool
}

// NewManager erzeugt einen Manager.
func NewManager(d *sql.DB, agent Agent, j *jobs.Manager, hub *events.Hub, deps ...DependentsFunc) *Manager {
	return &Manager{
		db: d, agent: agent, jobs: j, hub: hub, deps: deps,
		tokens: map[string]formatToken{}, smart: map[string]smartEntry{},
		now: time.Now, statfs: statfs, mounted: isMounted,
	}
}

func statfs(path string) (uint64, uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bs := uint64(st.Bsize)
	return uint64(st.Blocks) * bs, uint64(st.Bavail) * bs, nil
}

func isMounted(path string) bool {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) > 4 && f[4] == path {
			return true
		}
	}
	return false
}

// --- Datenbank ---

type dbDisk struct {
	ID, Role, FSType, FSUUID, Label, Model, Serial string
	Size                                           uint64
}

func (m *Manager) loadDisks(ctx context.Context) (map[string]dbDisk, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id, role, fs_type, fs_uuid, label, COALESCE(model,''), COALESCE(serial,''), size_bytes FROM disks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]dbDisk{}
	for rows.Next() {
		var d dbDisk
		if err := rows.Scan(&d.ID, &d.Role, &d.FSType, &d.FSUUID, &d.Label, &d.Model, &d.Serial, &d.Size); err != nil {
			return nil, err
		}
		out[d.ID] = d
	}
	return out, rows.Err()
}

func (m *Manager) loadPools(ctx context.Context) ([]Pool, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT p.name, p.create_policy, p.min_free, COALESCE(b.disk_id, '')
		FROM pools p LEFT JOIN pool_branches b ON b.pool_name = p.name ORDER BY p.name, b.position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pool
	for rows.Next() {
		var name, policy, minFree, disk string
		if err := rows.Scan(&name, &policy, &minFree, &disk); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].Name != name {
			out = append(out, Pool{Name: name, CreatePolicy: policy, MinFree: minFree, Branches: []string{}})
		}
		if disk != "" {
			out[len(out)-1].Branches = append(out[len(out)-1].Branches, disk)
		}
	}
	return out, rows.Err()
}

func (m *Manager) currentState(ctx context.Context) (State, map[string]dbDisk, error) {
	disks, err := m.loadDisks(ctx)
	if err != nil {
		return State{}, nil, err
	}
	pools, err := m.loadPools(ctx)
	if err != nil {
		return State{}, nil, err
	}
	st := State{Pools: pools}
	ids := make([]string, 0, len(disks))
	for id := range disks {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		d := disks[id]
		st.Disks = append(st.Disks, DiskMount{DiskID: d.ID, FSUUID: d.FSUUID, FSType: d.FSType})
	}
	return st, disks, nil
}

func poolOf(pools []Pool, diskID string) string {
	for _, p := range pools {
		if slices.Contains(p.Branches, diskID) {
			return p.Name
		}
	}
	return ""
}

func (m *Manager) liveDisks(ctx context.Context) ([]Disk, error) {
	var disks []Disk
	if err := m.agent.Call(ctx, "disk.list", nil, &disks); err != nil {
		return nil, err
	}
	return disks, nil
}

func (m *Manager) liveDisk(ctx context.Context, id string) (Disk, error) {
	disks, err := m.liveDisks(ctx)
	if err != nil {
		return Disk{}, err
	}
	for _, d := range disks {
		if d.ID == id {
			return d, nil
		}
	}
	return Disk{}, apierr.New(apierr.NotFound, "Datenträger nicht gefunden")
}

// --- Lesen ---

func (m *Manager) smartFor(ctx context.Context, device string) *SmartInfo {
	m.tmu.Lock()
	e, ok := m.smart[device]
	m.tmu.Unlock()
	if ok && m.now().Sub(e.at) < 5*time.Minute {
		return e.info
	}
	var info SmartInfo
	var res *SmartInfo
	if err := m.agent.Call(ctx, "disk.smart", map[string]string{"device": device}, &info); err == nil {
		res = &info
	}
	m.tmu.Lock()
	m.smart[device] = smartEntry{info: res, at: m.now()}
	m.tmu.Unlock()
	return res
}

// ListDisks führt erkannte Datenträger mit dem gespeicherten Zustand zusammen.
func (m *Manager) ListDisks(ctx context.Context) ([]DiskView, error) {
	live, err := m.liveDisks(ctx)
	if err != nil {
		return nil, err
	}
	st, known, err := m.currentState(ctx)
	if err != nil {
		return nil, err
	}
	out := []DiskView{}
	seen := map[string]bool{}
	for _, d := range live {
		v := DiskView{Disk: d, State: "online", Role: "unassigned"}
		switch {
		case d.System:
			v.Role = "system"
		case known[d.ID].ID != "" && d.Manageable:
			v.Role = known[d.ID].Role
			v.FSType = known[d.ID].FSType
			v.Pool = poolOf(st.Pools, d.ID)
			seen[d.ID] = true
		}
		if v.FSType == "" && len(d.Partitions) > 0 {
			v.FSType = d.Partitions[0].FSType
		}
		v.Smart = m.smartFor(ctx, d.Device)
		out = append(out, v)
	}
	for id, k := range known {
		if seen[id] {
			continue
		}
		out = append(out, DiskView{
			Disk: Disk{ID: id, Model: k.Model, Serial: k.Serial, SizeBytes: k.Size, Partitions: []Partition{}, Reason: "Datenträger fehlt"},
			Role: k.Role, State: "missing", Pool: poolOf(st.Pools, id), FSType: k.FSType,
		})
	}
	return out, nil
}

// ListPools liefert alle Pools mit Belegung.
func (m *Manager) ListPools(ctx context.Context) ([]PoolView, error) {
	pools, err := m.loadPools(ctx)
	if err != nil {
		return nil, err
	}
	out := []PoolView{}
	for _, p := range pools {
		v := PoolView{PoolInput: PoolInput{Name: p.Name, Branches: p.Branches, CreatePolicy: p.CreatePolicy, MinFree: p.MinFree}, Path: PoolPath(p.Name)}
		v.Mounted = m.mounted(v.Path)
		if v.Mounted {
			v.SizeBytes, v.FreeBytes, _ = m.statfs(v.Path)
		}
		out = append(out, v)
	}
	return out, nil
}

// --- Änderungen ---

// run startet eine Änderung als Job: unter Sperre, mit Audit und Ereignis bei Erfolg.
func (m *Manager) run(kind, target, actor, event string, details map[string]any, fn func(ctx context.Context, log jobs.Logger) error) string {
	return m.jobs.Start(kind, target, actor, func(ctx context.Context, log jobs.Logger) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		err := fn(ctx, log)
		result := "ok"
		if err != nil {
			result = "error"
		}
		_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: kind, Target: target, Result: result, Details: details})
		if err == nil {
			m.hub.Publish(events.Message{Type: event})
		}
		return err
	})
}

// Reconcile schreibt die Mount-Units aus dem gespeicherten Zustand neu (beim Start der API).
// Unveränderte Units bleiben unberührt; nach einem Update werden geänderte Units ersetzt.
func (m *Manager) Reconcile(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, _, err := m.currentState(ctx)
	if err != nil {
		return err
	}
	return m.agent.Call(ctx, "storage.apply", st, nil)
}

// applyAndCommit wendet st über den Agent an und schreibt bei Erfolg die DB-Änderung.
func (m *Manager) applyAndCommit(ctx context.Context, log jobs.Logger, st State, commit func(tx *sql.Tx) error) error {
	log.Printf("Wende Konfiguration an …")
	log.SetProgress(50)
	if err := m.agent.Call(ctx, "storage.apply", st, nil); err != nil {
		return err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := commit(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// PreviewFormat prüft, ob eine Platte formatiert werden darf, und liefert ein einmaliges Bestätigungs-Token.
func (m *Manager) PreviewFormat(ctx context.Context, id string) (FormatPreview, error) {
	d, err := m.liveDisk(ctx, id)
	if err != nil {
		return FormatPreview{}, err
	}
	if err := m.checkUnassigned(ctx, d); err != nil {
		return FormatPreview{}, err
	}
	tok := formatToken{diskID: d.ID, device: d.Device, size: d.SizeBytes, expires: m.now().Add(5 * time.Minute)}
	t := newToken()
	m.tmu.Lock()
	m.tokens[t] = tok
	m.tmu.Unlock()
	return FormatPreview{DiskID: d.ID, Device: d.Device, Model: d.Model, Serial: d.Serial, SizeBytes: d.SizeBytes,
		Partitions: d.Partitions, ConfirmToken: t, ExpiresAt: tok.expires}, nil
}

func (m *Manager) checkUnassigned(ctx context.Context, d Disk) error {
	switch {
	case d.System:
		return apierr.New(apierr.SystemDisk, "Die Systemplatte kann nicht verwendet werden")
	case !d.Manageable:
		return apierr.New(apierr.Conflict, "Datenträger kann nicht verwaltet werden: "+d.Reason)
	case len(d.Mountpoints()) > 0:
		return apierr.New(apierr.Conflict, "Der Datenträger ist eingehängt: "+strings.Join(d.Mountpoints(), ", "))
	}
	known, err := m.loadDisks(ctx)
	if err != nil {
		return err
	}
	if _, ok := known[d.ID]; ok {
		return apierr.New(apierr.Conflict, "Der Datenträger wird bereits von BetterNAS verwendet")
	}
	return nil
}

func confirmValue(d Disk) string {
	if d.Serial != "" {
		return d.Serial
	}
	return d.ID
}

// Format formatiert eine Platte nach bestätigter Vorschau und nimmt sie als Datenplatte auf.
func (m *Manager) Format(ctx context.Context, actor, id string, req FormatRequest) (string, error) {
	if req.FSType != "ext4" && req.FSType != "xfs" {
		return "", apierr.New(apierr.InvalidParams, "Dateisystem muss ext4 oder xfs sein")
	}
	m.tmu.Lock()
	tok, ok := m.tokens[req.ConfirmToken]
	m.tmu.Unlock()
	if !ok || tok.diskID != id || m.now().After(tok.expires) {
		return "", apierr.New(apierr.Conflict, "Die Bestätigung ist abgelaufen. Bitte die Vorschau erneut öffnen.")
	}
	d, err := m.liveDisk(ctx, id)
	if err != nil {
		return "", err
	}
	if d.Device != tok.device || d.SizeBytes != tok.size {
		return "", apierr.New(apierr.Conflict, "Der Datenträger hat sich seit der Vorschau verändert. Bitte erneut prüfen.")
	}
	if req.ConfirmSerial != confirmValue(d) {
		return "", apierr.New(apierr.InvalidParams, "Die eingegebene Seriennummer stimmt nicht überein")
	}
	if err := m.checkUnassigned(ctx, d); err != nil {
		return "", err
	}
	m.tmu.Lock()
	delete(m.tokens, req.ConfirmToken)
	m.tmu.Unlock()

	return m.run("disk.format", id, actor, "disks.changed", map[string]any{"device": d.Device, "fsType": req.FSType},
		func(ctx context.Context, log jobs.Logger) error {
			log.Printf("Formatiere %s (%s) mit %s …", d.Device, d.Model, req.FSType)
			var res struct {
				FSUUID string `json:"fsUuid"`
				Label  string `json:"label"`
			}
			if err := m.agent.Call(ctx, "disk.format", map[string]any{"diskId": d.ID, "device": tok.device, "sizeBytes": tok.size, "fsType": req.FSType}, &res); err != nil {
				return err
			}
			return m.addDataDisk(ctx, log, d, req.FSType, res.FSUUID, res.Label)
		}), nil
}

func (m *Manager) addDataDisk(ctx context.Context, log jobs.Logger, d Disk, fsType, uuid, label string) error {
	st, _, err := m.currentState(ctx)
	if err != nil {
		return err
	}
	st.Disks = append(st.Disks, DiskMount{DiskID: d.ID, FSUUID: uuid, FSType: fsType})
	return m.applyAndCommit(ctx, log, st, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO disks(id, role, fs_type, fs_uuid, label, model, serial, size_bytes, created_at)
			VALUES (?, 'data', ?, ?, ?, ?, ?, ?, ?)`, d.ID, fsType, uuid, label, d.Model, d.Serial, d.SizeBytes, m.now().UTC().Format(time.RFC3339))
		return err
	})
}

// Import übernimmt eine Platte mit vorhandenem Dateisystem, ohne sie zu formatieren.
func (m *Manager) Import(ctx context.Context, actor, id string) (string, error) {
	d, err := m.liveDisk(ctx, id)
	if err != nil {
		return "", err
	}
	if err := m.checkUnassigned(ctx, d); err != nil {
		return "", err
	}
	var part *Partition
	for i, p := range d.Partitions {
		if slices.Contains(fsTypes, p.FSType) && len(p.Mountpoints) == 0 && p.UUID != "" {
			part = &d.Partitions[i]
			break
		}
	}
	if part == nil {
		return "", apierr.New(apierr.InvalidParams, "Kein unterstütztes Dateisystem gefunden (ext4, xfs oder btrfs)")
	}
	p := *part
	return m.run("disk.import", id, actor, "disks.changed", map[string]any{"partition": p.Device},
		func(ctx context.Context, log jobs.Logger) error {
			log.Printf("Übernehme %s (%s) ohne Formatierung …", p.Device, p.FSType)
			return m.addDataDisk(ctx, log, d, p.FSType, p.UUID, p.Label)
		}), nil
}

// Release gibt eine Datenplatte frei (hängt sie aus); die Daten bleiben erhalten.
func (m *Manager) Release(ctx context.Context, actor, id string) (string, error) {
	st, known, err := m.currentState(ctx)
	if err != nil {
		return "", err
	}
	if _, ok := known[id]; !ok {
		return "", apierr.New(apierr.NotFound, "Datenträger wird nicht von BetterNAS verwaltet")
	}
	if p := poolOf(st.Pools, id); p != "" {
		return "", apierr.New(apierr.DiskInUse, "Der Datenträger gehört zu Pool "+p).With("pool", p)
	}
	return m.run("disk.release", id, actor, "disks.changed", nil, func(ctx context.Context, log jobs.Logger) error {
		st, _, err := m.currentState(ctx)
		if err != nil {
			return err
		}
		st.Disks = slices.DeleteFunc(st.Disks, func(d DiskMount) bool { return d.DiskID == id })
		return m.applyAndCommit(ctx, log, st, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `DELETE FROM disks WHERE id = ?`, id)
			return err
		})
	}), nil
}

// validatePool prüft Eingaben gegen den aktuellen Zustand und liefert den neuen Zustand.
func (m *Manager) validatePool(ctx context.Context, in PoolInput, replacing string) (State, error) {
	if in.CreatePolicy == "" {
		in.CreatePolicy = DefaultPolicy
	}
	if in.MinFree == "" {
		in.MinFree = DefaultMinFree
	}
	st, known, err := m.currentState(ctx)
	if err != nil {
		return State{}, err
	}
	if !PoolNameRe.MatchString(in.Name) {
		return State{}, invalid("Poolname: nur Kleinbuchstaben, Ziffern und Bindestrich, beginnend mit Buchstabe, max. 32 Zeichen")
	}
	if replacing == "" && slices.ContainsFunc(st.Pools, func(p Pool) bool { return p.Name == in.Name }) {
		return State{}, apierr.New(apierr.Conflict, "Ein Pool mit diesem Namen existiert bereits")
	}
	if len(in.Branches) == 0 {
		return State{}, invalid("Ein Pool braucht mindestens einen Datenträger")
	}
	for _, b := range in.Branches {
		if known[b].Role != "data" {
			return State{}, invalid("Nur formatierte oder übernommene Datenplatten können in einen Pool: " + b)
		}
		if p := poolOf(st.Pools, b); p != "" && p != replacing {
			return State{}, apierr.New(apierr.Conflict, "Datenträger "+b+" gehört bereits zu Pool "+p).With("pool", p)
		}
	}
	st.Pools = slices.DeleteFunc(st.Pools, func(p Pool) bool { return p.Name == replacing })
	st.Pools = append(st.Pools, Pool{Name: in.Name, Branches: in.Branches, CreatePolicy: in.CreatePolicy, MinFree: in.MinFree})
	if err := st.Validate(); err != nil {
		return State{}, err
	}
	return st, nil
}

func writePool(ctx context.Context, tx *sql.Tx, p Pool, now time.Time, create bool) error {
	if create {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pools(name, create_policy, min_free, created_at) VALUES (?,?,?,?)`,
			p.Name, p.CreatePolicy, p.MinFree, now.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE pools SET create_policy = ?, min_free = ? WHERE name = ?`, p.CreatePolicy, p.MinFree, p.Name); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM pool_branches WHERE pool_name = ?`, p.Name); err != nil {
			return err
		}
	}
	for i, b := range p.Branches {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pool_branches(pool_name, disk_id, position) VALUES (?,?,?)`, p.Name, b, i); err != nil {
			return err
		}
	}
	return nil
}

func findPool(st State, name string) Pool {
	for _, p := range st.Pools {
		if p.Name == name {
			return p
		}
	}
	return Pool{}
}

// CreatePool legt einen mergerfs-Pool an.
func (m *Manager) CreatePool(ctx context.Context, actor string, in PoolInput) (string, error) {
	if _, err := m.validatePool(ctx, in, ""); err != nil {
		return "", err
	}
	return m.run("pool.create", in.Name, actor, "pools.changed", map[string]any{"branches": in.Branches},
		func(ctx context.Context, log jobs.Logger) error {
			st, err := m.validatePool(ctx, in, "")
			if err != nil {
				return err
			}
			log.Printf("Lege Pool %s an …", in.Name)
			return m.applyAndCommit(ctx, log, st, func(tx *sql.Tx) error {
				return writePool(ctx, tx, findPool(st, in.Name), m.now(), true)
			})
		}), nil
}

func (m *Manager) poolExists(ctx context.Context, name string) error {
	pools, err := m.loadPools(ctx)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(pools, func(p Pool) bool { return p.Name == name }) {
		return apierr.New(apierr.NotFound, "Pool nicht gefunden")
	}
	return nil
}

// UpdatePool ändert Datenträger, Strategie oder Mindestfreiraum eines Pools (Name bleibt).
func (m *Manager) UpdatePool(ctx context.Context, actor, name string, in PoolInput) (string, error) {
	if err := m.poolExists(ctx, name); err != nil {
		return "", err
	}
	in.Name = name
	if _, err := m.validatePool(ctx, in, name); err != nil {
		return "", err
	}
	return m.run("pool.update", name, actor, "pools.changed", map[string]any{"branches": in.Branches},
		func(ctx context.Context, log jobs.Logger) error {
			st, err := m.validatePool(ctx, in, name)
			if err != nil {
				return err
			}
			log.Printf("Ändere Pool %s …", name)
			return m.applyAndCommit(ctx, log, st, func(tx *sql.Tx) error {
				return writePool(ctx, tx, findPool(st, name), m.now(), false)
			})
		}), nil
}

// DeletePool entfernt einen Pool. Die Daten auf den Platten bleiben erhalten.
func (m *Manager) DeletePool(ctx context.Context, actor, name string) (string, error) {
	if err := m.poolExists(ctx, name); err != nil {
		return "", err
	}
	var dependents []string
	for _, f := range m.deps {
		d, err := f(ctx, name)
		if err != nil {
			return "", err
		}
		dependents = append(dependents, d...)
	}
	if len(dependents) > 0 {
		return "", apierr.New(apierr.Conflict, "Der Pool wird noch verwendet: "+strings.Join(dependents, ", ")).With("dependents", dependents)
	}
	return m.run("pool.delete", name, actor, "pools.changed", nil, func(ctx context.Context, log jobs.Logger) error {
		st, _, err := m.currentState(ctx)
		if err != nil {
			return err
		}
		st.Pools = slices.DeleteFunc(st.Pools, func(p Pool) bool { return p.Name == name })
		log.Printf("Entferne Pool %s …", name)
		return m.applyAndCommit(ctx, log, st, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `DELETE FROM pools WHERE name = ?`, name)
			return err
		})
	}), nil
}
