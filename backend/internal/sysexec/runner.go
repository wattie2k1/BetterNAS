// Package sysexec kapselt Systembefehle und Dateizugriffe, damit sie in Tests ersetzt werden können.
package sysexec

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

// Result ist das Ergebnis eines Befehls.
type Result struct {
	Stdout, Stderr []byte
	ExitCode       int
}

// Runner führt Befehle aus. err ist nur bei Startfehlern gesetzt, nicht bei ExitCode != 0.
type Runner interface {
	Run(ctx context.Context, stdin []byte, name string, args ...string) (Result, error)
}

type execRunner struct{}

// NewRunner liefert einen Runner, der echte Prozesse startet.
func NewRunner() Runner { return execRunner{} }

func (execRunner) Run(ctx context.Context, stdin []byte, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"LC_ALL=C", "PATH=" + defaultPath()}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	res := Result{Stdout: out.Bytes(), Stderr: errb.Bytes()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
		return res, nil
	}
	return res, err
}

func defaultPath() string {
	if p := os.Getenv("PATH"); p != "" {
		return p
	}
	return "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
}

// CommandLine formatiert einen Aufruf als "name arg1 arg2".
func CommandLine(name string, args ...string) string {
	return strings.Join(append([]string{name}, args...), " ")
}
