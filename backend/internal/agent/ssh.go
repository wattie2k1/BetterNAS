package agent

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
)

const (
	sshDropinDir   = "/etc/ssh/sshd_config.d"
	sshDropinFile  = sshDropinDir + "/10-betternas.conf"
	sshDefaultPort = 22
)

// SSHSettings sind die von BetterNAS verwalteten Werte des SSH-Servers.
type SSHSettings struct {
	Port            int    `json:"port"`
	PasswordAuth    bool   `json:"passwordAuth"`
	PermitRootLogin string `json:"permitRootLogin"` // "no" | "prohibit-password"
	SocketActivated bool   `json:"socketActivated"` // nur Anzeige: Port ist dann nicht änderbar
	Active          bool   `json:"active"`
}

func sshDefaults() SSHSettings {
	return SSHSettings{Port: sshDefaultPort, PasswordAuth: true, PermitRootLogin: "prohibit-password"}
}

func parseSSHDropin(s string) SSHSettings {
	g := sshDefaults()
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch strings.ToLower(f[0]) {
		case "port":
			if n, err := strconv.Atoi(f[1]); err == nil {
				g.Port = n
			}
		case "passwordauthentication":
			g.PasswordAuth = f[1] == "yes"
		case "permitrootlogin":
			g.PermitRootLogin = f[1]
		}
	}
	return g
}

func renderSSHDropin(g SSHSettings) string {
	yn := "no"
	if g.PasswordAuth {
		yn = "yes"
	}
	return fmt.Sprintf("%s\nPort %d\nPasswordAuthentication %s\nPermitRootLogin %s\n", managedHeader, g.Port, yn, g.PermitRootLogin)
}

// sshPort liefert den konfigurierten SSH-Port (für die Firewall).
func sshPort(d Deps) int {
	if b, err := d.FS.ReadFile(sshDropinFile); err == nil {
		return parseSSHDropin(string(b)).Port
	}
	return sshDefaultPort
}

func unitActive(ctx context.Context, d Deps, unit string) bool {
	res, err := d.Runner.Run(ctx, nil, "systemctl", "is-active", unit)
	return err == nil && res.ExitCode == 0 && strings.TrimSpace(string(res.Stdout)) == "active"
}

// GetSSHSettings liest die verwalteten Werte (Standardwerte, solange nichts gespeichert ist).
func GetSSHSettings(ctx context.Context, d Deps) SSHSettings {
	g := sshDefaults()
	if b, err := d.FS.ReadFile(sshDropinFile); err == nil {
		g = parseSSHDropin(string(b))
	}
	g.Active = unitActive(ctx, d, "ssh")
	g.SocketActivated = unitActive(ctx, d, "ssh.socket")
	return g
}

// SetSSHSettings schreibt die Werte, prüft sie mit sshd -t und lädt den Dienst neu. Bei Fehlern bleibt alles wie vorher.
func SetSSHSettings(ctx context.Context, d Deps, g SSHSettings) error {
	mutate.Lock()
	defer mutate.Unlock()
	if g.Port < 1 || g.Port > 65535 {
		return apierr.New(apierr.InvalidParams, "Ungültiger Port (1–65535)")
	}
	if g.Port == 80 || g.Port == 443 {
		return apierr.New(apierr.InvalidParams, "Die Ports 80 und 443 gehören der Weboberfläche")
	}
	if g.PermitRootLogin != "no" && g.PermitRootLogin != "prohibit-password" {
		return apierr.New(apierr.InvalidParams, "Ungültige Einstellung für die Root-Anmeldung")
	}
	if old, err := d.FS.ReadFile(sshDropinFile); err == nil && !isManaged(old) {
		return apierr.New(apierr.Conflict, "Die SSH-Konfiguration wird nicht von BetterNAS verwaltet und wird nicht verändert.")
	}
	cur := GetSSHSettings(ctx, d)
	if g.Port != cur.Port && cur.SocketActivated {
		return apierr.New(apierr.Conflict, "SSH wird über ssh.socket gestartet; der Port lässt sich hier nicht ändern.")
	}
	if !g.PasswordAuth && cur.PasswordAuth {
		n, err := countAuthorizedKeys(ctx, d)
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.New(apierr.Conflict, "Hinterlegen Sie zuerst einen öffentlichen Schlüssel; ohne Schlüssel und ohne Passwort käme niemand mehr per SSH hinein.")
		}
	}
	old, haveOld := d.FS.ReadFile(sshDropinFile)
	rollback := func() {
		if haveOld == nil {
			_ = d.FS.WriteFile(sshDropinFile, old, 0o644)
		} else {
			_ = d.FS.Remove(sshDropinFile)
		}
	}
	if err := d.FS.MkdirAll(sshDropinDir, 0o755); err != nil {
		return err
	}
	if err := d.FS.WriteFile(sshDropinFile, []byte(renderSSHDropin(g)), 0o644); err != nil {
		return err
	}
	if res, err := d.Runner.Run(ctx, nil, "sshd", "-t"); err != nil || res.ExitCode != 0 {
		rollback()
		return apierr.New(apierr.InvalidParams, "SSH lehnt die Konfiguration ab; es wurde nichts geändert.").With("detail", strings.TrimSpace(string(res.Stderr)))
	}
	if cur.Active {
		if res, err := d.Runner.Run(ctx, nil, "systemctl", "reload", "ssh"); err != nil || res.ExitCode != 0 {
			rollback()
			_, _ = d.Runner.Run(ctx, nil, "systemctl", "reload", "ssh")
			return apierr.New(apierr.Internal, "SSH konnte die Konfiguration nicht laden; die vorherige wurde wiederhergestellt").With("detail", strings.TrimSpace(string(res.Stderr)))
		}
	}
	if g.Port != cur.Port {
		return reloadFirewallForSSHPort(ctx, d, g.Port)
	}
	return nil
}

