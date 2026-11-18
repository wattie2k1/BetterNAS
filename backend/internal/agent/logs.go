package agent

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/rpc"
)

// logUnits sind die einsehbaren Journale (Name in der Oberfläche → systemd-Einheit).
var logUnits = map[string]string{
	"api":     "betternas-api",
	"agent":   "betternas-agent",
	"samba":   "smbd",
	"docker":  "docker",
	"libvirt": "libvirtd",
}

const (
	logDefaultLines = 200
	logMaxLines     = 2000
)

// ReadLog liefert die letzten lines Zeilen des Journals der Quelle source.
func ReadLog(ctx context.Context, d Deps, source string, lines int) (string, error) {
	unit, ok := logUnits[source]
	if !ok {
		return "", apierr.New(apierr.InvalidParams, "Unbekannte Protokollquelle")
	}
	if lines <= 0 {
		lines = logDefaultLines
	}
	if lines > logMaxLines {
		lines = logMaxLines
	}
	res, err := d.Runner.Run(ctx, nil, "journalctl", "-u", unit+".service", "-o", "short-iso", "-n", strconv.Itoa(lines), "--no-pager")
	if err != nil {
		return "", apierr.New(apierr.Internal, "Das Protokoll konnte nicht gelesen werden")
	}
	return string(res.Stdout), nil
}

// supportCommands sind die Diagnosebefehle des Support-Pakets (Name der Datei → Befehl). Keine Geheimnisse.
var supportCommands = []struct {
	file string
	cmd  []string
}{
	{"system/uname.txt", []string{"uname", "-a"}},
	{"system/os-release.txt", []string{"cat", "/etc/os-release"}},
	{"system/uptime.txt", []string{"uptime"}},
	{"system/free.txt", []string{"free", "-h"}},
	{"system/df.txt", []string{"df", "-h"}},
	{"system/lsblk.txt", []string{"lsblk", "-o", "NAME,SIZE,TYPE,FSTYPE,MOUNTPOINT,MODEL"}},
	{"system/ip.txt", []string{"ip", "-br", "addr"}},
	{"system/failed-units.txt", []string{"systemctl", "--failed", "--no-pager"}},
	{"system/packages.txt", []string{"dpkg-query", "-W", "betternas", "docker.io", "snapraid", "mergerfs", "samba", "libvirt-daemon-system", "nfs-kernel-server"}},
	{"system/dmesg-errors.txt", []string{"journalctl", "-k", "-p", "err", "-n", "200", "--no-pager"}},
}

// CollectSupport sammelt Diagnosedaten als Dateiname → Inhalt. Fehlende Werkzeuge ergeben einen Hinweis statt eines Fehlers.
func CollectSupport(ctx context.Context, d Deps) map[string]string {
	out := map[string]string{}
	for _, c := range supportCommands {
		res, err := d.Runner.Run(ctx, nil, c.cmd[0], c.cmd[1:]...)
		if err != nil || res.ExitCode != 0 && len(res.Stdout) == 0 {
			out[c.file] = "(nicht verfügbar)\n"
			continue
		}
		out[c.file] = string(res.Stdout)
	}
	for name := range logUnits {
		txt, err := ReadLog(ctx, d, name, 1000)
		if err != nil {
			txt = "(nicht verfügbar)\n"
		}
		out["logs/"+name+".log"] = txt
	}
	return out
}

func registerLogs(s *rpc.Server, d Deps) {
	s.Handle("logs.read", func(ctx context.Context, p json.RawMessage) (any, error) {
		var req struct {
			Source string `json:"source"`
			Lines  int    `json:"lines"`
		}
		if err := decode(p, &req); err != nil {
			return nil, err
		}
		txt, err := ReadLog(ctx, d, strings.TrimSpace(req.Source), req.Lines)
		if err != nil {
			return nil, err
		}
		return map[string]string{"text": txt}, nil
	})
	s.Handle("support.collect", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return CollectSupport(ctx, d), nil
	})
}
