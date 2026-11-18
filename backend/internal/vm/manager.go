package vm

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/events"
)

// Agent ist der Teil des Agent-Clients, den der Manager braucht.
type Agent interface {
	Call(ctx context.Context, method string, params, result any) error
}

const changedEvent = "vm.changed"

// Manager verwaltet virtuelle Maschinen über den Agent.
type Manager struct {
	db     *sql.DB
	agent  Agent
	hub    *events.Hub
	isoDir string
}

// NewManager erzeugt den Manager.
func NewManager(d *sql.DB, a Agent, hub *events.Hub) *Manager {
	return &Manager{db: d, agent: a, hub: hub, isoDir: ISODir}
}

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

// Status meldet, ob libvirt verfügbar ist.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	var st Status
	err := m.agent.Call(ctx, "vm.status", nil, &st)
	return st, err
}

// List liefert alle VMs.
func (m *Manager) List(ctx context.Context) ([]VM, error) {
	list := []VM{}
	err := m.agent.Call(ctx, "vm.list", nil, &list)
	return list, err
}

// Networks liefert die aktiven libvirt-Netze.
func (m *Manager) Networks(ctx context.Context) ([]string, error) {
	list := []string{}
	err := m.agent.Call(ctx, "vm.networks", nil, &list)
	return list, err
}

// ISOs liefert die verfügbaren ISO-Dateien.
func (m *Manager) ISOs(ctx context.Context) ([]string, error) {
	list := []string{}
	err := m.agent.Call(ctx, "vm.isos", nil, &list)
	return list, err
}

// CreateInput ist die Eingabe zum Anlegen einer VM.
type CreateInput struct {
	Spec
	Start bool `json:"start"`
}

// Create legt eine VM an (und startet sie auf Wunsch).
func (m *Manager) Create(ctx context.Context, actor string, in CreateInput) error {
	if err := in.Spec.Validate(); err != nil {
		return err
	}
	err := m.agent.Call(ctx, "vm.create", in, nil)
	m.record(ctx, actor, "vm.create", in.Name, err)
	return err
}

// Action führt eine Aktion aus der Whitelist aus.
func (m *Manager) Action(ctx context.Context, actor, name, action string) error {
	if !DomainRe.MatchString(name) {
		return apierr.New(apierr.InvalidParams, "Ungültiger VM-Name")
	}
	if !Actions[action] {
		return apierr.New(apierr.InvalidParams, "Unbekannte VM-Aktion")
	}
	err := m.agent.Call(ctx, "vm.action", map[string]string{"name": name, "action": action}, nil)
	m.record(ctx, actor, "vm."+action, name, err)
	return err
}

// Delete entfernt eine ausgeschaltete VM, auf Wunsch mit ihren Festplatten.
func (m *Manager) Delete(ctx context.Context, actor, name string, deleteDisks bool) error {
	if !DomainRe.MatchString(name) {
		return apierr.New(apierr.InvalidParams, "Ungültiger VM-Name")
	}
	err := m.agent.Call(ctx, "vm.delete", map[string]any{"name": name, "deleteDisks": deleteDisks}, nil)
	m.record(ctx, actor, "vm.delete", name, err)
	return err
}

// SetISODir ändert das Upload-Verzeichnis (nur für Tests).
func (m *Manager) SetISODir(dir string) { m.isoDir = dir }

func checkName(name string) error {
	if !DomainRe.MatchString(name) {
		return apierr.New(apierr.InvalidParams, "Ungültiger VM-Name")
	}
	return nil
}

func checkSnap(name, snap string) error {
	if err := checkName(name); err != nil {
		return err
	}
	if !SnapshotRe.MatchString(snap) {
		return apierr.New(apierr.InvalidParams, "Ungültiger Snapshot-Name")
	}
	return nil
}

// Interfaces liefert die Host-Schnittstellen für ein LAN-Netz (macvtap).
func (m *Manager) Interfaces(ctx context.Context) ([]string, error) {
	list := []string{}
	err := m.agent.Call(ctx, "vm.interfaces", nil, &list)
	return list, err
}

// Snapshots liefert die Snapshots einer VM.
func (m *Manager) Snapshots(ctx context.Context, name string) ([]Snapshot, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	list := []Snapshot{}
	err := m.agent.Call(ctx, "vm.snapshots", map[string]string{"name": name}, &list)
	return list, err
}

// SnapshotAction legt einen Snapshot an (create), setzt darauf zurück (revert) oder löscht ihn (delete).
func (m *Manager) SnapshotAction(ctx context.Context, actor, op, name, snap string) error {
	if op != "create" && op != "revert" && op != "delete" {
		return apierr.New(apierr.InvalidParams, "Unbekannte Snapshot-Aktion")
	}
	if err := checkSnap(name, snap); err != nil {
		return err
	}
	err := m.agent.Call(ctx, "vm.snapshot."+op, map[string]string{"name": name, "snapshot": snap}, nil)
	m.record(ctx, actor, "vm.snapshot."+op, name+"@"+snap, err)
	return err
}

// Clone klont eine ausgeschaltete VM.
func (m *Manager) Clone(ctx context.Context, actor, name, newName string) error {
	if err := checkName(name); err != nil {
		return err
	}
	if !NameRe.MatchString(newName) {
		return apierr.New(apierr.InvalidParams, "Ungültiger Name der Kopie")
	}
	err := m.agent.Call(ctx, "vm.clone", map[string]string{"name": name, "newName": newName}, nil)
	m.record(ctx, actor, "vm.clone", name+" -> "+newName, err)
	return err
}

