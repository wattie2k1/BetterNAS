package firewall

import (
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
)

func TestNormalize(t *testing.T) {
	c := Config{Enabled: true, Rules: map[string]Rule{"smb": {Mode: ModeCustom, Networks: []string{"192.168.1.5", "10.0.0.7/8", "192.168.1.5/32", "2001:db8::1"}}}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	got := c.Rules["smb"].Networks
	if len(got) != 3 || got[0] != "192.168.1.5/32" || got[1] != "10.0.0.0/8" || got[2] != "2001:db8::1/128" {
		t.Fatalf("%v", got)
	}
	if c.Rules["web"].Mode != ModeAll {
		t.Fatal("fehlende Dienste müssen ergänzt werden")
	}
	for _, bad := range []Config{
		{Rules: map[string]Rule{"x": {Mode: ModeAll}}},
		{Rules: map[string]Rule{"smb": {Mode: "irgendwas"}}},
		{Rules: map[string]Rule{"smb": {Mode: ModeCustom}}},
		{Rules: map[string]Rule{"smb": {Mode: ModeCustom, Networks: []string{"1.2.3.4; drop"}}}},
	} {
		if apierr.From(bad.Normalize()).Code != apierr.InvalidParams {
			t.Errorf("%+v muss abgelehnt werden", bad)
		}
	}
}

func TestAllowsAndLockout(t *testing.T) {
	lan := Rule{Mode: ModeLAN}
	for ip, want := range map[string]bool{"192.168.1.20": true, "10.1.2.3": true, "fe80::1": true, "8.8.8.8": false, "2a00::1": false, "::ffff:192.168.1.2": true, "müll": false} {
		if lan.Allows(ip) != want {
			t.Errorf("lan %s: %v", ip, !want)
		}
	}
	c := Default()
	c.Enabled = true
	c.Rules["web"] = Rule{Mode: ModeCustom, Networks: []string{"10.0.0.0/24"}}
	if err := c.CheckLockout("192.168.1.20"); apierr.From(err).Code != apierr.Conflict {
		t.Fatal("eigene Adresse gesperrt muss abgelehnt werden:", err)
	}
	if err := c.CheckLockout("10.0.0.9"); err != nil {
		t.Fatal(err)
	}
	c.Enabled = false
	if err := c.CheckLockout("192.168.1.20"); err != nil {
		t.Fatal("ausgeschaltete Firewall sperrt nicht")
	}
}

func TestRender(t *testing.T) {
	off := Render(Default(), 22)
	if strings.Contains(off, "chain") || !strings.Contains(off, "delete table inet bnas_fw") {
		t.Fatal(off)
	}
	c := Default()
	c.Enabled = true
	c.Rules["smb"] = Rule{Mode: ModeLAN}
	c.Rules["ssh"] = Rule{Mode: ModeCustom, Networks: []string{"192.168.1.0/24", "2001:db8::/32"}}
	out := Render(c, 2222)
	for _, want := range []string{
		"policy drop", "ct state established,related accept", "iif lo accept",
		"tcp dport { 80, 443 } accept",
		"ip saddr { 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16 } tcp dport { 139, 445 } accept",
		"ip6 saddr { fc00::/7, fe80::/10 } udp dport { 137, 138 } accept",
		"ip saddr { 192.168.1.0/24 } tcp dport { 2222 } accept",
		"ip6 saddr { 2001:db8::/32 } tcp dport { 2222 } accept",
		"udp dport { 5353 } accept",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("fehlt: %s\n%s", want, out)
		}
	}
	if strings.Contains(out, "dport { 22 }") {
		t.Fatal("SSH-Port muss der konfigurierte sein")
	}
}

func TestRollbackScript(t *testing.T) {
	if s := RollbackScript(true); !strings.Contains(s, "cp "+RulesFile+BackupSuffix) || !strings.Contains(s, "nft -f "+RulesFile) {
		t.Fatal(s)
	}
	if s := RollbackScript(false); !strings.Contains(s, "delete table inet bnas_fw") || strings.Contains(s, "cp ") {
		t.Fatal(s)
	}
}
