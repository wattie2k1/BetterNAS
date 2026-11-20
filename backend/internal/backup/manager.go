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

const tokenTTL = 15 * time.Minute

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
	wake       chan struct{} // Einstellungen geändert → Planer neu berechnen

	mu     sync.Mutex
	tokens map[string]pendingRestore

	appdataMu sync.Mutex // eine appdata-Sicherung zur Zeit
}

// NewManager erzeugt einen Manager. dir ist das Backup-Verzeichnis; die ausstehende Wiederherstellung
// liegt im Nachbarverzeichnis „restore“ (dort sucht auch ApplyPending beim Start).
func NewManager(d *sql.DB, agent Agent, dir, version string, hub *events.Hub) *Manager {
	return &Manager{
		db: d, agent: agent, dir: dir, restoreDir: filepath.Join(filepath.Dir(dir), "restore"),
		version: version, hub: hub, now: time.Now, after: time.After, wake: make(chan struct{}, 1), tokens: map[string]pendingRestore{},
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

// Status beschreibt den aktuellen Sicherungsort.
type Status struct {
	Dir     string `json:"dir"`     // Ordner, in dem gesichert wird
	Default bool   `json:"default"` // true = Systemplatte (Standard)
	Appdata string `json:"appdata"` // Ordner der App-Einstellungen; leer, solange Docker nicht eingerichtet ist
	Free    int64  `json:"free"`    // freie Bytes am Sicherungsort (0 = unbekannt)
	Problem string `json:"problem"` // warum der Ort nicht nutzbar ist
}

type target struct {
	Dir     string `json:"dir"`
	Free    int64  `json:"free"`
	Appdata string `json:"appdata"`
}

// where liefert den Sicherungsordner der Einstellungen (m.dir = Systemplatte).
func (m *Manager) where(c Config) string {
	if c.Dir == "" {
		return m.dir
	}
	d, err := dirCheck(c.Dir)
	if err != nil {
		return m.dir
	}
	return d
}

func (m *Manager) stagingDir() string { return filepath.Join(filepath.Dir(m.dir), "staging") }

// prune räumt je Art nach der Aufbewahrung auf (im eigenen Ordner direkt, sonst über den Agent).
func (m *Manager) prune(ctx context.Context, c Config) error {
	if c.Dir == "" {
		return Prune(m.dir, c.Keep)
	}
	return m.agent.Call(ctx, "backup.prune", map[string]any{"dir": m.where(c), "keep": c.Keep}, nil)
}

// Status prüft den Sicherungsort (legt den Ordner bei Bedarf an) und meldet freien Platz.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	c, err := LoadConfig(ctx, m.db)
	if err != nil {
		return Status{}, err
	}
	st := Status{Dir: m.where(c), Default: c.Dir == ""}
	var t target
	if err := m.agent.Call(ctx, "backup.prepare", map[string]string{"dir": st.Dir}, &t); err != nil {
		st.Problem = apierr.From(err).Message
		return st, nil
	}
	st.Free, st.Appdata = t.Free, t.Appdata
	return st, nil
}

// Create legt eine Konfigurations-Sicherung an und räumt alte auf.
func (m *Manager) Create(ctx context.Context, actor string) (Info, error) {
	cfg, _ := LoadConfig(ctx, m.db)
	info, err := m.createConfig(ctx, cfg)
	m.record(ctx, actor, "backup.create", info.Name, err)
	if err != nil {
		return Info{}, err
	}
	_ = m.prune(ctx, cfg)
	m.publish()
	return info, nil
}

func (m *Manager) createConfig(ctx context.Context, cfg Config) (Info, error) {
	if cfg.Dir == "" {
		return Create(ctx, m.db, m.dir, m.version)
	}
	// Die API darf nur in ihr Zustandsverzeichnis schreiben: Archiv dort bauen, der Agent legt es im Zielordner ab.
	info, err := Create(ctx, m.db, m.stagingDir(), m.version, m.where(cfg))
	if err != nil {
		return Info{}, err
	}
	src := filepath.Join(m.stagingDir(), info.Name)
	defer os.Remove(src)
	if err := m.agent.Call(ctx, "backup.store", map[string]string{"src": src, "dir": m.where(cfg)}, nil); err != nil {
		return Info{}, err
	}
	return info, nil
}

// ErrAppdataRunning meldet, dass schon eine appdata-Sicherung läuft.
var errAppdataRunning = apierr.New(apierr.Conflict, "Eine appdata-Sicherung läuft bereits.")

// AppdataResult ist das Ergebnis einer appdata-Sicherung.
type AppdataResult struct {
	Info    Info `json:"info"`
	Changed bool `json:"changed"` // Dateien haben sich währenddessen geändert (laufende Container)
}

// CreateAppdata sichert den Ordner appdata (Einstellungen der Docker-Apps) und räumt alte Sicherungen auf.
// Das kann dauern; der Aufrufer sollte es als Auftrag laufen lassen.
func (m *Manager) CreateAppdata(ctx context.Context, actor string) (AppdataResult, error) {
	if !m.appdataMu.TryLock() {
		return AppdataResult{}, errAppdataRunning
	}
	defer m.appdataMu.Unlock()
	cfg, _ := LoadConfig(ctx, m.db)
	var res struct {
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		Changed bool   `json:"changed"`
	}
	err := m.agent.Call(ctx, "backup.appdata", map[string]string{"dir": m.where(cfg)}, &res)
	m.record(ctx, actor, "backup.appdata", res.Name, err)
	if err != nil {
		return AppdataResult{}, err
	}
	_ = m.prune(ctx, cfg)
	m.publish()
	info := Info{Name: res.Name, Kind: KindAppdata, Size: res.Size, Manifest: Manifest{CreatedAt: m.now().UTC()}}
	return AppdataResult{Info: info, Changed: res.Changed}, nil
}

// Config liefert die Einstellungen der automatischen Sicherung.
func (m *Manager) Config(ctx context.Context) (Config, error) { return LoadConfig(ctx, m.db) }

// SetConfig speichert neue Einstellungen. Ein eigener Ordner wird angelegt und geprüft (Schreiben durch den Agent,
// Lesen durch die API); appdata braucht eingerichtetes Docker. Danach wird nach der neuen Aufbewahrung aufgeräumt
// und der Planer geweckt, damit die neue Uhrzeit gilt. Vorhandene Sicherungen im alten Ordner bleiben dort liegen.
func (m *Manager) SetConfig(ctx context.Context, actor string, c Config) error {
	err := m.checkConfig(ctx, &c)
	if err == nil {
		err = SaveConfig(ctx, m.db, c)
	}
	m.record(ctx, actor, "backup.config", c.String(), err)
	if err != nil {
		return err
	}
	_ = m.prune(ctx, c)
	select {
	case m.wake <- struct{}{}:
	default:
	}
	m.publish()
	return nil
}

func (m *Manager) checkConfig(ctx context.Context, c *Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Dir == "" && !c.Appdata {
		return nil
	}
	if c.Dir != "" {
		c.Dir, _ = dirCheck(c.Dir)
	}
	var t target
	if err := m.agent.Call(ctx, "backup.prepare", map[string]string{"dir": m.where(*c)}, &t); err != nil {
		return err
	}
	if c.Dir != "" {
		if _, err := os.ReadDir(t.Dir); err != nil {
			return apierr.New(apierr.Conflict, "BetterNAS kann den Ordner nicht lesen. Er und seine übergeordneten Ordner müssen für andere Benutzer lesbar sein (Rechte r-x).").With("detail", err.Error())
		}
	}
	if c.Appdata && t.Appdata == "" {
		return apierr.New(apierr.Conflict, "appdata gibt es erst, wenn Docker eingerichtet ist (Einstellungen → Docker).")
	}
	return nil
}

// List liefert die abgelegten Sicherungen.
func (m *Manager) List(ctx context.Context) ([]Info, error) {
	c, err := LoadConfig(ctx, m.db)
	if err != nil {
		return nil, err
	}
	return List(m.where(c))
}

// Open öffnet eine Sicherung zum Herunterladen.
func (m *Manager) Open(ctx context.Context, name string) (*os.File, error) {
	if KindOf(name) == "" {
		return nil, invalid("Ungültiger Backup-Name.")
	}
	c, err := LoadConfig(ctx, m.db)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(m.where(c), name))
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
	c, _ := LoadConfig(ctx, m.db)
	var err error
	if c.Dir == "" {
		err = Delete(m.dir, name)
	} else if KindOf(name) == "" {
		err = invalid("Ungültiger Backup-Name.")
	} else {
		err = m.agent.Call(ctx, "backup.delete", map[string]string{"dir": m.where(c), "name": name}, nil)
	}
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
	if KindOf(name) == KindAppdata {
		return Preview{}, invalid("appdata-Sicherungen lassen sich nicht hier wiederherstellen. Stoppe die Container und entpacke das Archiv mit „tar -xzpf“ in den Ordner appdata.")
	}
	f, err := m.Open(ctx, name)
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