// ---- Schlüssel je Benutzer ----

// SSHKey ist ein öffentlicher Schlüssel in der authorized_keys eines Benutzers.
type SSHKey struct {
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	Comment     string `json:"comment"`
}

var sshKeyTypes = map[string]bool{
	"ssh-ed25519": true, "ssh-rsa": true,
	"ecdsa-sha2-nistp256": true, "ecdsa-sha2-nistp384": true, "ecdsa-sha2-nistp521": true,
	"sk-ssh-ed25519@openssh.com": true, "sk-ecdsa-sha2-nistp256@openssh.com": true,
}

// ParseSSHKey prüft eine Schlüsselzeile (ohne Optionen) und liefert die normalisierte Zeile.
func ParseSSHKey(line string) (SSHKey, string, error) {
	bad := func(msg string) (SSHKey, string, error) { return SSHKey{}, "", apierr.New(apierr.InvalidParams, msg) }
	line = strings.TrimSpace(line)
	if line == "" || strings.ContainsAny(line, "\r\n") {
		return bad("Bitte genau eine Zeile mit einem öffentlichen Schlüssel einfügen")
	}
	f := strings.Fields(line)
	if len(f) < 2 || !sshKeyTypes[f[0]] {
		return bad("Das ist kein gültiger öffentlicher SSH-Schlüssel (er beginnt z. B. mit ssh-ed25519)")
	}
	blob, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil || len(blob) < 16 {
		return bad("Der Schlüsseltext ist ungültig")
	}
	comment := strings.Join(f[2:], " ")
	if len(comment) > 100 {
		return bad("Der Kommentar ist zu lang (höchstens 100 Zeichen)")
	}
	for _, r := range comment {
		if r < 0x20 || r == 0x7f {
			return bad("Der Kommentar enthält Steuerzeichen")
		}
	}
	sum := sha256.Sum256(blob)
	k := SSHKey{Type: f[0], Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), Comment: comment}
	norm := f[0] + " " + f[1]
	if comment != "" {
		norm += " " + comment
	}
	return k, norm, nil
}

type sshUser struct{ name, home, uid, gid string }

// sshUsers liefert die Benutzerkonten, für die Schlüssel verwaltet werden dürfen (BetterNAS-Benutzer inkl. Installer-Admin).
func sshUsers(ctx context.Context, d Deps) (map[string]sshUser, error) {
	acc, err := readAccounts(d)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, u := range acc.accounts.Users {
		allowed[u.Name] = true
	}
	out := map[string]sshUser{}
	for _, line := range strings.Split(string(acc.passwd), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 7 || !allowed[f[0]] || f[5] == "" || !strings.HasPrefix(f[5], "/") || strings.Contains(f[5], "..") || f[5] == "/" {
			continue
		}
		out[f[0]] = sshUser{name: f[0], home: f[5], uid: f[2], gid: f[3]}
	}
	return out, nil
}

func keyFile(u sshUser) string { return u.home + "/.ssh/authorized_keys" }

