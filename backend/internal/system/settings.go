package system

import (
	"context"
	"database/sql"

	"github.com/betternas/betternas/internal/audit"
)

// Settings verwaltet Hostname, Zeit und Energie über den Agent.
type Settings struct {
	db    *sql.DB
	agent Agent
}

// NewSettings erzeugt den Manager.
func NewSettings(d *sql.DB, a Agent) *Settings { return &Settings{db: d, agent: a} }

func (s *Settings) record(ctx context.Context, actor, action, target string, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	_ = audit.Record(ctx, s.db, audit.Entry{Actor: actor, Action: action, Target: target, Result: result})
}

// Get liefert Hostname, Zeitzone und NTP-Einstellungen.
func (s *Settings) Get(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	err := s.agent.Call(ctx, "system.settings.get", nil, &out)
	return out, err
}

// Timezones liefert alle gültigen Zeitzonen.
func (s *Settings) Timezones(ctx context.Context) ([]string, error) {
	out := []string{}
	err := s.agent.Call(ctx, "system.timezones", nil, &out)
	return out, err
}

// Set ändert Hostname, Zeitzone, NTP und NTP-Server (nur gesetzte Felder).
func (s *Settings) Set(ctx context.Context, actor string, change map[string]any) error {
	err := s.agent.Call(ctx, "system.settings.set", change, nil)
	target := ""
	for k := range change {
		target += k + " "
	}
	s.record(ctx, actor, "system.settings.set", target, err)
	return err
}

// Power startet neu oder fährt herunter (action: reboot | poweroff).
func (s *Settings) Power(ctx context.Context, actor, action string) error {
	err := s.agent.Call(ctx, "system.power", map[string]string{"action": action}, nil)
	s.record(ctx, actor, "system.power", action, err)
	return err
}

// Services liefert den Zustand der steuerbaren Dienste.
func (s *Settings) Services(ctx context.Context) ([]map[string]any, error) {
	out := []map[string]any{}
	err := s.agent.Call(ctx, "service.list", nil, &out)
	return out, err
}

// ServiceAction startet, stoppt oder startet einen Dienst neu.
func (s *Settings) ServiceAction(ctx context.Context, actor, unit, action string) error {
	err := s.agent.Call(ctx, "service.action", map[string]string{"unit": unit, "action": action}, nil)
	s.record(ctx, actor, "service."+action, unit, err)
	return err
}

// SMB liefert die globalen Samba-Einstellungen.
func (s *Settings) SMB(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	err := s.agent.Call(ctx, "smb.global.get", nil, &out)
	return out, err
}

// SetSMB speichert die globalen Samba-Einstellungen.
func (s *Settings) SetSMB(ctx context.Context, actor string, v map[string]any) error {
	err := s.agent.Call(ctx, "smb.global.set", v, nil)
	s.record(ctx, actor, "smb.global.set", "smb", err)
	return err
}

// Docker liefert die Docker-Einstellungen.
func (s *Settings) Docker(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	err := s.agent.Call(ctx, "docker.settings.get", nil, &out)
	return out, err
}

// SetDocker speichert die Docker-Einstellungen (ohne Neustart des Dienstes).
func (s *Settings) SetDocker(ctx context.Context, actor string, v map[string]any) error {
	err := s.agent.Call(ctx, "docker.settings.set", v, nil)
	s.record(ctx, actor, "docker.settings.set", "docker", err)
	return err
}

// SSH liefert die SSH-Einstellungen.
func (s *Settings) SSH(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	err := s.agent.Call(ctx, "ssh.get", nil, &out)
	return out, err
}

// SetSSH speichert die SSH-Einstellungen.
func (s *Settings) SetSSH(ctx context.Context, actor string, v map[string]any) error {
	err := s.agent.Call(ctx, "ssh.set", v, nil)
	s.record(ctx, actor, "ssh.set", "ssh", err)
	return err
}

// SSHKeys liefert die öffentlichen Schlüssel eines Benutzers.
func (s *Settings) SSHKeys(ctx context.Context, user string) ([]map[string]any, error) {
	out := []map[string]any{}
	err := s.agent.Call(ctx, "ssh.keys.list", map[string]string{"user": user}, &out)
	return out, err
}

// AddSSHKey hinterlegt einen öffentlichen Schlüssel.
func (s *Settings) AddSSHKey(ctx context.Context, actor, user, key string) error {
	err := s.agent.Call(ctx, "ssh.keys.add", map[string]string{"user": user, "key": key}, nil)
	s.record(ctx, actor, "ssh.key.add", user, err)
	return err
}

// RemoveSSHKey entfernt einen öffentlichen Schlüssel.
func (s *Settings) RemoveSSHKey(ctx context.Context, actor, user, fingerprint string) error {
	err := s.agent.Call(ctx, "ssh.keys.remove", map[string]string{"user": user, "fingerprint": fingerprint}, nil)
	s.record(ctx, actor, "ssh.key.remove", user, err)
	return err
}

// Firewall liefert Konfiguration und Zustand der Firewall.
func (s *Settings) Firewall(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	err := s.agent.Call(ctx, "firewall.get", nil, &out)
	return out, err
}

// SetFirewall übernimmt die Firewall-Konfiguration vorläufig (Bestätigung nötig).
func (s *Settings) SetFirewall(ctx context.Context, actor, clientIP string, cfg map[string]any) error {
	err := s.agent.Call(ctx, "firewall.set", map[string]any{"config": cfg, "clientIp": clientIP}, nil)
	s.record(ctx, actor, "firewall.set", "firewall", err)
	return err
}

// ConfirmFirewall bestätigt die Firewall-Änderung.
func (s *Settings) ConfirmFirewall(ctx context.Context, actor string) error {
	err := s.agent.Call(ctx, "firewall.confirm", nil, nil)
	s.record(ctx, actor, "firewall.confirm", "firewall", err)
	return err
}

// RevertFirewall nimmt die Firewall-Änderung zurück.
func (s *Settings) RevertFirewall(ctx context.Context, actor string) error {
	err := s.agent.Call(ctx, "firewall.revert", nil, nil)
	s.record(ctx, actor, "firewall.revert", "firewall", err)
	return err
}
