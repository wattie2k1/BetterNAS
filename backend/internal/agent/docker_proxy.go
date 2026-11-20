package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/docker"
)

const proxyCaddyCmd = "/etc/caddy/Caddyfile"

func proxyConf() string    { return proxyDir() + "/conf" }
func proxyNew() string     { return proxyConf() + "/Caddyfile.new" }
func proxyCurrent() string { return proxyConf() + "/Caddyfile" }

// ProxyWrite legt die Verzeichnisse und den Stack des Reverse-Proxys an und schreibt die neue Caddy-Konfiguration
// als Caddyfile.new. Erst ProxyValidate und ProxyCommit machen sie zur laufenden Konfiguration.
// Die Einstellungen werden hier erneut geprüft und die Dateien aus ihnen erzeugt (nie aus fremden Texten).
func ProxyWrite(_ context.Context, d Deps, cfg docker.ProxyConfig) error {
	cfg, err := cfg.Normalize()
	if err != nil {
		return err
	}
	for _, sub := range []string{"conf", "data", "config"} {
		if err := d.FS.MkdirAll(proxyDir()+"/"+sub, 0o755); err != nil {
			return err
		}
	}
	if err := SaveStack(context.Background(), d, StackSave{StackFiles: StackFiles{Name: docker.ProxyStack, Compose: docker.RenderProxyCompose(cfg, proxyDir())}}); err != nil {
		return err
	}
	return d.FS.WriteFile(proxyNew(), []byte(docker.RenderCaddyfile(cfg)), 0o644)
}

// ProxyValidate lässt Caddy die neue Konfiguration prüfen (das Image muss vorhanden sein). Bei einem Fehler wird sie verworfen.
func ProxyValidate(ctx context.Context, d Deps) error {
	if !d.FS.Exists(proxyNew()) {
		return apierr.New(apierr.NotFound, "Keine neue Konfiguration vorhanden")
	}
	res, err := d.Runner.Run(ctx, nil, "docker", "run", "--rm", "-v", proxyConf()+":/etc/caddy:ro", docker.ProxyImage,
		"caddy", "validate", "--config", "/etc/caddy/Caddyfile.new", "--adapter", "caddyfile")
	if err != nil {
		return apierr.New(apierr.Internal, "Docker ist nicht verfügbar")
	}
	if res.ExitCode != 0 {
		_ = d.FS.Remove(proxyNew())
		return apierr.New(apierr.InvalidParams, "Caddy lehnt die Konfiguration ab: "+caddyErrors(string(res.Stderr)))
	}
	return nil
}

// caddyErrors macht aus Caddys Ausgabe (JSON-Logzeilen oder „Error: …“) eine lesbare Meldung.
func caddyErrors(stderr string) string {
	var msgs []string
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		var entry struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		}
		switch {
		case line == "":
		case strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &entry) == nil:
			if entry.Level == "error" || entry.Level == "fatal" {
				msgs = append(msgs, strings.TrimPrefix(strings.ReplaceAll(entry.Msg, "/etc/caddy/Caddyfile.new:", "Zeile "), "adapting config using caddyfile: "))
			}
		case strings.HasPrefix(line, "Error:"):
			msgs = append(msgs, strings.TrimSpace(strings.TrimPrefix(line, "Error:")))
		}
	}
	if len(msgs) == 0 {
		return strings.TrimSpace(stderr)
	}
	return strings.Join(msgs, "; ")
}

// ProxyCommit macht die geprüfte Konfiguration zur laufenden.
func ProxyCommit(_ context.Context, d Deps) error {
	if !d.FS.Exists(proxyNew()) {
		return apierr.New(apierr.NotFound, "Keine neue Konfiguration vorhanden")
	}
	if d.FS.Exists(proxyCurrent()) {
		if err := d.FS.Remove(proxyCurrent()); err != nil {
			return err
		}
	}
	return d.FS.Rename(proxyNew(), proxyCurrent())
}

// ProxyReload lädt die Konfiguration im laufenden Caddy-Container neu (ohne Verbindungen zu kappen).
func ProxyReload(ctx context.Context, d Deps) error {
	if _, err := run(ctx, d.Runner, nil, "docker", "exec", docker.ProxyContainer, "caddy", "reload", "--config", proxyCaddyCmd, "--adapter", "caddyfile"); err != nil {
		return apierr.New(apierr.Conflict, "Caddy konnte die Konfiguration nicht neu laden").With("detail", err.Error())
	}
	return nil
}

func registerDockerProxy(s dockerReg, d Deps) {
	s.Handle("proxy.write", func(ctx context.Context, p json.RawMessage) (any, error) {
		var cfg docker.ProxyConfig
		if err := decode(p, &cfg); err != nil {
			return nil, err
		}
		mutate.Lock()
		defer mutate.Unlock()
		if err := ProxyWrite(ctx, d, cfg); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	})
	for method, fn := range map[string]func(context.Context, Deps) error{
		"proxy.validate": ProxyValidate,
		"proxy.commit":   ProxyCommit,
		"proxy.reload":   ProxyReload,
	} {
		s.Handle(method, func(ctx context.Context, _ json.RawMessage) (any, error) {
			if err := fn(ctx, d); err != nil {
				return nil, err
			}
			return map[string]bool{"ok": true}, nil
		})
	}
}
