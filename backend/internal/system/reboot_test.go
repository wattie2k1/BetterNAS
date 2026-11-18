package system

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeRoot(t *testing.T, modules ...string) (modDir, flag string) {
	t.Helper()
	root := t.TempDir()
	modDir = filepath.Join(root, "modules")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, m := range modules {
		if err := os.MkdirAll(filepath.Join(modDir, m), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return modDir, filepath.Join(root, "reboot-required")
}

func TestRebootNotNeeded(t *testing.T) {
	mod, flag := fakeRoot(t, "6.12.43+deb13-amd64")
	c := RebootChecker{Running: func() (string, error) { return "6.12.43+deb13-amd64", nil }, ModulesDir: mod, FlagFile: flag}
	if st := c.Check(); st.Required {
		t.Fatalf("kein Neustart nötig, war aber %+v", st)
	}
}

func TestRebootNewerKernelInstalled(t *testing.T) {
	mod, flag := fakeRoot(t, "6.12.43+deb13-amd64", "6.12.48+deb13-amd64")
	c := RebootChecker{Running: func() (string, error) { return "6.12.43+deb13-amd64", nil }, ModulesDir: mod, FlagFile: flag}
	st := c.Check()
	if !st.Required || st.RunningKernel != "6.12.43+deb13-amd64" || st.InstalledKernel != "6.12.48+deb13-amd64" {
		t.Fatalf("Neustart wegen neuem Kernel erwartet: %+v", st)
	}
}

func TestRebootNumericVersionCompare(t *testing.T) {
	// 6.1.0-9 ist kleiner als 6.1.0-28 (numerisch, nicht lexikografisch).
	mod, flag := fakeRoot(t, "6.1.0-9-amd64", "6.1.0-28-amd64")
	c := RebootChecker{Running: func() (string, error) { return "6.1.0-28-amd64", nil }, ModulesDir: mod, FlagFile: flag}
	if st := c.Check(); st.Required {
		t.Fatalf("laufender Kernel ist der neueste: %+v", st)
	}
}

func TestRebootOlderOrOtherFlavorIgnored(t *testing.T) {
	mod, flag := fakeRoot(t, "6.12.43+deb13-amd64", "6.12.99+deb13-rt-amd64", "6.1.0-28-amd64")
	c := RebootChecker{Running: func() (string, error) { return "6.12.43+deb13-amd64", nil }, ModulesDir: mod, FlagFile: flag}
	if st := c.Check(); st.Required {
		t.Fatalf("andere Variante/älterer Kernel darf nichts melden: %+v", st)
	}
}

func TestRebootRunningModulesGone(t *testing.T) {
	mod, flag := fakeRoot(t, "6.12.48+deb13-amd64")
	c := RebootChecker{Running: func() (string, error) { return "6.12.43+deb13-amd64", nil }, ModulesDir: mod, FlagFile: flag}
	if st := c.Check(); !st.Required {
		t.Fatalf("Modulverzeichnis des laufenden Kernels fehlt → Neustart nötig: %+v", st)
	}
}

func TestRebootFlagFile(t *testing.T) {
	mod, flag := fakeRoot(t, "6.12.43+deb13-amd64")
	if err := os.WriteFile(flag, []byte("*** System restart required ***\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flag+".pkgs", []byte("libc6\nlinux-image-6.12\nlibc6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := RebootChecker{Running: func() (string, error) { return "6.12.43+deb13-amd64", nil }, ModulesDir: mod, FlagFile: flag}
	st := c.Check()
	if !st.Required || len(st.Packages) != 2 || st.Packages[0] != "libc6" {
		t.Fatalf("Flag-Datei mit Paketen erwartet: %+v", st)
	}
}

func TestRebootUnreadable(t *testing.T) {
	c := RebootChecker{Running: func() (string, error) { return "6.12.43", nil }, ModulesDir: "/nicht/vorhanden", FlagFile: "/nicht/vorhanden/flag"}
	if st := c.Check(); st.Required {
		t.Fatalf("ohne lesbare Quellen kein falscher Alarm: %+v", st)
	}
}