func readKeyLines(d Deps, u sshUser) []string {
	b, err := d.FS.ReadFile(keyFile(u))
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func countAuthorizedKeys(ctx context.Context, d Deps) (int, error) {
	us, err := sshUsers(ctx, d)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range us {
		for _, l := range readKeyLines(d, u) {
			if _, _, err := ParseSSHKey(l); err == nil {
				n++
			}
		}
	}
	return n, nil
}

func sshUserFor(ctx context.Context, d Deps, name string) (sshUser, error) {
	us, err := sshUsers(ctx, d)
	if err != nil {
		return sshUser{}, err
	}
	u, ok := us[name]
	if !ok {
		return sshUser{}, apierr.New(apierr.NotFound, "Benutzer nicht gefunden")
	}
	return u, nil
}

// ListSSHKeys liefert die Schlüssel eines Benutzers.
func ListSSHKeys(ctx context.Context, d Deps, user string) ([]SSHKey, error) {
	u, err := sshUserFor(ctx, d, user)
	if err != nil {
		return nil, err
	}
	out := []SSHKey{}
	for _, l := range readKeyLines(d, u) {
		if k, _, err := ParseSSHKey(l); err == nil {
			out = append(out, k)
		}
	}
	return out, nil
}

func writeKeyLines(ctx context.Context, d Deps, u sshUser, lines []string) error {
	dir := u.home + "/.ssh"
	if err := d.FS.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := d.FS.WriteFile(keyFile(u), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	owner := u.uid + ":" + u.gid
	for _, p := range []string{dir, keyFile(u)} {
		if res, err := d.Runner.Run(ctx, nil, "chown", owner, p); err != nil || res.ExitCode != 0 {
			return apierr.New(apierr.Internal, "Die Rechte der Schlüsseldatei konnten nicht gesetzt werden")
		}
	}
	return nil
}

// AddSSHKey fügt einen Schlüssel hinzu (doppelte werden abgelehnt).
func AddSSHKey(ctx context.Context, d Deps, user, line string) error {
	mutate.Lock()
	defer mutate.Unlock()
	u, err := sshUserFor(ctx, d, user)
	if err != nil {
		return err
	}
	k, norm, err := ParseSSHKey(line)
	if err != nil {
		return err
	}
	lines := readKeyLines(d, u)
	if len(lines) >= 50 {
		return apierr.New(apierr.InvalidParams, "Höchstens 50 Schlüssel je Benutzer")
	}
	for _, l := range lines {
		if e, _, err := ParseSSHKey(l); err == nil && e.Fingerprint == k.Fingerprint {
			return apierr.New(apierr.Conflict, "Dieser Schlüssel ist bereits hinterlegt")
		}
	}
	return writeKeyLines(ctx, d, u, append(lines, norm))
}

// RemoveSSHKey entfernt den Schlüssel mit dem Fingerabdruck. Der letzte Schlüssel bleibt, solange die
// Passwort-Anmeldung aus ist.
func RemoveSSHKey(ctx context.Context, d Deps, user, fingerprint string) error {
	mutate.Lock()
	defer mutate.Unlock()
	u, err := sshUserFor(ctx, d, user)
	if err != nil {
		return err
	}
	var keep []string
	found := false
	for _, l := range readKeyLines(d, u) {
		if k, _, err := ParseSSHKey(l); err == nil && k.Fingerprint == fingerprint {
			found = true
			continue
		}
		keep = append(keep, l)
	}
	if !found {
		return apierr.New(apierr.NotFound, "Schlüssel nicht gefunden")
	}
	if cur := (func() SSHSettings {
		if b, err := d.FS.ReadFile(sshDropinFile); err == nil {
			return parseSSHDropin(string(b))
		}
		return sshDefaults()
	})(); !cur.PasswordAuth {
		if n, err := countAuthorizedKeys(ctx, d); err == nil && n <= 1 {
			return apierr.New(apierr.Conflict, "Das ist der letzte Schlüssel und die Passwort-Anmeldung ist aus. Schalten Sie zuerst die Passwort-Anmeldung wieder ein.")
		}
	}
	return writeKeyLines(ctx, d, u, keep)
}

func registerSSH(s *rpc.Server, d Deps) {
	s.Handle("ssh.get", func(ctx context.Context, _ json.RawMessage) (any, error) { return GetSSHSettings(ctx, d), nil })
	s.Handle("ssh.set", func(ctx context.Context, p json.RawMessage) (any, error) {
		var g SSHSettings
		if err := decode(p, &g); err != nil {
			return nil, err
		}
		return nil, SetSSHSettings(ctx, d, g)
	})
	s.Handle("ssh.keys.list", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct{ User string }
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return ListSSHKeys(ctx, d, req.User)
	})
	s.Handle("ssh.keys.add", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct{ User, Key string }
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return nil, AddSSHKey(ctx, d, req.User, req.Key)
	})
	s.Handle("ssh.keys.remove", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct{ User, Fingerprint string }
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		return nil, RemoveSSHKey(ctx, d, req.User, req.Fingerprint)
	})
}
