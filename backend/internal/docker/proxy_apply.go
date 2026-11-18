package docker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/jobs"
)

const (
	keyProxy      = "docker.proxy"
	proxyReloads  = 5 // Versuche, Caddy neu zu laden, während der Container noch startet
	proxyAuditKey = "docker.proxy"
)

// Proxy liefert die gespeicherten Einstellungen des Reverse-Proxys (Standard: aus).
func (m *Manager) Proxy(ctx context.Context) (ProxyConfig, error) {
	var raw string
	err := m.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keyProxy).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultProxyConfig(), nil
	}
	if err != nil {
		return DefaultProxyConfig(), err
	}
	c := DefaultProxyConfig() // fehlende Felder behalten ihren Standard
	if json.Unmarshal([]byte(raw), &c) != nil {
		return DefaultProxyConfig(), nil
	}
	if c, err = c.Normalize(); err != nil {
		return DefaultProxyConfig(), nil
	}
	return c, nil
}

// SetProxy prüft und speichert die Einstellungen. Wirksam werden sie erst mit ApplyProxy.
func (m *Manager) SetProxy(ctx context.Context, actor string, in ProxyConfig) (ProxyConfig, error) {
	c, err := in.Normalize()
	if err != nil {
		return in, err
	}
	b, _ := json.Marshal(c)
	_, err = m.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, keyProxy, string(b))
	m.record(ctx, actor, proxyAuditKey+".config", ProxyStack, err)
	return c, err
}

// ApplyProxy setzt die gespeicherten Einstellungen als Job um: Dateien schreiben, Caddy-Image laden, die neue
// Konfiguration von Caddy prüfen lassen, erst dann übernehmen, den Stack starten und Caddy neu laden.
// Lehnt Caddy die Konfiguration ab, bleibt die bisherige unverändert.
func (m *Manager) ApplyProxy(ctx context.Context, actor string) (string, error) {
	cfg, err := m.Proxy(ctx)
	if err != nil {
		return "", err
	}
	return m.jobs.Start("docker.proxy", ProxyStack, actor, func(ctx context.Context, log jobs.Logger) (err error) {
		defer func() { m.record(ctx, actor, proxyAuditKey+".apply", ProxyStack, err) }()
		step := func(action string) error {
			unit, err := m.startStack(ctx, actor, ProxyStack, action, RunOptions{})
			if err != nil {
				return err
			}
			return m.waitStack(ctx, log, ProxyStack, action, unit)
		}
		log.Printf("Reverse-Proxy: Konfiguration wird geschrieben …")
		if err := m.agent.Call(ctx, "proxy.write", cfg, nil); err != nil {
			return err
		}
		if !cfg.Enabled {
			log.Printf("Reverse-Proxy: wird ausgeschaltet.")
			return step("down")
		}
		log.SetProgress(15)
		if err := step("pull"); err != nil {
			return err
		}
		log.SetProgress(40)
		log.Printf("Reverse-Proxy: Caddy prüft die Konfiguration …")
		if err := m.agent.Call(ctx, "proxy.validate", nil, nil); err != nil {
			return err
		}
		if err := m.agent.Call(ctx, "proxy.commit", nil, nil); err != nil {
			return err
		}
		log.SetProgress(60)
		if err := step("up"); err != nil {
			return err
		}
		log.SetProgress(85)
		var reloadErr error
		for i := 0; i < proxyReloads; i++ {
			if reloadErr = m.agent.Call(ctx, "proxy.reload", nil, nil); reloadErr == nil {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(m.proxyRetryEvery):
			}
		}
		if reloadErr != nil {
			return apierr.New(apierr.Conflict, "Caddy läuft, hat die Konfiguration aber nicht übernommen: "+apierr.From(reloadErr).Message)
		}
		log.Printf("Reverse-Proxy ist aktiv. Zertifikate holt Caddy beim ersten Aufruf der Domains.")
		return nil
	}), nil
}
