package docker

import (
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"github.com/betternas/betternas/internal/apierr"
)

// Reverse-Proxy: BetterNAS betreibt Caddy als eigenen Stack und erzeugt dessen Konfiguration aus einer
// validierten Liste (Domain → Ziel). Caddy holt und erneuert die Let's-Encrypt-Zertifikate selbst.
// Die BetterNAS-Oberfläche belegt Port 443; Caddy läuft deshalb auf eigenen Host-Ports, auf die der Router
// die Ports 80 und 443 weiterleitet (innen hört Caddy auf den Standard-Ports, die ACME-Prüfung funktioniert so).

const (
	// ProxyStack ist der Name des Stacks, den BetterNAS für den Reverse-Proxy verwaltet.
	ProxyStack = "reverse-proxy"
	// ProxyImage ist das Caddy-Image; Compose und Prüfung verwenden dasselbe.
	ProxyImage = "caddy:2"
	// ProxyContainer ist der Name des Caddy-Containers im Stack.
	ProxyContainer = "reverse-proxy-caddy-1"

	maxProxySites = 100
)

// ProxySite ist ein Eintrag: Anfragen an Domain gehen an das Ziel.
type ProxySite struct {
	Domain string `json:"domain"`
	// Host ist leer für dieses NAS, sonst die IPv4-Adresse eines Geräts im Heimnetz.
	Host string `json:"host"`
	Port int    `json:"port"`
	// HTTPS sagt, dass das Ziel selbst HTTPS spricht (meist mit selbstsigniertem Zertifikat, das nicht geprüft wird).
	HTTPS bool `json:"https"`
	// Public erlaubt den Zugriff auch aus dem Internet; sonst antwortet der Proxy nur Anfragen aus privaten Netzen.
	Public bool `json:"public"`
}

// ProxyConfig sind die Einstellungen des Reverse-Proxys.
type ProxyConfig struct {
	Enabled bool `json:"enabled"`
	// Email ist der Kontakt für Let's Encrypt (optional, aber empfohlen: Hinweise bei ablaufenden Zertifikaten).
	Email string `json:"email"`
	// HTTPPort und HTTPSPort sind die Ports auf diesem NAS, an die der Router 80 bzw. 443 weiterleitet.
	HTTPPort  int         `json:"httpPort"`
	HTTPSPort int         `json:"httpsPort"`
	Sites     []ProxySite `json:"sites"`
}

// DefaultProxyConfig: aus, Ports 8080/8443, keine Einträge.
func DefaultProxyConfig() ProxyConfig {
	return ProxyConfig{HTTPPort: 8080, HTTPSPort: 8443, Sites: []ProxySite{}}
}

var (
	domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	emailRe  = regexp.MustCompile(`^[A-Za-z0-9._%+-]{1,64}@[A-Za-z0-9.-]{1,253}\.[A-Za-z]{2,63}$`)
)

func proxyInvalid(format string, a ...any) error {
	return apierr.New(apierr.InvalidParams, fmt.Sprintf(format, a...))
}

func validPort(p int) bool { return p >= 1 && p <= 65535 }

