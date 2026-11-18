package docker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/rpc"
)

// Agent ist der Teil des Agent-Clients, den der Manager braucht.
type Agent interface {
	Call(ctx context.Context, method string, params, result any) error
}

// StackInput sind die Eingaben zum Anlegen und Ändern eines Stacks.
type StackInput struct {
	Name    string `json:"name"`
	Compose string `json:"compose"`
	Env     string `json:"env"`
}

// Manager verwaltet Stacks und Container über den Agent.
type Manager struct {
	db        *sql.DB
	agent     Agent
	jobs      *jobs.Manager
	hub       *events.Hub
	pollEvery time.Duration

	// Auto-Update: Planer und Gesundheitsprüfung (in Tests verkürzt).
	now           func() time.Time
	after         func(time.Duration) <-chan time.Time
	wake          chan struct{}
	healthSettle  time.Duration // so lange müssen alle Container ununterbrochen gesund sein
	healthTimeout time.Duration // so lange wird auf einen gesunden Stack gewartet
}

// NewManager erzeugt den Manager.
func NewManager(d *sql.DB, a Agent, j *jobs.Manager, hub *events.Hub) *Manager {
	return &Manager{db: d, agent: a, jobs: j, hub: hub, pollEvery: 2 * time.Second,
		now: time.Now, after: time.After, wake: make(chan struct{}, 1), healthSettle: time.Minute, healthTimeout: 10 * time.Minute}
}

const (
	maxRunTime     = 2 * time.Hour
	maxPollErrors  = 30 // Agent-Neustart während eines Laufs überbrücken
	changedEvent   = "docker.changed"
	stackNameError = "Ungültiger Stackname (erlaubt: Kleinbuchstaben, Ziffern, - und _, bis 40 Zeichen)"
)

func (m *Manager) record(ctx context.Context, actor, action, target string, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: action, Target: target, Result: result})
	if err == nil {
		m.hub.Publish(events.Message{Type: changedEvent})
	}
}

func checkName(name string) error {
	if !StackNameRe.MatchString(name) {
		return apierr.New(apierr.InvalidParams, stackNameError)
	}
	return nil
}

// Status meldet, ob Docker installiert und aktiv ist.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	var st Status
	err := m.agent.Call(ctx, "docker.status", nil, &st)
	return st, err
}

// ListStacks liefert alle Stacks mit Laufzustand.
func (m *Manager) ListStacks(ctx context.Context) ([]StackInfo, error) {
	list := []StackInfo{}
	if err := m.agent.Call(ctx, "stack.list", nil, &list); err != nil {
		return list, err
	}
	if icons, err := m.iconVersions(ctx); err == nil {
		for i := range list {
			list[i].Icon = icons[list[i].Name]
		}
	}
	return list, nil
}

// GetStack liefert compose.yaml und .env.
func (m *Manager) GetStack(ctx context.Context, name string) (StackFiles, error) {
	if err := checkName(name); err != nil {
		return StackFiles{}, err
	}
	var sf StackFiles
	err := m.agent.Call(ctx, "stack.get", map[string]string{"name": name}, &sf)
	return sf, err
}

func (m *Manager) save(ctx context.Context, actor string, in StackInput, create bool) error {
	if err := checkName(in.Name); err != nil {
		return err
	}
	req := map[string]any{"name": in.Name, "compose": in.Compose, "env": in.Env, "create": create}
	err := m.agent.Call(ctx, "stack.save", req, nil)
	m.record(ctx, actor, "stack.save", in.Name, err)
	return err
}

// CreateStack legt einen neuen Stack an (Konflikt, wenn der Name existiert).
func (m *Manager) CreateStack(ctx context.Context, actor string, in StackInput) error {
	return m.save(ctx, actor, in, true)
}

// UpdateStack überschreibt compose.yaml und .env eines vorhandenen Stacks.
func (m *Manager) UpdateStack(ctx context.Context, actor string, in StackInput) error {
	if _, err := m.GetStack(ctx, in.Name); err != nil {
		return err
	}
	return m.save(ctx, actor, in, false)
}

