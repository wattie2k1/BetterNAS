// Package agent enthält die privilegierten Operationen des betternas-agent.
package agent

import (
	"context"
	"regexp"

	"github.com/betternas/betternas/internal/sysexec"
)

var usernameRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// unixChkpwd ist der Passwort-Helper von pam_unix. Als root aufgerufen prüft er beliebige Konten
// gegen /etc/shadow; das Passwort wird NUL-terminiert über stdin übergeben.
const unixChkpwd = "/usr/sbin/unix_chkpwd"

// VerifyPassword prüft ein Passwort. Ungültige Benutzernamen werden ohne Aufruf abgelehnt.
func VerifyPassword(ctx context.Context, r sysexec.Runner, username, password string) (bool, error) {
	if !usernameRe.MatchString(username) {
		return false, nil
	}
	res, err := r.Run(ctx, []byte(password+"\x00"), unixChkpwd, username, "nonull")
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0, nil
}
