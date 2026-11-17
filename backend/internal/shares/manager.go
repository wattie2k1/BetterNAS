package shares

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/users"
)

// Agent ist der Teil des Agent-Clients, den der Manager braucht.
type Agent interface {
	Call(ctx context.Context, method string, params, result any) error
}

// ShareView ist eine Freigabe mit abgeleiteten Angaben für die Oberfläche.
type ShareView struct {
	Share
	Path        string `json:"path"`
	PoolMounted bool   `json:"poolMounted"`
	UNCPath     string `json:"uncPath"`
	// NFSMount ist die Mount-Quelle „host:/pfad“, wenn der NFS-Export aktiv ist.
	NFSMount string `json:"nfsMount,omitempty"`
}

// Manager verwaltet Freigaben: Datenbank, Validierung und Anwendung über den Agent.
type Manager struct {
	db    *sql.DB
	agent Agent
	jobs  *jobs.Manager
	hub   *events.Hub

	mu       sync.Mutex // serialisiert alle Änderungen
	now      func() time.Time
	mounted  func(path string) bool
	hostname func() string
}

// NewManager erzeugt einen Manager.
func NewManager(d *sql.DB, agent Agent, j *jobs.Manager, hub *events.Hub) *Manager {
	return &Manager{
		db: d, agent: agent, jobs: j, hub: hub,
		now: time.Now, mounted: isMounted,
		hostname: func() string {
			h, err := os.Hostname()
			if err != nil || h == "" {
				return "betternas"
			}
			return h
		},
	}
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

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// load liest alle Freigaben samt Rechten (Reihenfolge der Rechte bleibt erhalten).
func (m *Manager) load(ctx context.Context) (State, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT name, pool, subdir, comment, browsable, guest, guest_write, recycle, fruit, nfs_json FROM shares ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return State{}, err
	}
	st := State{Shares: []Share{}}
	index := map[string]int{}
	for rows.Next() {
		var s Share
		var br, gu, gw, re, fr int
		var nfsJSON string
		if err := rows.Scan(&s.Name, &s.Pool, &s.Subdir, &s.Comment, &br, &gu, &gw, &re, &fr, &nfsJSON); err != nil {
			rows.Close()
			return State{}, err
		}
		s.Browsable, s.Guest, s.GuestWrite, s.Recycle, s.Fruit = br == 1, gu == 1, gw == 1, re == 1, fr == 1
		s.ACL = []ACL{}
		if nfsJSON != "" {
			_ = json.Unmarshal([]byte(nfsJSON), &s.NFS)
		}
		s = normalize(s)
		index[strings.ToLower(s.Name)] = len(st.Shares)
		st.Shares = append(st.Shares, s)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return State{}, err
	}
	rows.Close()

	acls, err := m.db.QueryContext(ctx, `SELECT share_name, principal_type, principal_name, access FROM share_acl ORDER BY rowid`)
	if err != nil {
		return State{}, err
	}
	defer acls.Close()
	for acls.Next() {
		var name string
		var a ACL
		if err := acls.Scan(&name, &a.Type, &a.Name, &a.Access); err != nil {
			return State{}, err
		}
		if i, ok := index[strings.ToLower(name)]; ok {
			st.Shares[i].ACL = append(st.Shares[i].ACL, a)
		}
	}
	return st, acls.Err()
}