// ValidateStack prüft den gespeicherten Stack mit `docker compose config`.
func (m *Manager) ValidateStack(ctx context.Context, name string) (StackCheck, error) {
	if err := checkName(name); err != nil {
		return StackCheck{}, err
	}
	var c StackCheck
	err := m.agent.Call(ctx, "stack.validate", map[string]string{"name": name}, &c)
	return c, err
}

// DeleteStack fährt den Stack herunter (Volumes bleiben) und löscht seine Dateien.
func (m *Manager) DeleteStack(ctx context.Context, actor, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	err := m.agent.Call(ctx, "stack.delete", map[string]string{"name": name}, nil)
	if err == nil {
		_, _ = m.db.ExecContext(ctx, `DELETE FROM stack_icons WHERE stack = ?`, name)
	}
	m.record(ctx, actor, "stack.delete", name, err)
	return err
}

// startStack startet eine Stack-Aktion beim Agent und liefert den Namen der Lauf-Unit.
// Der Agent prüft Existenz, Gültigkeit und laufende Vorgänge synchron; Fehler gehen direkt an den Aufrufer.
func (m *Manager) startStack(ctx context.Context, actor, name, action string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	if !StackActions[action] {
		return "", apierr.New(apierr.InvalidParams, "Unbekannte Stack-Aktion")
	}
	var res struct {
		Unit string `json:"unit"`
	}
	err := m.agent.Call(ctx, "stack.run", map[string]string{"name": name, "action": action}, &res)
	m.record(ctx, actor, "stack."+action, name, err)
	if err != nil {
		return "", err
	}
	return res.Unit, nil
}

// waitStack begleitet einen gestarteten Lauf, bis die Unit des Agents fertig ist, und führt danach die
// Nacharbeiten der Aktion aus (Update-Merker, Rollback-Stand).
func (m *Manager) waitStack(ctx context.Context, log jobs.Logger, name, action, unit string) error {
	defer m.hub.Publish(events.Message{Type: changedEvent})
	log.Printf("Stack %s: %s …", name, action)
	deadline, shown, errs := time.Now().Add(maxRunTime), "", 0
	for {
		time.Sleep(m.pollEvery)
		var st RunState
		if err := m.agent.Call(ctx, "stack.runstatus", map[string]string{"unit": unit}, &st); err != nil {
			if errs++; errs >= maxPollErrors {
				return err
			}
			continue
		}
		errs = 0
		if st.Log != shown {
			if rest, ok := strings.CutPrefix(st.Log, shown); ok {
				log.Printf("%s", strings.TrimLeft(rest, "\n"))
			} else {
				log.Printf("%s", st.Log)
			}
			shown = st.Log
		}
		switch st.State {
		case "succeeded":
			switch action {
			case "update":
				m.forgetUpdate(ctx, name)
				// Hat das Update Images verändert, wird der vorherige Stand zum Rollback-Stand.
				_ = m.agent.Call(ctx, "stack.rollback.commit", map[string]string{"name": name}, nil)
			case "rollback":
				_ = m.agent.Call(ctx, "stack.rollback.clear", map[string]string{"name": name}, nil)
			}
			return nil
		case "failed":
			return apierr.New(apierr.Conflict, "Der Vorgang ist fehlgeschlagen (Protokoll siehe oben).")
		}
		if time.Now().After(deadline) {
			return apierr.New(apierr.Internal, "Der Vorgang dauert zu lange.")
		}
	}
}

// RunStack startet eine Stack-Aktion und begleitet sie als Job.
func (m *Manager) RunStack(ctx context.Context, actor, name, action string) (string, error) {
	unit, err := m.startStack(ctx, actor, name, action)
	if err != nil {
		return "", err
	}
	return m.jobs.Start("stack."+action, name, actor, func(ctx context.Context, log jobs.Logger) error {
		return m.waitStack(ctx, log, name, action, unit)
	}), nil
}

