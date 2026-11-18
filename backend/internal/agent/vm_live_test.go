package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
	"github.com/betternas/betternas/internal/vm"
)

// liveEnv: laufende VM mit Obergrenzen (RAM 4096 aktuell/8192 max, vCPUs 2 aktuell/4 max), CD-Laufwerk sdb.
func liveEnv(t *testing.T, state string) (Deps, *sysexec.FakeRunner, string) {
	t.Helper()
	d, f, root := configEnv(t, state)
	x := string(mustRead(t, "../vm/testdata/haos.xml"))
	x = strings.Replace(x, "<memory unit='KiB'>4194304</memory>", "<memory unit='KiB'>8388608</memory>", 1)
	x = strings.Replace(x, "<vcpu placement='static'>2</vcpu>", "<vcpu placement='static' current='2'>4</vcpu>", 1)
	x = strings.Replace(x, "  </devices>", "    <disk type='file' device='cdrom'>\n      <driver name='qemu' type='raw'/>\n      <target dev='sdb' bus='sata'/>\n      <readonly/>\n    </disk>\n  </devices>", 1)
	f.Responses[v+"dumpxml --inactive homeassistant"] = sysexec.Result{Stdout: []byte(x)}
	f.Responses[v+"dumpxml homeassistant"] = sysexec.Result{Stdout: []byte(x)}
	writeFile(t, root, "/srv/vms/iso/a.iso")
	return d, f, root
}

func ip(i int) *int { return &i }

func TestVMLiveMemoryCPUMedia(t *testing.T) {
	d, f, _ := liveEnv(t, "running")
	media := "a.iso"
	u := vm.LiveUpdate{MemoryMiB: ip(6144), VCPUs: ip(4), Media: &media}
	if err := VMLive(context.Background(), d, "homeassistant", u); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		v + "setmem homeassistant 6291456 --live --config",
		v + "setvcpus homeassistant 4 --live --config",
		v + "change-media homeassistant sdb /srv/vms/iso/a.iso --update --live --config",
	} {
		if !f.Called(want) {
			t.Errorf("%q fehlt: %v", want, f.Calls)
		}
	}
	empty := ""
	if err := VMLive(context.Background(), d, "homeassistant", vm.LiveUpdate{Media: &empty}); err != nil {
		t.Fatal(err)
	}
	// Ohne eingelegtes Medium ist das Auswerfen ein No-op, das Medium ist laut Konfiguration schon leer.
	if f.Called(v + "change-media homeassistant sdb --eject") {
		t.Fatal("kein Auswerfen bei leerem Laufwerk")
	}
}

func TestVMLiveLimitsAndState(t *testing.T) {
	ctx := context.Background()
	d, f, _ := liveEnv(t, "running")
	if code(VMLive(ctx, d, "homeassistant", vm.LiveUpdate{MemoryMiB: ip(9000)})) != apierr.InvalidParams {
		t.Fatal("über Maximum RAM")
	}
	if code(VMLive(ctx, d, "homeassistant", vm.LiveUpdate{VCPUs: ip(8)})) != apierr.InvalidParams {
		t.Fatal("über Maximum vCPUs")
	}
	media := "fehlt.iso"
	if code(VMLive(ctx, d, "homeassistant", vm.LiveUpdate{VCPUs: ip(3), Media: &media})) != apierr.NotFound {
		t.Fatal("ISO fehlt")
	}
	if f.Called(v+"setvcpus") || f.Called(v+"setmem") {
		t.Fatal("bei Ablehnung darf nichts halb geändert werden")
	}
	d, _, _ = liveEnv(t, "shut off")
	if code(VMLive(ctx, d, "homeassistant", vm.LiveUpdate{MemoryMiB: ip(2048)})) != apierr.Conflict {
		t.Fatal("ausgeschaltet")
	}
	if code(VMLive(ctx, d, "-x", vm.LiveUpdate{MemoryMiB: ip(2048)})) != apierr.InvalidParams {
		t.Fatal("Name")
	}
	if err := VMLive(ctx, d, "homeassistant", vm.LiveUpdate{}); err != nil {
		t.Fatal("leeres Update ist ein No-op")
	}
}

func TestVMLiveUSB(t *testing.T) {
	d, f, _ := liveEnv(t, "running")
	a := vm.USBDevice{Vendor: "1a86", Product: "7523"}
	if err := VMLive(context.Background(), d, "homeassistant", vm.LiveUpdate{USB: &[]vm.USBDevice{a}}); err != nil {
		t.Fatal(err)
	}
	i := f.Index(v + "attach-device homeassistant /dev/stdin --live --config")
	if i < 0 || !strings.Contains(string(f.Stdins[i]), "<vendor id='0x1a86'/>") {
		t.Fatalf("%v", f.Calls)
	}
	// Persistente Konfiguration enthält das Gerät schon → nur das fehlende wird zugewiesen, das überzählige entzogen.
	x := f.Responses[v+"dumpxml --inactive homeassistant"].Stdout
	x2, _, _ := vm.ApplyConfig(string(x), vm.ConfigUpdate{VCPUs: 2, MemoryMiB: 4096, MaxMemoryMiB: 8192, MaxVCPUs: 4, SecureBoot: true, Network: "default", USB: &[]vm.USBDevice{a}})
	f.Responses[v+"dumpxml --inactive homeassistant"] = sysexec.Result{Stdout: []byte(x2)}
	b := vm.USBDevice{Vendor: "10c4", Product: "ea60"}
	f.Calls, f.Stdins = nil, nil
	if err := VMLive(context.Background(), d, "homeassistant", vm.LiveUpdate{USB: &[]vm.USBDevice{b}}); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f, v+"detach-device homeassistant /dev/stdin --live --config", v+"attach-device homeassistant /dev/stdin --live --config")
	// Fehler beim Zuweisen wird mit Hinweis gemeldet.
	f.Responses[v+"attach-device"] = sysexec.Result{ExitCode: 1, Stderr: []byte("error: device not found")}
	err := VMLive(context.Background(), d, "homeassistant", vm.LiveUpdate{USB: &[]vm.USBDevice{a, b}})
	if code(err) != apierr.Internal || !strings.Contains(err.Error(), "angeschlossen") {
		t.Fatalf("%v", err)
	}
}

func TestVMLiveNoCDROM(t *testing.T) {
	d, f, _ := liveEnv(t, "running")
	x := strings.Replace(string(f.Responses[v+"dumpxml --inactive homeassistant"].Stdout), "device='cdrom'", "device='disk'", 1)
	f.Responses[v+"dumpxml --inactive homeassistant"] = sysexec.Result{Stdout: []byte(x)}
	media := "a.iso"
	if code(VMLive(context.Background(), d, "homeassistant", vm.LiveUpdate{Media: &media})) != apierr.Conflict {
		t.Fatal("kein CD-Laufwerk")
	}
}
