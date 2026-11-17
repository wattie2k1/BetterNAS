package backup

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

// Agent ist der Teil des Agent-Clients, den der Manager braucht.
type Agent interface {
	Call(ctx context.Context, method string, params, result any) error
}

const (
	keepBackups = 7
	tokenTTL    = 15 * time.Minute
)

// Preview ist die Vorschau einer Wiederherstellung.
type Preview struct {
	Token    string    `json:"token"`
	Manifest Manifest  `json:"manifest"`
	Expires  time.Time `json:"expires"`
	Warnings []string  `json:"warnings"`
}

type pendingRestore struct {
	tmp      string
	dbPath   string
	manifest Manifest
	source   string
	expires  time.Time
}

// Manager verwaltet Sicherungen und Wiederherstellungen.
type Manager struct {
	db         *sql.DB
	agent      Agent
	dir        string
	restoreDir string
	version    string
	hub        *events.Hub
	now        func() time.Time
	after      func(time.Duration) <-chan time.Time

	mu     sync.Mutex
	tokens map[string]pendingRestore
}

// NewManager erzeugt einen Manager. dir ist das Backup-Verzeichnis; die ausstehende Wiederherstellung
// liegt im Nachbarverzeichnis „restore“ (dort sucht auch ApplyPending beim Start).
func NewManager(d *sql.DB, agent Agent, dir, version string, hub *events.Hub) *Manager {
	return &Manager{
		db: d, agent: agent, dir: dir, restoreDir: filepath.Join(filepath.Dir(dir), "restore"),
		version: version, hub: hub, now: time.Now, after: time.After, tokens: map[string]pendingRestore{},
	}
}

func (m *Manager) publish() {
	if m.hub != nil {
		m.hub.Publish(events.Message{Type: "backups.changed"})
	}
}

func (m *Manager) record(ctx context.Context, actor, action, target string, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: action, Target: target, Result: result})
}

// Create legt eine Sicherung an und räumt alte auf.
func (m *Manager) Create(ctx context.Context, actor string) (Info, error) {
	info, err := Create(ctx, m.db, m.dir, m.version)
	m.record(ctx, actor, "backup.create", info.Name, err)
	if err != nil {
		return Info{}, err
	}
	_ = Prune(m.dir, keepBackups)
	m.publish()
	return info, nil
}

// List liefert die abgelegten Sicherungen.
func (m *Manager) List(context.Context) ([]Info, error) { return List(m.dir) }

// Open öffnet eine Sicherung zum Herunterladen.
func (m *Manager) Open(name string) (*os.File, error) {
	if !NameRe.MatchString(name) {
		return nil, invalid("Ungültiger Backup-Name.")
	}
	f, err := os.Open(filepath.Join(m.dir, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, apierr.New(apierr.NotFound, "Backup nicht gefunden.")
		}
		return nil, err
	}
	return f, nil
}

// Audit hält fest, dass ein Backup heruntergeladen wurde.
func (m *Manager) Audit(ctx context.Context, actor, action, name string) {
	m.record(ctx, actor, action, name, nil)
}

// Delete löscht eine Sicherung.
func (m *Manager) Delete(ctx context.Context, actor, name string) error {
	err := Delete(m.dir, name)
	m.record(ctx, actor, "backup.delete", name, err)
	if err == nil {
		m.publish()
	}
	return err
}

func (m *Manager) cleanupLocked() {
	for t, p := range m.tokens {
		if m.now().After(p.expires) {
			os.RemoveAll(p.tmp)
			delete(m.tokens, t)
		}
	}
}

func (m *Manager) warnings(mf Manifest) []string {
	var w []string
	if mf.Version != "" && mf.Version != m.version {
		w = append(w, fmt.Sprintf("Das Backup stammt von BetterNAS %s, hier läuft %s.", mf.Version, m.version))
	}
	if !mf.CreatedAt.IsZero() && m.now().Sub(mf.CreatedAt) > 90*24*time.Hour {
		w = append(w, "Das Backup ist älter als 90 Tage.")
	}
	return w
}

// PreviewUpload prüft ein hochgeladenes Archiv und merkt es für die Wiederherstellung vor.
func (m *Manager) PreviewUpload(ctx context.Context, r io.Reader) (Preview, error) {
	return m.preview(ctx, r, "")
}

// PreviewStored prüft eine abgelegte Sicherung.
func (m *Manager) PreviewStored(ctx context.Context, name string) (Preview, error) {
	f, err := m.Open(name)
	if err != nil {
		return Preview{}, err
	}
	defer f.Close()
	return m.preview(ctx, f, name)
}

func (m *Manager) preview(ctx context.Context, r io.Reader, source string) (Preview, error) {
	m.mu.Lock()
	m.cleanupLocked()
	m.mu.Unlock()
	schema, err := SchemaVersion(m.db)
	if err != nil {
		return Preview{}, err
	}
	if err := os.MkdirAll(m.restoreDir, 0o700); err != nil {
		return Preview{}, err
	}
	tmp, err := os.MkdirTemp(m.restoreDir, "tmp-")
	if err != nil {
		return Preview{}, err
	}
	mf, dbPath, err := Inspect(r, tmp, schema)
	if err != nil {
		os.RemoveAll(tmp)
		return Preview{}, err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		os.RemoveAll(tmp)
		return Preview{}, err
	}
	token := hex.EncodeToString(b)
	exp := m.now().Add(tokenTTL)
	m.mu.Lock()
	m.tokens[token] = pendingRestore{tmp: tmp, dbPath: dbPath, manifest: mf, source: source, expires: exp}
	m.mu.Unlock()
	return Preview{Token: token, Manifest: mf, Expires: exp, Warnings: m.warnings(mf)}, nil
}

// Restore legt die vorgemerkte DB bereit und lässt die API über den Agent neu starten;
// beim Start wird sie dann eingesetzt (ApplyPending).
func (m *Manager) Restore(ctx context.Context, actor, token string) error {
	m.mu.Lock()
	p, ok := m.tokens[token]
	if ok {
		delete(m.tokens, token)
	}
	m.mu.Unlock()
	if !ok {
		return apierr.New(apierr.NotFound, "Unbekannte oder bereits verbrauchte Wiederherstellung.")
	}
	defer os.RemoveAll(p.tmp)
	if m.now().After(p.expires) {
		return apierr.New(apierr.Conflict, "Die Vorschau ist abgelaufen. Bitte das Backup erneut auswählen.")
	}
	target := p.source
	if target == "" {
		target = "upload"
	}
	if err := Stage(m.restoreDir, p.dbPath, p.manifest); err != nil {
		m.record(ctx, actor, "backup.restore", target, err)
		return err
	}
	// Vor dem Neustart auditieren: danach ist die DB eine andere.
	m.record(ctx, actor, "backup.restore", target, nil)
	if err := m.agent.Call(ctx, "system.restart-api", nil, nil); err != nil {
		Unstage(m.restoreDir)
		return err
	}
	return nil
}
