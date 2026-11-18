package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const resetMarker = "factory-reset"

// StageReset merkt das Zurücksetzen auf Werkseinstellungen für den nächsten Start vor.
func StageReset(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, resetMarker), []byte(time.Now().UTC().Format(time.RFC3339)), 0o600)
}

// UnstageReset nimmt die Vormerkung zurück.
func UnstageReset(dir string) { os.Remove(filepath.Join(dir, resetMarker)) }

// ApplyFactoryReset setzt die Konfigurations-DB zurück, falls vorgemerkt (Aufruf vor dem Öffnen der DB).
// Die alte DB bleibt als <dbPath>.pre-reset-<zeit> liegen (neueste keep Stück); die neue DB legt db.Open an.
func ApplyFactoryReset(dir, dbPath string, keep int) (bool, error) {
	marker := filepath.Join(dir, resetMarker)
	if _, err := os.Stat(marker); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if _, err := os.Stat(dbPath); err == nil {
		saved := dbPath + ".pre-reset-" + time.Now().UTC().Format("20060102-150405")
		if err := os.Rename(dbPath, saved); err != nil {
			return false, err
		}
		for _, s := range []string{"-wal", "-shm"} {
			if err := os.Rename(dbPath+s, saved+s); err != nil && !errors.Is(err, os.ErrNotExist) {
				os.Remove(dbPath + s)
			}
		}
	}
	os.Remove(marker)
	all, _ := filepath.Glob(dbPath + ".pre-reset-*")
	var old []string
	for _, o := range all {
		if !strings.HasSuffix(o, "-wal") && !strings.HasSuffix(o, "-shm") {
			old = append(old, o)
		}
	}
	sort.Strings(old)
	for i := 0; i < len(old)-keep; i++ {
		os.Remove(old[i])
		os.Remove(old[i] + "-wal")
		os.Remove(old[i] + "-shm")
	}
	return true, nil
}

// FactoryReset merkt das Zurücksetzen vor und lässt die API über den Agent neu starten.
// Daten auf den Platten, Docker und Systembenutzer bleiben unberührt.
func (m *Manager) FactoryReset(ctx context.Context, actor string) error {
	if err := StageReset(m.restoreDir); err != nil {
		m.record(ctx, actor, "system.factory-reset", "config", err)
		return err
	}
	// Vor dem Neustart auditieren: danach ist die DB eine andere.
	m.record(ctx, actor, "system.factory-reset", "config", nil)
	if err := m.agent.Call(ctx, "system.restart-api", nil, nil); err != nil {
		UnstageReset(m.restoreDir)
		return err
	}
	return nil
}