func (m *Manager) view(s Share) ShareView {
	v := ShareView{
		Share: s, Path: s.Path(),
		PoolMounted: m.mounted(storage.PoolPath(s.Pool)),
		UNCPath:     `\\` + m.hostname() + `\` + s.Name,
	}
	if s.NFS.Enabled {
		v.NFSMount = m.hostname() + ":" + s.Path()
	}
	return v
}

// List liefert alle Freigaben.
func (m *Manager) List(ctx context.Context) ([]ShareView, error) {
	st, err := m.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ShareView, 0, len(st.Shares))
	for _, s := range st.Shares {
		out = append(out, m.view(s))
	}
	return out, nil
}

// Get liefert eine Freigabe; die Schreibweise des Namens ist egal.
func (m *Manager) Get(ctx context.Context, name string) (ShareView, error) {
	st, err := m.load(ctx)
	if err != nil {
		return ShareView{}, err
	}
	if i := slices.IndexFunc(st.Shares, func(s Share) bool { return strings.EqualFold(s.Name, name) }); i >= 0 {
		return m.view(st.Shares[i]), nil
	}
	return ShareView{}, apierr.New(apierr.NotFound, "Freigabe nicht gefunden")
}

// check prüft eine neue oder geänderte Freigabe gegen den bisherigen Zustand, Pools und Konten.
// replacing ist der Name der Freigabe, die ersetzt wird ("" bei neuen).
func (m *Manager) check(ctx context.Context, st State, in Share, replacing string) (State, error) {
	if err := in.Validate(); err != nil {
		return State{}, err
	}
	next := State{Shares: slices.DeleteFunc(slices.Clone(st.Shares), func(s Share) bool { return strings.EqualFold(s.Name, replacing) })}
	if slices.ContainsFunc(next.Shares, func(s Share) bool { return strings.EqualFold(s.Name, in.Name) }) {
		return State{}, apierr.New(apierr.Conflict, "Es gibt bereits eine Freigabe mit diesem Namen.").With("field", "name")
	}
	var n int
	if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pools WHERE name = ?`, in.Pool).Scan(&n); err != nil {
		return State{}, err
	}
	if n == 0 {
		return State{}, apierr.New(apierr.InvalidParams, "Der Pool existiert nicht.").With("field", "pool")
	}
	var acc users.Accounts
	if err := m.agent.Call(ctx, "account.list", nil, &acc); err != nil {
		return State{}, err
	}
	for _, a := range in.ACL {
		known := slices.ContainsFunc(acc.Users, func(u users.User) bool { return u.Name == a.Name })
		if a.Type == TypeGroup {
			known = slices.ContainsFunc(acc.Groups, func(g users.Group) bool { return g.Name == a.Name })
		}
		if !known {
			return State{}, apierr.New(apierr.InvalidParams, "Unbekannter Benutzer oder unbekannte Gruppe: "+a.Name).With("field", "acl")
		}
	}
	next.Shares = append(next.Shares, in)
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

func (m *Manager) run(kind, target, actor string, fn func(ctx context.Context, log jobs.Logger) error) string {
	return m.jobs.Start(kind, target, actor, func(ctx context.Context, log jobs.Logger) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		err := fn(ctx, log)
		result := "ok"
		if err != nil {
			result = "error"
		}
		_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: kind, Target: target, Result: result})
		if err == nil {
			m.hub.Publish(events.Message{Type: "shares.changed"})
		}
		return err
	})
}

