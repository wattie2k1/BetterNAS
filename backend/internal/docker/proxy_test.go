package docker

import (
	"strings"
	"testing"
)

func validCfg() ProxyConfig {
	return ProxyConfig{
		Enabled: true, Email: "admin@example.de", HTTPPort: 8080, HTTPSPort: 8443,
		Sites: []ProxySite{
			{Domain: "jellyfin.example.de", Port: 8096},
			{Domain: "cloud.example.de", Port: 8443, HTTPS: true, Public: true},
			{Domain: "drucker.example.de", Host: "192.168.1.50", Port: 80, Public: true},
		},
	}
}

func TestProxyConfigValidate(t *testing.T) {
	if _, err := validCfg().Normalize(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(c *ProxyConfig){
		"HTTP-Port 0":              func(c *ProxyConfig) { c.HTTPPort = 0 },
		"HTTPS-Port zu groß":       func(c *ProxyConfig) { c.HTTPSPort = 70000 },
		"443 gehört BetterNAS":     func(c *ProxyConfig) { c.HTTPSPort = 443 },
		"gleiche Ports":            func(c *ProxyConfig) { c.HTTPSPort = c.HTTPPort },
		"E-Mail mit Zeilenumbruch": func(c *ProxyConfig) { c.Email = "a@b.de\nrespond" },
		"E-Mail ohne @":            func(c *ProxyConfig) { c.Email = "admin" },
		"Domain mit Leerzeichen":   func(c *ProxyConfig) { c.Sites[0].Domain = "a b.example.de" },
		"Domain mit Klammer":       func(c *ProxyConfig) { c.Sites[0].Domain = "a.example.de {" },
		"Domain ohne Punkt":        func(c *ProxyConfig) { c.Sites[0].Domain = "jellyfin" },
		"Wildcard":                 func(c *ProxyConfig) { c.Sites[0].Domain = "*.example.de" },
		"IP als Domain":            func(c *ProxyConfig) { c.Sites[0].Domain = "192.168.1.5" },
		"doppelte Domain":          func(c *ProxyConfig) { c.Sites[1].Domain = "jellyfin.example.de" },
		"Ziel-Port 0":              func(c *ProxyConfig) { c.Sites[0].Port = 0 },
		"Ziel-Port 65536":          func(c *ProxyConfig) { c.Sites[0].Port = 65536 },
		"öffentliche Ziel-IP":      func(c *ProxyConfig) { c.Sites[2].Host = "8.8.8.8" },
		"Ziel-Host als Name":       func(c *ProxyConfig) { c.Sites[2].Host = "evil.example.com" },
		"Ziel-Host Injektion":      func(c *ProxyConfig) { c.Sites[2].Host = "192.168.1.5\n}" },
		"IPv6-Ziel":                func(c *ProxyConfig) { c.Sites[2].Host = "fd00::1" },
		"zu viele Einträge":        func(c *ProxyConfig) { c.Sites = make([]ProxySite, 101) },
	}
	for name, mutate := range bad {
		c := validCfg()
		c.Sites = append([]ProxySite(nil), c.Sites...)
		mutate(&c)
		if _, err := c.Normalize(); err == nil {
			t.Errorf("%s wurde akzeptiert", name)
		}
	}
}

func TestProxyConfigNormalizes(t *testing.T) {
	c := ProxyConfig{HTTPPort: 8080, HTTPSPort: 8443, Email: " Admin@Example.de ", Sites: []ProxySite{
		{Domain: " Cloud.Example.DE ", Port: 80}, {Domain: "a.example.de", Port: 80},
	}}
	n, err := c.Normalize()
	if err != nil || n.Email != "Admin@Example.de" || n.Sites[0].Domain != "a.example.de" || n.Sites[1].Domain != "cloud.example.de" {
		t.Fatalf("%+v %v", n, err)
	}
	if _, err := (ProxyConfig{HTTPPort: 8080, HTTPSPort: 8443}).Normalize(); err != nil {
		t.Fatalf("leere Konfiguration: %v", err)
	}
}

func TestDefaultProxyConfig(t *testing.T) {
	d := DefaultProxyConfig()
	if d.Enabled || d.HTTPPort != 8080 || d.HTTPSPort != 8443 || d.Sites == nil {
		t.Fatalf("%+v", d)
	}
}

func TestRenderCaddyfile(t *testing.T) {
	c, _ := validCfg().Normalize()
	got := RenderCaddyfile(c)
	want := `# Von BetterNAS erzeugt – Änderungen bitte unter Docker → Reverse-Proxy vornehmen.
{
	email admin@example.de
}

cloud.example.de {
	reverse_proxy host.docker.internal:8443 {
		transport http {
			tls_insecure_skip_verify
		}
	}
}

drucker.example.de {
	reverse_proxy 192.168.1.50:80
}

jellyfin.example.de {
	@outside not remote_ip private_ranges
	handle @outside {
		respond "Nur aus dem Heimnetz erreichbar." 403
	}
	handle {
		reverse_proxy host.docker.internal:8096
	}
}
`
	if got != want {
		t.Fatalf("Caddyfile weicht ab:\n%s\n--- erwartet:\n%s", got, want)
	}
}

func TestRenderCaddyfileWithoutSitesOrEmail(t *testing.T) {
	got := RenderCaddyfile(ProxyConfig{HTTPPort: 8080, HTTPSPort: 8443})
	if strings.Contains(got, "email") || strings.Contains(got, "{") || !strings.HasPrefix(got, "# Von BetterNAS") {
		t.Fatalf("%q", got)
	}
}

func TestRenderProxyCompose(t *testing.T) {
	got := RenderProxyCompose(ProxyConfig{HTTPPort: 8080, HTTPSPort: 9443})
	for _, want := range []string{
		"image: caddy:2", "restart: unless-stopped",
		`"8080:80"`, `"9443:443"`, `"9443:443/udp"`,
		"host.docker.internal:host-gateway",
		"/srv/appdata/reverse-proxy/conf:/etc/caddy", "/srv/appdata/reverse-proxy/data:/data", "/srv/appdata/reverse-proxy/config:/config",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("fehlt %q:\n%s", want, got)
		}
	}
}