// Normalize prüft die Einstellungen und liefert sie bereinigt (Domains klein, Einträge nach Domain sortiert).
// Nur geprüfte Werte gelangen später in die Caddy-Konfiguration.
func (c ProxyConfig) Normalize() (ProxyConfig, error) {
	if !validPort(c.HTTPPort) || !validPort(c.HTTPSPort) {
		return c, proxyInvalid("Die Ports müssen zwischen 1 und 65535 liegen.")
	}
	if c.HTTPPort == c.HTTPSPort {
		return c, proxyInvalid("HTTP- und HTTPS-Port müssen verschieden sein.")
	}
	if c.HTTPPort == 443 || c.HTTPSPort == 443 {
		return c, proxyInvalid("Port 443 belegt die BetterNAS-Oberfläche. Wähle einen anderen Port (z. B. 8443) und leite am Router 443 darauf weiter.")
	}
	c.Email = strings.TrimSpace(c.Email)
	if c.Email != "" && !emailRe.MatchString(c.Email) {
		return c, proxyInvalid("Die E-Mail-Adresse ist ungültig.")
	}
	if len(c.Sites) > maxProxySites {
		return c, proxyInvalid("Zu viele Einträge (höchstens %d).", maxProxySites)
	}
	seen := map[string]bool{}
	sites := make([]ProxySite, 0, len(c.Sites))
	for _, s := range c.Sites {
		s.Domain = strings.ToLower(strings.TrimSpace(s.Domain))
		s.Host = strings.TrimSpace(s.Host)
		if len(s.Domain) > 253 || !domainRe.MatchString(s.Domain) {
			return c, proxyInvalid("„%s“ ist keine gültige Domain (z. B. jellyfin.meinedomain.de, ohne http:// und Pfad).", s.Domain)
		}
		if seen[s.Domain] {
			return c, proxyInvalid("Die Domain %s kommt mehrfach vor.", s.Domain)
		}
		seen[s.Domain] = true
		if !validPort(s.Port) {
			return c, proxyInvalid("%s: Der Ziel-Port muss zwischen 1 und 65535 liegen.", s.Domain)
		}
		if s.Host != "" {
			a, err := netip.ParseAddr(s.Host)
			if err != nil || !a.Is4() || !a.IsPrivate() || a.String() != s.Host {
				return c, proxyInvalid("%s: Als Ziel sind nur IPv4-Adressen aus dem Heimnetz erlaubt (192.168.x.x, 10.x.x.x, 172.16–31.x.x).", s.Domain)
			}
		}
		sites = append(sites, s)
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].Domain < sites[j].Domain })
	c.Sites = sites
	return c, nil
}

// RenderCaddyfile erzeugt die Caddy-Konfiguration. Die Einstellungen müssen vorher Normalize bestanden haben.
func RenderCaddyfile(c ProxyConfig) string {
	var b strings.Builder
	b.WriteString("# Von BetterNAS erzeugt – Änderungen bitte unter Docker → Reverse-Proxy vornehmen.\n")
	if c.Email != "" {
		fmt.Fprintf(&b, "{\n\temail %s\n}\n", c.Email)
	}
	for _, s := range c.Sites {
		host := s.Host
		if host == "" {
			host = "host.docker.internal" // das NAS selbst, vom Container aus gesehen
		}
		var proxy string
		if s.HTTPS {
			proxy = fmt.Sprintf("reverse_proxy %s:%d {\n\t\ttransport http {\n\t\t\ttls_insecure_skip_verify\n\t\t}\n\t}", host, s.Port)
		} else {
			proxy = fmt.Sprintf("reverse_proxy %s:%d", host, s.Port)
		}
		fmt.Fprintf(&b, "\n%s {\n", s.Domain)
		if s.Public {
			fmt.Fprintf(&b, "\t%s\n", proxy)
		} else {
			fmt.Fprintf(&b, "\t@outside not remote_ip private_ranges\n\thandle @outside {\n\t\trespond \"Nur aus dem Heimnetz erreichbar.\" 403\n\t}\n\thandle {\n\t\t%s\n\t}\n", strings.ReplaceAll(proxy, "\n\t", "\n\t\t"))
		}
		b.WriteString("}\n")
	}
	return b.String()
}

// RenderProxyCompose erzeugt die compose.yaml des Reverse-Proxy-Stacks.
func RenderProxyCompose(c ProxyConfig, proxyDir string) string {
	return fmt.Sprintf(`# Von BetterNAS verwaltet. Die Ports stellst du unter Docker → Reverse-Proxy ein.
services:
  caddy:
    image: %s
    restart: unless-stopped
    ports:
      - "%d:80"
      - "%d:443"
      - "%d:443/udp"
    extra_hosts:
      - "host.docker.internal:host-gateway"
    volumes:
      - %s/conf:/etc/caddy
      - %s/data:/data
      - %s/config:/config
`, ProxyImage, c.HTTPPort, c.HTTPSPort, c.HTTPSPort, proxyDir, proxyDir, proxyDir)
}
