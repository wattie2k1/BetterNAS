package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
)

const (
	smbGlobalFile    = "/etc/samba/betternas.d/global.conf"
	smbGlobalInclude = "include = /etc/samba/betternas.d/global.conf"
	smbSharesInclude = "include = /etc/samba/betternas.d/shares.conf"
	dockerDaemonFile = "/etc/docker/daemon.json"
	managedHeader    = "# Verwaltet von BetterNAS – nicht manuell bearbeiten"
)

// ServiceDef beschreibt einen steuerbaren Dienst.
type ServiceDef struct {
	Unit  string `json:"unit"`
	Label string `json:"label"`
}

// Services sind die Dienste, die die Oberfläche steuern darf (feste Liste, keine freie Eingabe).
var Services = []ServiceDef{
	{"smbd", "Windows-Freigaben (SMB)"},
	{"nmbd", "Netzwerkumgebung (NetBIOS)"},
	{"nfs-server", "NFS-Freigaben"},
	{"docker", "Docker"},
	{"libvirtd", "Virtuelle Maschinen (libvirt)"},
	{"avahi-daemon", "Namensauflösung im LAN (mDNS)"},
	{"systemd-timesyncd", "Zeitsynchronisierung"},
	{"ssh", "SSH-Zugang"},
}

// ServiceState ist der Zustand eines Dienstes.
type ServiceState struct {
	ServiceDef
	Active    bool `json:"active"`
	Enabled   bool `json:"enabled"`
	Installed bool `json:"installed"`
}

// ListServices liefert den Zustand aller steuerbaren Dienste.
func ListServices(ctx context.Context, d Deps) ([]ServiceState, error) {
	out := make([]ServiceState, 0, len(Services))
	for _, s := range Services {
		st := ServiceState{ServiceDef: s}
		if res, err := d.Runner.Run(ctx, nil, "systemctl", "is-active", s.Unit); err == nil {
			st.Active = res.ExitCode == 0 && strings.TrimSpace(string(res.Stdout)) == "active"
		}
		if res, err := d.Runner.Run(ctx, nil, "systemctl", "is-enabled", s.Unit); err == nil {
			state := strings.TrimSpace(string(res.Stdout))
			st.Enabled = res.ExitCode == 0 && (state == "enabled" || state == "static" || state == "alias")
			st.Installed = state != "not-found" && !strings.Contains(string(res.Stderr), "not found") && !strings.Contains(string(res.Stderr), "nicht gefunden")
		} else {
			st.Installed = true
		}
		out = append(out, st)
	}
	return out, nil
}

