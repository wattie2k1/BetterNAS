//go:build integration

// Integrationstest für SMART (Ruhezustand, -d sat) und Spindown auf echter Hardware.
// Braucht root, smartctl und hdparm; läuft nur mit mindestens einer echten, verwaltbaren Platte.
package agent

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/sysexec"
)

func TestSmartAndSpindown(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("braucht root")
	}
	for _, tool := range []string{"smartctl", "hdparm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " ist nicht installiert")
		}
	}
	ctx := context.Background()
	d := Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/")}
	disks, err := ListDisks(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, disk := range disks {
		if disk.System || !disk.Manageable || disk.Device == "" {
			continue
		}
		n++
		// SMART: Erfolg (auch Ruhezustand) oder ein verständlicher Fehler, nie ein Absturz.
		info, err := Smart(ctx, d, disk.Device)
		t.Logf("%s smart: %+v err=%v", disk.Device, info, err)
		// Spindown aus (-S 0) ist harmlos: entweder gesetzt oder als „nicht unterstützt“ (Konflikt) abgelehnt.
		if err := Spindown(ctx, d, disk.Device, 0); err != nil && apierr.From(err).Code != apierr.Conflict {
			t.Fatalf("%s: unerwarteter Fehler: %v", disk.Device, err)
		}
	}
	if n == 0 {
		t.Skip("keine verwaltbare Platte vorhanden")
	}
}