// Folders liefert die Unterordner von p für die Ordnerauswahl (leer: die Wurzeln /srv/pools und /srv/appdata).
func (m *Manager) Folders(ctx context.Context, p string) (FolderList, error) {
	c, err := CleanFolder(p)
	if err != nil {
		return FolderList{}, err
	}
	var out FolderList
	err = m.agent.Call(ctx, "folder.list", map[string]string{"path": c}, &out)
	return out, err
}

// HostDevices liefert die an Container durchreichbaren Geräte (GPU, USB-Seriell-Adapter).
func (m *Manager) HostDevices(ctx context.Context) ([]HostDevice, error) {
	list := []HostDevice{}
	err := m.agent.Call(ctx, "device.list", nil, &list)
	return list, err
}

// ListContainers liefert alle Container.
func (m *Manager) ListContainers(ctx context.Context) ([]Container, error) {
	list := []Container{}
	err := m.agent.Call(ctx, "container.list", nil, &list)
	return list, err
}

// ContainerAction führt start, stop, restart oder remove aus.
func (m *Manager) ContainerAction(ctx context.Context, actor, id, action string) error {
	if !ContainerIDRe.MatchString(id) {
		return apierr.New(apierr.InvalidParams, "Ungültige Container-ID")
	}
	if !ContainerActions[action] {
		return apierr.New(apierr.InvalidParams, "Unbekannte Container-Aktion")
	}
	err := m.agent.Call(ctx, "container.action", map[string]string{"id": id, "action": action}, nil)
	m.record(ctx, actor, "container."+action, id, err)
	return err
}

// SessionOpener ist der Teil des Agent-Clients für bidirektionale Sitzungen (*rpc.Client).
type SessionOpener interface {
	OpenSession(ctx context.Context, method string, params any) (*rpc.Session, error)
}

// OpenSession öffnet eine Sitzung (Logs, Shell) beim Agent.
func (m *Manager) OpenSession(ctx context.Context, method string, params any) (*rpc.Session, error) {
	so, ok := m.agent.(SessionOpener)
	if !ok {
		return nil, apierr.New(apierr.Internal, "Sitzungen sind nicht verfügbar")
	}
	return so.OpenSession(ctx, method, params)
}

// Stats liefert CPU und Speicher der laufenden Container.
func (m *Manager) Stats(ctx context.Context) ([]ContainerStats, error) {
	list := []ContainerStats{}
	err := m.agent.Call(ctx, "container.stats", nil, &list)
	return list, err
}

// AuditShell hält fest, dass ein Administrator eine Shell in einem Container geöffnet hat.
func (m *Manager) AuditShell(ctx context.Context, actor, id string) {
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "container.shell", Target: id, Result: "ok"})
}

const keyUpdates = "docker.updates"

func (m *Manager) loadReport(ctx context.Context) (UpdateReport, error) {
	rep := UpdateReport{Stacks: map[string]UpdateInfo{}}
	var raw string
	err := m.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keyUpdates).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return rep, nil
	}
	if err != nil {
		return rep, err
	}
	_ = json.Unmarshal([]byte(raw), &rep)
	if rep.Stacks == nil {
		rep.Stacks = map[string]UpdateInfo{}
	}
	return rep, nil
}

func (m *Manager) saveReport(ctx context.Context, rep UpdateReport) error {
	b, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, keyUpdates, string(b))
	return err
}

// Updates liefert das Ergebnis der letzten Update-Prüfung.
func (m *Manager) Updates(ctx context.Context) (UpdateReport, error) { return m.loadReport(ctx) }

// forgetUpdate entfernt einen Stack aus dem Prüfergebnis (nach „Aktualisieren“ ist er aktuell).
func (m *Manager) forgetUpdate(ctx context.Context, name string) {
	rep, err := m.loadReport(ctx)
	if err != nil {
		return
	}
	if _, ok := rep.Stacks[name]; ok {
		delete(rep.Stacks, name)
		_ = m.saveReport(ctx, rep)
	}
}