func (m *Manager) applyAndCommit(ctx context.Context, log jobs.Logger, st State, commit func(tx *sql.Tx) error) error {
	log.Printf("Wende Samba-Konfiguration an …")
	log.SetProgress(50)
	if err := m.agent.Call(ctx, "share.apply", st, nil); err != nil {
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

func writeShare(ctx context.Context, tx *sql.Tx, s Share, now time.Time, create bool) error {
	if create {
		if _, err := tx.ExecContext(ctx, `INSERT INTO shares(name, pool, subdir, comment, browsable, guest, guest_write, recycle, fruit, nfs_json, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			s.Name, s.Pool, s.Subdir, s.Comment, b2i(s.Browsable), b2i(s.Guest), b2i(s.GuestWrite), b2i(s.Recycle), b2i(s.Fruit), nfsJSON(s.NFS), now.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE shares SET pool=?, subdir=?, comment=?, browsable=?, guest=?, guest_write=?, recycle=?, fruit=?, nfs_json=? WHERE name = ?`,
			s.Pool, s.Subdir, s.Comment, b2i(s.Browsable), b2i(s.Guest), b2i(s.GuestWrite), b2i(s.Recycle), b2i(s.Fruit), nfsJSON(s.NFS), s.Name); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM share_acl WHERE share_name = ?`, s.Name); err != nil {
			return err
		}
	}
	for _, a := range s.ACL {
		if _, err := tx.ExecContext(ctx, `INSERT INTO share_acl(share_name, principal_type, principal_name, access) VALUES (?,?,?,?)`, s.Name, a.Type, a.Name, a.Access); err != nil {
			return err
		}
	}
	return nil
}

func normalize(s Share) Share {
	if s.ACL == nil {
		s.ACL = []ACL{}
	}
	if s.NFS.Clients == nil {
		s.NFS.Clients = []string{}
	}
	if s.NFS.Squash == "" {
		s.NFS.Squash = SquashRoot
	}
	return s
}

// nfsJSON liefert den gespeicherten Wert: leer, solange NFS nie konfiguriert wurde.
func nfsJSON(n NFS) string {
	if !n.Enabled && len(n.Clients) == 0 && !n.ReadOnly {
		return ""
	}
	b, _ := json.Marshal(n)
	return string(b)
}

// Create legt eine Freigabe an.
func (m *Manager) Create(ctx context.Context, actor string, in Share) (string, error) {
	in = normalize(in)
	st, err := m.load(ctx)
	if err != nil {
		return "", err
	}
	if _, err := m.check(ctx, st, in, ""); err != nil {
		return "", err
	}
	return m.run("share.create", in.Name, actor, func(ctx context.Context, log jobs.Logger) error {
		st, err := m.load(ctx)
		if err != nil {
			return err
		}
		next, err := m.check(ctx, st, in, "")
		if err != nil {
			return err
		}
		log.Printf("Lege Freigabe %s an …", in.Name)
		return m.applyAndCommit(ctx, log, next, func(tx *sql.Tx) error { return writeShare(ctx, tx, in, m.now(), true) })
	}), nil
}

// Update ändert eine Freigabe; der Name bleibt (die Schreibweise des angelegten Namens gilt).
func (m *Manager) Update(ctx context.Context, actor, name string, in Share) (string, error) {
	st, err := m.load(ctx)
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(st.Shares, func(s Share) bool { return strings.EqualFold(s.Name, name) })
	if i < 0 {
		return "", apierr.New(apierr.NotFound, "Freigabe nicht gefunden")
	}
	name = st.Shares[i].Name
	in = normalize(in)
	in.Name = name
	if _, err := m.check(ctx, st, in, name); err != nil {
		return "", err
	}
	return m.run("share.update", name, actor, func(ctx context.Context, log jobs.Logger) error {
		st, err := m.load(ctx)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(st.Shares, func(s Share) bool { return s.Name == name }) {
			return apierr.New(apierr.NotFound, "Freigabe nicht gefunden")
		}
		next, err := m.check(ctx, st, in, name)
		if err != nil {
			return err
		}
		log.Printf("Ändere Freigabe %s …", name)
		return m.applyAndCommit(ctx, log, next, func(tx *sql.Tx) error { return writeShare(ctx, tx, in, m.now(), false) })
	}), nil
}

// Delete entfernt die Freigabe. Die Daten im Ordner bleiben erhalten.
func (m *Manager) Delete(ctx context.Context, actor, name string) (string, error) {
	v, err := m.Get(ctx, name)
	if err != nil {
		return "", err
	}
	name = v.Name
	return m.run("share.delete", name, actor, func(ctx context.Context, log jobs.Logger) error {
		st, err := m.load(ctx)
		if err != nil {
			return err
		}
		st.Shares = slices.DeleteFunc(st.Shares, func(s Share) bool { return s.Name == name })
		log.Printf("Entferne Freigabe %s …", name)
		return m.applyAndCommit(ctx, log, st, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `DELETE FROM shares WHERE name = ?`, name)
			return err
		})
	}), nil
}

// Reconcile schreibt die Samba-Konfiguration aus dem gespeicherten Zustand neu (beim Start der API).
func (m *Manager) Reconcile(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.load(ctx)
	if err != nil {
		return err
	}
	return m.agent.Call(ctx, "share.apply", st, nil)
}

// PoolDependents nennt die Freigaben auf einem Pool (für storage.DependentsFunc).
func (m *Manager) PoolDependents(ctx context.Context, pool string) ([]string, error) {
	st, err := m.load(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range st.Shares {
		if s.Pool == pool {
			out = append(out, "share:"+s.Name)
		}
	}
	return out, nil
}

// PrincipalUsage nennt die Freigaben, in denen ein Benutzer (kind "user") oder eine Gruppe (kind "group") berechtigt ist.
func (m *Manager) PrincipalUsage(ctx context.Context, kind, name string) ([]string, error) {
	st, err := m.load(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range st.Shares {
		if slices.ContainsFunc(s.ACL, func(a ACL) bool { return a.Type == kind && a.Name == name }) {
			out = append(out, "share:"+s.Name)
		}
	}
	return out, nil
}