// ServiceAction startet, stoppt oder startet einen Dienst neu.
func ServiceAction(ctx context.Context, d Deps, unit, action string) error {
	known := false
	for _, s := range Services {
		known = known || s.Unit == unit
	}
	if !known {
		return apierr.New(apierr.InvalidParams, "Unbekannter Dienst")
	}
	if action != "start" && action != "stop" && action != "restart" {
		return apierr.New(apierr.InvalidParams, "Unbekannte Aktion")
	}
	res, err := d.Runner.Run(ctx, nil, "systemctl", action, unit)
	if err != nil || res.ExitCode != 0 {
		return apierr.New(apierr.Internal, "Die Aktion ist fehlgeschlagen").With("detail", strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

// ---- Samba (global) ----

var workgroupRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,14}$`)

// SMBGlobal sind die einstellbaren globalen Samba-Werte.
type SMBGlobal struct {
	Workgroup   string `json:"workgroup"`
	ServerName  string `json:"serverName"` // "server string"
	MinProtocol string `json:"minProtocol"`
}

var smbMinProtocols = map[string]bool{"SMB2_10": true, "SMB3_00": true, "SMB3_11": true}

func smbDefaults() SMBGlobal {
	return SMBGlobal{Workgroup: "WORKGROUP", ServerName: "BetterNAS", MinProtocol: "SMB2_10"}
}

// GetSMBGlobal liest die globalen Werte (Standardwerte, solange nichts gespeichert ist).
func GetSMBGlobal(d Deps) SMBGlobal {
	g := smbDefaults()
	b, err := d.FS.ReadFile(smbGlobalFile)
	if err != nil {
		return g
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "workgroup":
			g.Workgroup = strings.TrimSpace(v)
		case "server string":
			g.ServerName = strings.TrimSpace(v)
		case "server min protocol":
			g.MinProtocol = strings.TrimSpace(v)
		}
	}
	return g
}

func validSMB(g SMBGlobal) error {
	if !workgroupRe.MatchString(g.Workgroup) {
		return apierr.New(apierr.InvalidParams, "Ungültige Arbeitsgruppe (Buchstaben, Ziffern, - und _, höchstens 15 Zeichen)")
	}
	if g.ServerName == "" || len(g.ServerName) > 64 || strings.ContainsAny(g.ServerName, "\n\r\t;#=[]%$\\") {
		return apierr.New(apierr.InvalidParams, "Ungültige Serverbeschreibung (höchstens 64 Zeichen, keine Sonderzeichen wie ; # = [ ] % $)")
	}
	if !smbMinProtocols[g.MinProtocol] {
		return apierr.New(apierr.InvalidParams, "Ungültiges Mindestprotokoll")
	}
	return nil
}

// SetSMBGlobal schreibt die globalen Werte, prüft sie mit testparm und lädt Samba neu. Bei Fehlern bleibt alles wie vorher.
func SetSMBGlobal(ctx context.Context, d Deps, g SMBGlobal) error {
	mutate.Lock()
	defer mutate.Unlock()
	g.Workgroup = strings.ToUpper(strings.TrimSpace(g.Workgroup))
	g.ServerName = strings.TrimSpace(g.ServerName)
	if err := validSMB(g); err != nil {
		return err
	}
	conf, err := d.FS.ReadFile(smbConf)
	if err != nil || !isManaged(conf) {
		return apierr.New(apierr.Conflict, "Die Samba-Konfiguration wird nicht von BetterNAS verwaltet und wird nicht verändert.")
	}
	newConf := conf
	if !strings.Contains(string(conf), smbGlobalInclude) {
		// Die globalen Werte müssen vor den Freigaben stehen und nach den Standardwerten (spätere Zeilen gewinnen).
		lines := strings.Split(string(conf), "\n")
		done := false
		for i, l := range lines {
			if strings.TrimSpace(l) == smbSharesInclude {
				lines = append(lines[:i], append([]string{"   " + smbGlobalInclude}, lines[i:]...)...)
				done = true
				break
			}
		}
		if !done {
			return apierr.New(apierr.Conflict, "Die Samba-Konfiguration hat nicht das erwartete Format.")
		}
		newConf = []byte(strings.Join(lines, "\n"))
	}
	oldGlobal, haveGlobal := d.FS.ReadFile(smbGlobalFile)
	rollback := func() {
		_ = d.FS.WriteFile(smbConf, conf, 0o644)
		if haveGlobal == nil {
			_ = d.FS.WriteFile(smbGlobalFile, oldGlobal, 0o644)
		} else {
			_ = d.FS.Remove(smbGlobalFile)
		}
	}
	if err := d.FS.MkdirAll("/etc/samba/betternas.d", 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf("%s\n[global]\n   workgroup = %s\n   server string = %s\n   server min protocol = %s\n", managedHeader, g.Workgroup, g.ServerName, g.MinProtocol)
	if err := d.FS.WriteFile(smbGlobalFile, []byte(body), 0o644); err != nil {
		return err
	}
	if err := d.FS.WriteFile(smbConf, newConf, 0o644); err != nil {
		rollback()
		return err
	}
	res, err := d.Runner.Run(ctx, nil, "testparm", "-s")
	if err != nil || res.ExitCode != 0 {
		rollback()
		return apierr.New(apierr.InvalidParams, "Samba lehnt die Konfiguration ab; es wurde nichts geändert.").With("testparm", strings.TrimSpace(string(res.Stderr)))
	}
	if active, err := d.Runner.Run(ctx, nil, "systemctl", "is-active", "smbd"); err == nil && active.ExitCode == 0 {
		if _, err := run(ctx, d.Runner, nil, "smbcontrol", "smbd", "reload-config"); err != nil {
			return err
		}
	}
	return nil
}

// ---- Docker ----

var (
	logSizeRe = regexp.MustCompile(`^[1-9][0-9]{0,5}[kmg]$`)
)

// DockerSettings sind die einstellbaren Werte der daemon.json.
type DockerSettings struct {
	DataRoot        string   `json:"dataRoot"` // nur Anzeige
	LogMaxSize      string   `json:"logMaxSize"`
	LogMaxFile      int      `json:"logMaxFile"`
	RegistryMirrors []string `json:"registryMirrors"`
	LiveRestore     bool     `json:"liveRestore"`
}

func readDaemon(d Deps) (map[string]any, error) {
	m := map[string]any{}
	b, err := d.FS.ReadFile(dockerDaemonFile)
	if err != nil {
		return m, nil
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, apierr.New(apierr.Conflict, "Die Docker-Konfiguration (daemon.json) ist keine gültige JSON-Datei und wird nicht verändert.")
	}
	return m, nil
}

// GetDockerSettings liest die einstellbaren Werte aus der daemon.json.
func GetDockerSettings(d Deps) (DockerSettings, error) {
	s := DockerSettings{DataRoot: "/var/lib/docker", LogMaxSize: "10m", LogMaxFile: 3, RegistryMirrors: []string{}}
	m, err := readDaemon(d)
	if err != nil {
		return s, err
	}
	if v, ok := m["data-root"].(string); ok {
		s.DataRoot = v
	}
	if lo, ok := m["log-opts"].(map[string]any); ok {
		if v, ok := lo["max-size"].(string); ok {
			s.LogMaxSize = v
		}
		if v, ok := lo["max-file"].(string); ok {
			fmt.Sscanf(v, "%d", &s.LogMaxFile)
		}
	}
	if rm, ok := m["registry-mirrors"].([]any); ok {
		for _, v := range rm {
			if str, ok := v.(string); ok {
				s.RegistryMirrors = append(s.RegistryMirrors, str)
			}
		}
	}
	if v, ok := m["live-restore"].(bool); ok {
		s.LiveRestore = v
	}
	return s, nil
}

// SetDockerSettings schreibt die Werte in die daemon.json (andere Schlüssel bleiben erhalten) und prüft sie.
// Der Docker-Dienst wird nicht neu gestartet; das entscheidet der Aufrufer.
func SetDockerSettings(ctx context.Context, d Deps, s DockerSettings) error {
	mutate.Lock()
	defer mutate.Unlock()
	if !logSizeRe.MatchString(s.LogMaxSize) {
		return apierr.New(apierr.InvalidParams, "Ungültige Log-Größe (z. B. 10m)")
	}
	if s.LogMaxFile < 1 || s.LogMaxFile > 100 {
		return apierr.New(apierr.InvalidParams, "Die Anzahl der Log-Dateien muss zwischen 1 und 100 liegen")
	}
	if len(s.RegistryMirrors) > 5 {
		return apierr.New(apierr.InvalidParams, "Höchstens 5 Registry-Mirrors")
	}
	mirrors := []string{}
	for _, m := range s.RegistryMirrors {
		m = strings.TrimSpace(m)
		u, err := url.Parse(m)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || strings.ContainsAny(m, " \n\r\t") {
			return apierr.New(apierr.InvalidParams, "Ungültiger Registry-Mirror: "+m)
		}
		mirrors = append(mirrors, m)
	}
	m, err := readDaemon(d)
	if err != nil {
		return err
	}
	old, haveOld := d.FS.ReadFile(dockerDaemonFile)
	lo, _ := m["log-opts"].(map[string]any)
	if lo == nil {
		lo = map[string]any{}
	}
	lo["max-size"], lo["max-file"] = s.LogMaxSize, fmt.Sprint(s.LogMaxFile)
	m["log-opts"] = lo
	if _, ok := m["log-driver"]; !ok {
		m["log-driver"] = "json-file"
	}
	if len(mirrors) > 0 {
		m["registry-mirrors"] = mirrors
	} else {
		delete(m, "registry-mirrors")
	}
	if s.LiveRestore {
		m["live-restore"] = true
	} else {
		delete(m, "live-restore")
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := d.FS.MkdirAll("/etc/docker", 0o755); err != nil {
		return err
	}
	if err := d.FS.WriteFile(dockerDaemonFile, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if res, err := d.Runner.Run(ctx, nil, "dockerd", "--validate", "--config-file="+dockerDaemonFile); err != nil || res.ExitCode != 0 {
		if haveOld == nil {
			_ = d.FS.WriteFile(dockerDaemonFile, old, 0o644)
		} else {
			_ = d.FS.Remove(dockerDaemonFile)
		}
		return apierr.New(apierr.InvalidParams, "Docker lehnt die Konfiguration ab; es wurde nichts geändert.").With("detail", strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

func registerServices(s *rpc.Server, d Deps) {
	s.Handle("service.list", func(ctx context.Context, _ json.RawMessage) (any, error) { return ListServices(ctx, d) })
	s.Handle("service.action", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct{ Unit, Action string }
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return nil, ServiceAction(ctx, d, req.Unit, req.Action)
	})
	s.Handle("smb.global.get", func(context.Context, json.RawMessage) (any, error) { return GetSMBGlobal(d), nil })
	s.Handle("smb.global.set", func(ctx context.Context, p json.RawMessage) (any, error) {
		var g SMBGlobal
		if err := decode(p, &g); err != nil {
			return nil, err
		}
		return nil, SetSMBGlobal(ctx, d, g)
	})
	s.Handle("docker.settings.get", func(context.Context, json.RawMessage) (any, error) { return GetDockerSettings(d) })
	s.Handle("docker.settings.set", func(ctx context.Context, p json.RawMessage) (any, error) {
		var v DockerSettings
		if err := decode(p, &v); err != nil {
			return nil, err
		}
		return nil, SetDockerSettings(ctx, d, v)
	})
}