// ConsolePort liefert den lokalen VNC-Port einer laufenden VM und vermerkt die Sitzung im Audit-Log.
func (m *Manager) ConsolePort(ctx context.Context, actor, name string) (int, error) {
	if err := checkName(name); err != nil {
		return 0, err
	}
	var out struct {
		Port int `json:"port"`
	}
	err := m.agent.Call(ctx, "vm.display", map[string]string{"name": name}, &out)
	if err == nil && (out.Port < 5900 || out.Port > 5999) {
		err = apierr.New(apierr.Internal, "Ungültiger VNC-Port")
	}
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "vm.console", Target: name, Result: resultOf(err)})
	return out.Port, err
}

func resultOf(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}

// MaxUploadBytes ist die größte erlaubte ISO-/Image-Datei.
const MaxUploadBytes int64 = 50 << 30

// UploadISO speichert eine ISO-/qcow2-Datei unter ISODir. Bestehende Dateien werden nie überschrieben;
// die Datei erscheint erst nach vollständigem Empfang (Temp-Datei + Hardlink).
func (m *Manager) UploadISO(ctx context.Context, actor, file string, body io.Reader) error {
	if !ISORe.MatchString(file) && !ImageRe.MatchString(file) {
		return apierr.New(apierr.InvalidParams, "Ungültiger Dateiname (erlaubt: .iso oder .qcow2, ohne Pfad)")
	}
	err := m.upload(file, body)
	_ = audit.Record(ctx, m.db, audit.Entry{Actor: actor, Action: "vm.iso.upload", Target: file, Result: resultOf(err)})
	if err == nil {
		m.hub.Publish(events.Message{Type: changedEvent})
	}
	return err
}

func (m *Manager) upload(file string, body io.Reader) error {
	final := filepath.Join(m.isoDir, file)
	if _, err := os.Lstat(final); err == nil {
		return apierr.New(apierr.Conflict, "Eine Datei mit diesem Namen existiert bereits")
	}
	tmp, err := os.CreateTemp(m.isoDir, ".upload-*")
	if err != nil {
		return apierr.New(apierr.Internal, "Das Zielverzeichnis ist nicht beschreibbar").With("detail", err.Error())
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, io.LimitReader(body, MaxUploadBytes+1))
	if err == nil {
		var st os.FileInfo
		if st, err = tmp.Stat(); err == nil && st.Size() > MaxUploadBytes {
			_ = tmp.Close()
			return apierr.New(apierr.InvalidParams, "Die Datei ist größer als 50 GiB")
		}
	}
	if err != nil {
		_ = tmp.Close()
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return apierr.New(apierr.InvalidParams, "Die Datei ist zu groß")
		}
		return apierr.New(apierr.Internal, "Der Upload wurde abgebrochen")
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return apierr.New(apierr.Internal, "Die Datei konnte nicht gespeichert werden").With("detail", err.Error())
	}
	if err := os.Link(tmp.Name(), final); err != nil {
		if os.IsExist(err) {
			return apierr.New(apierr.Conflict, "Eine Datei mit diesem Namen existiert bereits")
		}
		return apierr.New(apierr.Internal, "Die Datei konnte nicht gespeichert werden").With("detail", err.Error())
	}
	return nil
}

// DeleteISO löscht eine ISO-/Image-Datei (der Agent prüft, ob eine VM sie verwendet).
func (m *Manager) DeleteISO(ctx context.Context, actor, file string) error {
	if !ISORe.MatchString(file) && !ImageRe.MatchString(file) {
		return apierr.New(apierr.InvalidParams, "Ungültiger Dateiname")
	}
	err := m.agent.Call(ctx, "vm.iso.delete", map[string]string{"file": file}, nil)
	m.record(ctx, actor, "vm.iso.delete", file, err)
	return err
}

// Config liefert die Konfiguration einer VM.
func (m *Manager) Config(ctx context.Context, name string) (Config, error) {
	if err := checkName(name); err != nil {
		return Config{}, err
	}
	var c Config
	err := m.agent.Call(ctx, "vm.config", map[string]string{"name": name}, &c)
	return c, err
}

// USBDevices liefert die am Host angeschlossenen USB-Geräte.
func (m *Manager) USBDevices(ctx context.Context) ([]USBDevice, error) {
	list := []USBDevice{}
	err := m.agent.Call(ctx, "vm.usb", nil, &list)
	return list, err
}

// Disks liefert die Festplatten einer VM mit Größe.
func (m *Manager) Disks(ctx context.Context, name string) ([]Disk, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	list := []Disk{}
	err := m.agent.Call(ctx, "vm.disks", map[string]string{"name": name}, &list)
	return list, err
}

// ResizeDisk vergrößert eine Festplatte einer ausgeschalteten VM.
func (m *Manager) ResizeDisk(ctx context.Context, actor, name string, r DiskResize) error {
	if err := checkName(name); err != nil {
		return err
	}
	if err := r.Validate(); err != nil {
		return err
	}
	params := struct {
		Name string `json:"name"`
		DiskResize
	}{name, r}
	err := m.agent.Call(ctx, "vm.disk.resize", params, nil)
	m.record(ctx, actor, "vm.disk.resize", name+" "+r.Path+" -> "+strconv.Itoa(r.SizeGiB)+"G", err)
	return err
}

// Configure ändert die Konfiguration einer ausgeschalteten VM.
func (m *Manager) Configure(ctx context.Context, actor, name string, u ConfigUpdate) error {
	if err := checkName(name); err != nil {
		return err
	}
	if err := u.Validate(); err != nil {
		return err
	}
	params := struct {
		Name string `json:"name"`
		ConfigUpdate
	}{name, u}
	err := m.agent.Call(ctx, "vm.configure", params, nil)
	m.record(ctx, actor, "vm.configure", name, err)
	return err
}
