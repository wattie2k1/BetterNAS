package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
	"github.com/betternas/betternas/internal/vm"
)

func configEnv(t *testing.T, state string) (Deps, *sysexec.FakeRunner, string) {
	t.Helper()
	d, f, root := vmEnv(t)
	xml, err := os.ReadFile("../vm/testdata/haos.xml")
	if err != nil {
		t.Fatal(err)
	}
	f.Responses[v+"dominfo homeassistant"] = sysexec.Result{Stdout: []byte("Name: homeassistant\nState: " + state + "\nAutostart: enable\n")}
	f.Responses[v+"dumpxml --inactive homeassistant"] = sysexec.Result{Stdout: xml}
	return d, f, root
}

func TestVMConfig(t *testing.T) {
	d, _, _ := configEnv(t, "shut off")
	c, err := VMConfig(context.Background(), d, "homeassistant")
	if err != nil || c.State != "shutoff" || !c.Autostart || !c.SecureBoot || c.VCPUs != 2 {
		t.Fatalf("%+v %v", c, err)
	}
	if code(func() error { _, err := VMConfig(context.Background(), d, "-x"); return err }()) != apierr.InvalidParams {
		t.Fatal("Name")
	}
	if code(func() error { _, err := VMConfig(context.Background(), d, "unbekannt"); return err }()) != apierr.NotFound {
		t.Fatal("unbekannt")
	}
}

func TestVMConfigure(t *testing.T) {
	d, f, root := configEnv(t, "shut off")
	nvram := "/var/lib/libvirt/qemu/nvram/homeassistant_VARS.fd"
	writeFile(t, root, nvram)
	writeFile(t, root, "/srv/vms/iso/a.iso")
	u := vm.ConfigUpdate{VCPUs: 4, MemoryMiB: 2048, SecureBoot: false, Network: "default", Media: "a.iso"}
	if err := VMConfigure(context.Background(), d, "homeassistant", u); err != nil {
		t.Fatal(err)
	}
	i := f.Index(v + "define /dev/stdin")
	if i < 0 {
		t.Fatal(f.Calls)
	}
	x := string(f.Stdins[i])
	for _, want := range []string{"<vcpu placement='static'>4</vcpu>", "name='secure-boot'/>", "/srv/vms/iso/a.iso"} {
		if !strings.Contains(x, want) {
			t.Errorf("%q fehlt:\n%s", want, x)
		}
	}
	if _, err := os.Stat(filepath.Join(root, nvram)); !os.IsNotExist(err) {
		t.Fatal("alte nvram-Datei muss entfernt sein")
	}
}

func TestVMConfigureRejects(t *testing.T) {
	d, f, _ := configEnv(t, "running")
	u := vm.ConfigUpdate{VCPUs: 2, MemoryMiB: 4096, SecureBoot: true, Network: "default"}
	if code(VMConfigure(context.Background(), d, "homeassistant", u)) != apierr.Conflict {
		t.Fatal("laufende VM")
	}
	d, f, _ = configEnv(t, "shut off")
	u.Media = "fehlt.iso"
	if code(VMConfigure(context.Background(), d, "homeassistant", u)) != apierr.NotFound {
		t.Fatal("ISO fehlt")
	}
	u.Media, u.Network = "", "gibtsnicht"
	if code(VMConfigure(context.Background(), d, "homeassistant", u)) != apierr.InvalidParams {
		t.Fatal("Netz fehlt")
	}
	if f.Called(v + "define") {
		t.Fatal("nichts darf definiert werden")
	}
}
