package netcfg

import (
	"reflect"
	"strings"
	"testing"
)

func valid() IPConfig {
	return IPConfig{Mode: ModeStatic, Address: "192.168.1.10/24", Gateway: "192.168.1.1", DNS: []string{"192.168.1.1", "1.1.1.1"}, Domains: []string{"Fritz.Box"}}
}

func TestIPValidate(t *testing.T) {
	c := valid()
	if err := c.Validate(); err != nil || c.Domains[0] != "fritz.box" {
		t.Fatal(err, c)
	}
	bad := map[string]func(*IPConfig){
		"modus":        func(c *IPConfig) { c.Mode = "x" },
		"v6":           func(c *IPConfig) { c.Address = "fd00::1/64" },
		"ohne maske":   func(c *IPConfig) { c.Address = "192.168.1.10" },
		"/31":          func(c *IPConfig) { c.Address = "192.168.1.10/31" },
		"netzadresse":  func(c *IPConfig) { c.Address = "192.168.1.0/24" },
		"broadcast":    func(c *IPConfig) { c.Address = "192.168.1.255/24" },
		"loopback":     func(c *IPConfig) { c.Address = "127.0.0.1/8" },
		"gw fremd":     func(c *IPConfig) { c.Gateway = "10.0.0.1" },
		"gw = adresse": func(c *IPConfig) { c.Gateway = "192.168.1.10" },
		"gw leer":      func(c *IPConfig) { c.Gateway = "" },
		"kein dns":     func(c *IPConfig) { c.DNS = nil },
		"dns kaputt":   func(c *IPConfig) { c.DNS = []string{"abc"} },
		"4 dns":        func(c *IPConfig) { c.DNS = []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4"} },
		"domain":       func(c *IPConfig) { c.Domains = []string{"-x.de"} },
		"domain inj":   func(c *IPConfig) { c.Domains = []string{"a.de\n[Network]"} },
	}
	for name, mut := range bad {
		c := valid()
		mut(&c)
		if c.Validate() == nil {
			t.Errorf("%s: Fehler erwartet", name)
		}
	}
	d := IPConfig{Mode: ModeDHCP, Address: "müll"}
	if err := d.Validate(); err != nil || d.Address != "" {
		t.Fatal("DHCP ignoriert die Felder", err, d)
	}
}

func TestIPDropinRoundtrip(t *testing.T) {
	c := valid()
	_ = c.Validate()
	out := RenderIPDropin(c)
	if !strings.Contains(out, "DHCP=no") || !strings.Contains(out, ManagedHeader) {
		t.Fatal(out)
	}
	if got := ParseIPDropin(out); !reflect.DeepEqual(got, c) {
		t.Fatalf("%+v != %+v", got, c)
	}
}

func TestIPTarget(t *testing.T) {
	main := ManagedHeader + "\n[Match]\nName=eno1\n\n[Network]\nDHCP=yes\n"
	if f, i := IPTarget(false, main); f != MainNetworkFile || i != "eno1" {
		t.Fatal(f, i)
	}
	if f, i := IPTarget(true, main); f != BridgeFile || i != "br0" {
		t.Fatal(f, i)
	}
	if DropinPath(BridgeFile) != "/etc/systemd/network/05-bnas-br0.network.d/10-bnas-ip.conf" {
		t.Fatal(DropinPath(BridgeFile))
	}
}

func TestParsers(t *testing.T) {
	ifs := ParseIfaces([]byte(`[{"ifname":"lo","operstate":"UNKNOWN","address":"00:00:00:00:00:00","addr_info":[{"family":"inet","local":"127.0.0.1","prefixlen":8}]},
	{"ifname":"br0","operstate":"UP","address":"d8:cb","addr_info":[{"family":"inet","local":"192.168.1.65","prefixlen":24},{"family":"inet6","local":"fe80::1","prefixlen":64}]},
	{"ifname":"docker0","operstate":"DOWN","addr_info":[]},{"ifname":"vnet28","operstate":"UNKNOWN","addr_info":[]}]`))
	if len(ifs) != 1 || ifs[0].Name != "br0" || ifs[0].State != "up" || !reflect.DeepEqual(ifs[0].Addresses, []string{"192.168.1.65/24"}) {
		t.Fatalf("%+v", ifs)
	}
	if ParseDefaultGateway([]byte(`[{"dst":"default","gateway":"192.168.1.1"}]`)) != "192.168.1.1" || ParseDefaultGateway([]byte(`oops`)) != "" {
		t.Fatal("Gateway")
	}
	if got := ParseResolvectl([]byte("Link 107 (br0): 192.168.1.1 fd34::1\n")); !reflect.DeepEqual(got, []string{"192.168.1.1", "fd34::1"}) {
		t.Fatal(got)
	}
	if got := ParseResolvectl([]byte("Link 2 (eno1):\n")); len(got) != 0 {
		t.Fatal(got)
	}
}