// runCheck vergleicht die Image-Digests aller Stacks mit den Registries und speichert das Ergebnis.
func (m *Manager) runCheck(ctx context.Context, log jobs.Logger) error {
	stacks, err := m.ListStacks(ctx)
	if err != nil {
		return err
	}
	rep := UpdateReport{CheckedAt: time.Now().UTC().Format(time.RFC3339), Stacks: map[string]UpdateInfo{}}
	for i, s := range stacks {
		log.Printf("Prüfe Stack %s …", s.Name)
		info := UpdateInfo{Outdated: []string{}, Errors: []string{}}
		var images []string
		if err := m.agent.Call(ctx, "stack.images", map[string]string{"name": s.Name}, &images); err != nil {
			info.Errors = append(info.Errors, apierr.From(err).Message)
		} else if len(images) > 0 {
			var sts []ImageStatus
			if err := m.agent.Call(ctx, "image.digests", map[string]any{"images": images}, &sts); err != nil {
				info.Errors = append(info.Errors, apierr.From(err).Message)
			}
			for _, st := range sts {
				switch {
				case st.Outdated:
					info.Outdated = append(info.Outdated, st.Image)
				case st.Error != "":
					info.Errors = append(info.Errors, st.Image+": "+st.Error)
				}
			}
		}
		rep.Stacks[s.Name] = info
		if len(info.Outdated) > 0 {
			log.Printf("  Updates verfügbar für: %s", strings.Join(info.Outdated, ", "))
		}
		log.SetProgress((i + 1) * 100 / len(stacks))
	}
	if err := m.saveReport(ctx, rep); err != nil {
		return err
	}
	log.Printf("Prüfung abgeschlossen.")
	return nil
}

// CheckUpdates vergleicht als Job die Image-Digests aller Stacks mit den Registries.
func (m *Manager) CheckUpdates(ctx context.Context, actor string) (string, error) {
	return m.jobs.Start("docker.updatecheck", "docker", actor, func(ctx context.Context, log jobs.Logger) (err error) {
		defer func() {
			m.record(ctx, actor, "docker.updatecheck", "docker", err)
		}()
		return m.runCheck(ctx, log)
	}), nil
}

func validKind(kind string) bool {
	return kind == KindImage || kind == KindNetwork || kind == KindVolume
}

// Images liefert alle Images.
func (m *Manager) Images(ctx context.Context) ([]ImageInfo, error) {
	list := []ImageInfo{}
	err := m.agent.Call(ctx, "image.list", nil, &list)
	return list, err
}

// Networks liefert alle Netzwerke.
func (m *Manager) Networks(ctx context.Context) ([]NetworkInfo, error) {
	list := []NetworkInfo{}
	err := m.agent.Call(ctx, "network.list", nil, &list)
	return list, err
}

// Volumes liefert alle Volumes.
func (m *Manager) Volumes(ctx context.Context) ([]VolumeInfo, error) {
	list := []VolumeInfo{}
	err := m.agent.Call(ctx, "volume.list", nil, &list)
	return list, err
}

// RemoveResource entfernt ein Image, Netzwerk oder Volume.
func (m *Manager) RemoveResource(ctx context.Context, actor, kind, ref string) error {
	if !validKind(kind) || ref == "" || len(ref) > 300 {
		return apierr.New(apierr.InvalidParams, "Ungültige Anfrage")
	}
	err := m.agent.Call(ctx, "resource.remove", map[string]string{"kind": kind, "ref": ref}, nil)
	m.record(ctx, actor, "docker."+kind+".remove", ref, err)
	return err
}

// PruneResources räumt ungenutzte Ressourcen einer Art auf.
func (m *Manager) PruneResources(ctx context.Context, actor, kind string, all bool) (PruneResult, error) {
	if !validKind(kind) {
		return PruneResult{}, apierr.New(apierr.InvalidParams, "Ungültige Anfrage")
	}
	var res PruneResult
	err := m.agent.Call(ctx, "resource.prune", map[string]any{"kind": kind, "all": all}, &res)
	m.record(ctx, actor, "docker."+kind+".prune", kind, err)
	return res, err
}
