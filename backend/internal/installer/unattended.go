package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/betternas/betternas/internal/sysexec"
)

const (
	// ConfigLabel ist das Dateisystem-Label des Datenträgers mit answers.json.
	ConfigLabel = "BNASCFG"
	configMount = "/run/bnascfg"
)

// Find sucht einen Datenträger mit Label BNASCFG und liefert den Inhalt von answers.json.
func Find(ctx context.Context, r sysexec.Runner, fs sysexec.FS) ([]byte, bool, error) {
	res, err := r.Run(ctx, nil, "blkid", "-L", ConfigLabel)
	if err != nil {
		return nil, false, err
	}
	dev := strings.TrimSpace(string(res.Stdout))
	if res.ExitCode != 0 || dev == "" {
		return nil, false, nil
	}
	if err := fs.MkdirAll(configMount, 0o755); err != nil {
		return nil, false, err
	}
	if res, err := r.Run(ctx, nil, "mount", "-o", "ro", dev, configMount); err != nil || res.ExitCode != 0 {
		return nil, false, fmt.Errorf("%s konnte nicht eingehängt werden: %s", dev, strings.TrimSpace(string(res.Stderr)))
	}
	data, err := fs.ReadFile(configMount + "/answers.json")
	if err != nil {
		return nil, false, fmt.Errorf("answers.json auf %s nicht lesbar: %w", dev, err)
	}
	return data, true, nil
}

// LoadAnswers liest answers.json. Fehlende Felder erhalten Standardwerte, unbekannte Felder sind Fehler.
func LoadAnswers(data []byte) (Answers, error) {
	a := Defaults()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return Answers{}, fmt.Errorf("answers.json ungültig: %w", err)
	}
	return a, nil
}

// CheckUnattended prüft, ob eine unbeaufsichtigte Installation erlaubt ist.
func CheckUnattended(a Answers, targets []Target, tz func(string) bool) error {
	found := false
	for _, t := range targets {
		if t.Device == a.TargetDevice {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("zielgerät %q ist kein zulässiger Systemdatenträger", a.TargetDevice)
	}
	if a.ConfirmWipe != a.TargetDevice {
		return errors.New("confirmWipe muss exakt dem Zielgerät entsprechen")
	}
	if errs := a.Validate(tz); len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return errors.New(strings.Join(msgs, "; "))
	}
	return nil
}
