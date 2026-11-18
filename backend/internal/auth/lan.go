package auth

import (
	"net"
	"net/http"
	"net/netip"

	"github.com/betternas/betternas/internal/apierr"
)

var privateNets = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
}

// ClientIP liefert die Adresse des direkten Gegenübers (Proxy-Header werden bewusst ignoriert).
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// localPrefixes liefert die Subnetze der Schnittstellen des NAS (ohne Loopback). Variable, damit Tests sie ersetzen können.
var localPrefixes = func() []netip.Prefix {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Prefix
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			if p, ok := netip.AddrFromSlice(n.IP); ok {
				ones, _ := n.Mask.Size()
				out = append(out, netip.PrefixFrom(p.Unmap(), ones).Masked())
			}
		}
	}
	return out
}

// isPrivate: private Netze oder dasselbe Subnetz wie eine Schnittstelle des NAS. Letzteres deckt
// Clients mit globaler IPv6-Adresse (Provider-Präfix) im eigenen LAN ab, die über betternas.local kommen.
func isPrivate(ip string) bool {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	a = a.Unmap().WithZone("")
	for _, p := range privateNets {
		if p.Contains(a) {
			return true
		}
	}
	for _, p := range localPrefixes() {
		if p.Bits() > 0 && p.Contains(a) {
			return true
		}
	}
	return false
}

// LANOnly lässt nur Anfragen aus privaten Netzen durch.
func LANOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isPrivate(ClientIP(r)) {
			apierr.Write(w, apierr.New(apierr.Forbidden, "Zugriff nur aus dem lokalen Netzwerk erlaubt"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
