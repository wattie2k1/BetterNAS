package sysexec

import (
	"context"
	"strings"
	"sync"
)

// FakeRunner zeichnet Aufrufe auf und liefert vorbereitete Antworten.
// Schlüssel in Responses ist "name arg1 arg2". Unbekannte Aufrufe liefern ExitCode 0.
// Ist ein Schlüssel nicht exakt vorhanden, wird der längste passende Präfix verwendet
// (z. B. "blkid -s UUID -o value" für alle Partitionen).
type FakeRunner struct {
	mu        sync.Mutex
	Calls     []string
	Stdins    [][]byte
	Responses map[string]Result
	// Hook wird, falls gesetzt, vor der Antwortsuche aufgerufen und kann eine Antwort liefern.
	Hook func(call string) (Result, bool)
}

func (f *FakeRunner) Run(_ context.Context, stdin []byte, name string, args ...string) (Result, error) {
	call := CommandLine(name, args...)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, call)
	f.Stdins = append(f.Stdins, stdin)
	if f.Hook != nil {
		if r, ok := f.Hook(call); ok {
			return r, nil
		}
	}
	if r, ok := f.Responses[call]; ok {
		return r, nil
	}
	best := ""
	for k := range f.Responses {
		if strings.HasPrefix(call, k) && len(k) > len(best) {
			best = k
		}
	}
	if best != "" {
		return f.Responses[best], nil
	}
	return Result{}, nil
}

// Called meldet, ob ein Aufruf mit diesem Präfix stattfand.
func (f *FakeRunner) Called(prefix string) bool {
	return f.Index(prefix) >= 0
}

// Index liefert die Position des ersten Aufrufs mit diesem Präfix oder -1.
func (f *FakeRunner) Index(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.Calls {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}
