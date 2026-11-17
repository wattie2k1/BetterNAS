package shares

import (
	"strconv"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
)

func TestValidClient(t *testing.T) {
	ok := []string{"192.168.1.10", "192.168.1.0/24", "fe80::1", "2001:db8::/32", "nas.local", "host-1.example.org", "a"}
	bad := []string{"*", "", "a b", "10.0.0.1(rw)", "a,b", "-x.de", "x-.de", "999.1.1.1/40", "999.1.1.1", strings.Repeat("a", 254),
		"host_name", "a..b", "10.0.0.1(rw,no_root_squash)", "10.0.0.1\n/etc", "/etc", "a/24", ".local"}
	for _, c := range ok {
		if !ValidClient(c) {
			t.Errorf("%q abgelehnt", c)
		}
	}
	for _, c := range bad {
		if ValidClient(c) {
			t.Errorf("%q akzeptiert", c)
		}
	}
}

func TestNFSValidate(t *testing.T) {
	good := NFS{Enabled: true, Clients: []string{"192.168.1.0/24", "nas.local"}, Squash: "root"}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, sq := range []string{"", "root", "all"} {
		g := good
		g.Squash = sq
		if err := g.Validate(); err != nil {
			t.Errorf("Squash %q: %v", sq, err)
		}
	}
	cases := map[string]NFS{
		"keine Clients":    {Enabled: true, Squash: "root"},
		"Wildcard":         {Enabled: true, Clients: []string{"*"}, Squash: "root"},
		"Optionen":         {Enabled: true, Clients: []string{"10.0.0.1(rw)"}, Squash: "root"},
		"Squash unbekannt": {Enabled: true, Clients: []string{"nas.local"}, Squash: "none"},
		"33 Clients":       {Enabled: true, Clients: manyClients(33), Squash: "root"},
	}
	for name, n := range cases {
		if err := n.Validate(); err == nil || apierr.From(err).Code != apierr.InvalidParams {
			t.Errorf("%s: erwartet invalid_params, bekam %v", name, err)
		}
	}
	// deaktiviert: Inhalt wird nicht geprüft
	if err := (NFS{Enabled: false, Clients: []string{"*"}, Squash: "x"}).Validate(); err != nil {
		t.Errorf("deaktiviert: %v", err)
	}
}

func manyClients(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "10.0.0." + strconv.Itoa(i+1)
	}
	return out
}

func TestFSIDStable(t *testing.T) {
	if got := FSID("media"); got != 210708 {
		t.Fatalf("%d", got)
	}
	if FSID("Media") != FSID("media") {
		t.Error("Groß-/Kleinschreibung muss gleich sein")
	}
	if id := FSID("x"); id < 1000 || id >= 901000 {
		t.Errorf("außerhalb des Bereichs: %d", id)
	}
}

func TestStateRejectsFsidCollision(t *testing.T) {
	seen := map[int]string{}
	var a, b string
	for i := 0; i < 50000 && b == ""; i++ {
		n := "s" + strconv.Itoa(i)
		if prev, dup := seen[FSID(n)]; dup {
			a, b = prev, n
		}
		seen[FSID(n)] = n
	}
	if b == "" {
		t.Skip("keine Kollision gefunden")
	}
	nfs := NFS{Enabled: true, Clients: []string{"192.168.1.0/24"}, Squash: "root"}
	mk := func(name string, enabled bool) Share {
		n := nfs
		n.Enabled = enabled
		return Share{Name: name, Pool: "media", Subdir: name, Browsable: true, ACL: []ACL{{"user", "anna", "rw"}}, NFS: n}
	}
	err := State{Shares: []Share{mk(a, true), mk(b, true)}}.Validate()
	if err == nil || apierr.From(err).Code != apierr.InvalidParams || !strings.Contains(err.Error(), "NFS") {
		t.Fatalf("Kollision %s/%s nicht erkannt: %v", a, b, err)
	}
	if err := (State{Shares: []Share{mk(a, true), mk(b, false)}}).Validate(); err != nil {
		t.Fatalf("nur eine NFS-Freigabe aktiv darf nicht kollidieren: %v", err)
	}
}
