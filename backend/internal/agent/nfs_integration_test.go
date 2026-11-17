//go:build integration

// Integrationstest für NFS-Exporte mit dem echten exportfs: Die von BetterNAS erzeugte Exportzeile
// (Optionssyntax, fsid, Squash) muss exportfs akzeptieren. Es wird nichts gemountet.
package agent

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/shares"
	"github.com/betternas/betternas/internal/sysexec"
)

func TestNfsExports(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	if _, err := exec.LookPath("exportfs"); err != nil {
		t.Skip("exportfs ist nicht installiert (nfs-kernel-server)")
	}
	if _, err := os.Stat(exportsFile); err == nil {
		t.Skipf("übersprungen: %s existiert bereits", exportsFile)
	}
	dir, err := os.MkdirTemp("/var/tmp", "bnas-nfs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Remove(exportsFile)
		_ = exec.Command("exportfs", "-ra").Run()
		os.RemoveAll(dir)
	})
	if err := os.MkdirAll(exportsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	st := shares.State{Shares: []shares.Share{
		{Name: "Itnfs", Pool: "itnfs", Subdir: "Test", Guest: true, NFS: shares.NFS{Enabled: true, Clients: []string{"127.0.0.1", "192.0.2.0/24"}, Squash: shares.SquashAll, ReadOnly: true}},
		{Name: "Itnfs2", Pool: "itnfs", Subdir: "Zwei", Guest: true, NFS: shares.NFS{Enabled: true, Clients: []string{"localhost"}, Squash: shares.SquashRoot}},
	}}
	if err := st.Validate(); err != nil {
		t.Fatal(err)
	}
	content := string(shares.RenderExports(st))
	content = strings.ReplaceAll(content, "/srv/pools/itnfs/Test", dir)
	two := dir + "/zwei"
	if err := os.MkdirAll(two, 0o755); err != nil {
		t.Fatal(err)
	}
	content = strings.ReplaceAll(content, "/srv/pools/itnfs/Zwei", two)
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/")}
	if err := d.FS.WriteFile(exportsFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, err := d.Runner.Run(context.Background(), nil, "exportfs", "-ra"); err != nil || res.ExitCode != 0 {
		t.Fatalf("exportfs -ra: %v %d %s\n%s", err, res.ExitCode, res.Stderr, content)
	}
	// Die Erkennung der NFS-Optionen läuft gegen die echten mergerfs-Prozesse (Pools vor 0.5.0-rc3: ohne noforget).
	if pools, _ := os.ReadDir("/srv/pools"); len(pools) > 0 {
		for _, p := range pools {
			ok, err := poolHasNFSOptions(context.Background(), d, p.Name())
			if err != nil {
				t.Fatalf("poolHasNFSOptions(%s): %v", p.Name(), err)
			}
			t.Logf("Pool %s: noforget aktiv = %v", p.Name(), ok)
		}
	}
	res, err := d.Runner.Run(context.Background(), nil, "exportfs", "-v")
	if err != nil || !strings.Contains(string(res.Stdout), dir) || !strings.Contains(string(res.Stdout), "all_squash") {
		t.Fatalf("exportfs -v: %v\n%s", err, res.Stdout)
	}
}
