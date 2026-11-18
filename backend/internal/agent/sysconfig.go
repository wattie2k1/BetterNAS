package agent

import (
	"context"
	"encoding/json"
	"net/netip"
	"regexp"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
)

const (
	timesyncDropin = "/etc/systemd/timesyncd.conf.d/10-bnas.conf"
	hostsFile      = "/etc/hosts"
)

var (
	hostnameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	ntpHostRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
)

// SysSettings sind Hostname und Zeiteinstellungen.
type SysSettings struct {
	Hostname   string   `json:"hostname"`
	Timezone   string   `json:"timezone"`
	NTP        bool     `json:"ntp"`
	NTPSynced  bool     `json:"ntpSynced"`
	NTPServers []string `json:"ntpServers"`
	Time       string   `json:"time"`
}

// SysSettingsChange sind die zu ändernden Werte; nil bedeutet unverändert.
type SysSettingsChange struct {
	Hostname   *string   `json:"hostname"`
	Timezone   *string   `json:"timezone"`
	NTP        *bool     `json:"ntp"`
	NTPServers *[]string `json:"ntpServers"`
}

func parseShow(out []byte) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			m[k] = strings.TrimSpace(v)
		}
	}
	return m
}

// GetSysSettings liest Hostname, Zeitzone und NTP-Status.
func GetSysSettings(ctx context.Context, d Deps) (SysSettings, error) {
	s := SysSettings{Hostname: d.readTrim("/proc/sys/kernel/hostname"), NTPServers: []string{}}
	res, err := d.Runner.Run(ctx, nil, "timedatectl", "show")
	if err != nil || res.ExitCode != 0 {
		return s, apierr.New(apierr.Internal, "Die Zeiteinstellungen konnten nicht gelesen werden")
	}
	m := parseShow(res.Stdout)
	s.Timezone, s.NTP, s.NTPSynced, s.Time = m["Timezone"], m["NTP"] == "yes", m["NTPSynchronized"] == "yes", m["TimeUSec"]
	if b, err := d.FS.ReadFile(timesyncDropin); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "NTP="); ok {
				s.NTPServers = strings.Fields(v)
			}
		}
	}
	return s, nil
}

// Timezones liefert alle gültigen Zeitzonen.
func Timezones(ctx context.Context, d Deps) ([]string, error) {
	res, err := d.Runner.Run(ctx, nil, "timedatectl", "list-timezones", "--no-pager")
	if err != nil || res.ExitCode != 0 {
		return nil, apierr.New(apierr.Internal, "Die Zeitzonen konnten nicht gelesen werden")
	}
	return strings.Fields(string(res.Stdout)), nil
}

func validNTPServer(s string) bool {
	if _, err := netip.ParseAddr(s); err == nil {
		return true
	}
	return len(s) <= 253 && ntpHostRe.MatchString(s)
}

// rewriteHosts setzt den Eintrag für 127.0.1.1 auf den neuen Hostnamen, damit er lokal auflösbar bleibt.
func rewriteHosts(content, host string) string {
	line := "127.0.1.1\t" + host + ".local " + host
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	found := false
	for i, l := range lines {
		if f := strings.Fields(l); len(f) > 0 && f[0] == "127.0.1.1" {
			lines[i], found = line, true
		}
	}
	if !found {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n") + "\n"
}

// SetSysSettings ändert Hostname, Zeitzone, NTP an/aus und NTP-Server.
func SetSysSettings(ctx context.Context, d Deps, c SysSettingsChange) error {
	mutate.Lock()
	defer mutate.Unlock()
	run := func(msg, name string, args ...string) error {
		res, err := d.Runner.Run(ctx, nil, name, args...)
		if err != nil || res.ExitCode != 0 {
			return apierr.New(apierr.Internal, msg).With("detail", strings.TrimSpace(string(res.Stderr)))
		}
		return nil
	}
	// Erst alles prüfen, dann ändern.
	if c.Hostname != nil && !hostnameRe.MatchString(*c.Hostname) {
		return apierr.New(apierr.InvalidParams, "Ungültiger Hostname (Kleinbuchstaben, Ziffern und Bindestriche, höchstens 63 Zeichen)")
	}
	if c.Timezone != nil {
		zones, err := Timezones(ctx, d)
		if err != nil {
			return err
		}
		ok := false
		for _, z := range zones {
			ok = ok || z == *c.Timezone
		}
		if !ok {
			return apierr.New(apierr.InvalidParams, "Unbekannte Zeitzone")
		}
	}
	var servers []string
	if c.NTPServers != nil {
		if len(*c.NTPServers) > 4 {
			return apierr.New(apierr.InvalidParams, "Höchstens 4 NTP-Server")
		}
		for _, s := range *c.NTPServers {
			s = strings.ToLower(strings.TrimSpace(s))
			if !validNTPServer(s) {
				return apierr.New(apierr.InvalidParams, "Ungültiger NTP-Server: "+s)
			}
			servers = append(servers, s)
		}
	}
	if c.Hostname != nil {
		if err := run("Der Hostname konnte nicht geändert werden", "hostnamectl", "set-hostname", *c.Hostname); err != nil {
			return err
		}
		hosts, _ := d.FS.ReadFile(hostsFile)
		if err := d.FS.WriteFile(hostsFile, []byte(rewriteHosts(string(hosts), *c.Hostname)), 0o644); err != nil {
			return err
		}
	}
	if c.Timezone != nil {
		if err := run("Die Zeitzone konnte nicht geändert werden", "timedatectl", "set-timezone", *c.Timezone); err != nil {
			return err
		}
	}
	if c.NTP != nil {
		v := "false"
		if *c.NTP {
			v = "true"
		}
		if err := run("NTP konnte nicht umgeschaltet werden", "timedatectl", "set-ntp", v); err != nil {
			return err
		}
	}
	if c.NTPServers != nil {
		if len(servers) == 0 {
			_ = d.FS.Remove(timesyncDropin)
		} else {
			if err := d.FS.MkdirAll("/etc/systemd/timesyncd.conf.d", 0o755); err != nil {
				return err
			}
			body := "# Verwaltet von BetterNAS – nicht manuell bearbeiten\n[Time]\nNTP=" + strings.Join(servers, " ") + "\n"
			if err := d.FS.WriteFile(timesyncDropin, []byte(body), 0o644); err != nil {
				return err
			}
		}
		if err := run("Der Zeitdienst konnte nicht neu gestartet werden", "systemctl", "restart", "systemd-timesyncd"); err != nil {
			return err
		}
	}
	return nil
}

// Power startet das NAS neu oder fährt es herunter. Die Ausführung folgt verzögert, damit die Antwort noch ankommt.
func Power(ctx context.Context, d Deps, action string) error {
	var verb string
	switch action {
	case "reboot":
		verb = "reboot"
	case "poweroff":
		verb = "poweroff"
	default:
		return apierr.New(apierr.InvalidParams, "Unbekannte Aktion")
	}
	res, err := d.Runner.Run(ctx, nil, "systemd-run", "--unit=bnas-power", "--on-active=3", "systemctl", verb)
	if err != nil || res.ExitCode != 0 {
		return apierr.New(apierr.Internal, "Die Aktion konnte nicht gestartet werden").With("detail", strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

func registerSysConfig(s *rpc.Server, d Deps) {
	s.Handle("system.settings.get", func(ctx context.Context, _ json.RawMessage) (any, error) { return GetSysSettings(ctx, d) })
	s.Handle("system.timezones", func(ctx context.Context, _ json.RawMessage) (any, error) { return Timezones(ctx, d) })
	s.Handle("system.settings.set", func(ctx context.Context, p json.RawMessage) (any, error) {
		var c SysSettingsChange
		if err := decode(p, &c); err != nil {
			return nil, err
		}
		return nil, SetSysSettings(ctx, d, c)
	})
	s.Handle("system.power", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Action string `json:"action"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return nil, Power(ctx, d, req.Action)
	})
}
