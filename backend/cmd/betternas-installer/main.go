// Command betternas-installer installiert BetterNAS aus dem Live-System auf die Systemplatte.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/betternas/betternas/internal/installer"
	"github.com/betternas/betternas/internal/installer/tui"
	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/sysexec"
	"github.com/betternas/betternas/internal/version"
)

func main() {
	squashfs := flag.String("squashfs", "/run/live/medium/betternas/rootfs.squashfs", "Root-Dateisystem-Image")
	target := flag.String("target", "/target", "Mountpoint für das Zielsystem")
	logPath := flag.String("log", "/tmp/betternas-install.log", "Protokolldatei")
	flag.Parse()

	logf, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer logf.Close()
	fmt.Fprintf(logf, "=== BetterNAS-Installer %s gestartet %s ===\n", version.Version, time.Now().Format(time.RFC3339))

	ctx := context.Background()
	runner := &loggingRunner{next: sysexec.NewRunner(), log: logf}
	fs := sysexec.NewRootFS("/")
	_, efiErr := os.Stat("/sys/firmware/efi")
	env := installer.Env{
		Runner: runner, FS: fs, Squashfs: *squashfs, Target: *target,
		UEFI: efiErr == nil, Arch: runtime.GOARCH, Version: version.Version,
	}

	targets, err := discoverTargets(ctx, runner)
	if err != nil {
		fmt.Fprintf(logf, "Datenträgererkennung fehlgeschlagen: %v\n", err)
	}

	if data, found, err := installer.Find(ctx, runner, fs); err != nil || found {
		os.Exit(unattended(ctx, env, targets, data, err, logf))
	}

	m := tui.New(tui.Deps{
		Targets:     targets,
		Interfaces:  interfaces(),
		ApplyKeymap: func(k string) error { return runQuiet(ctx, runner, "loadkeys", k) },
		Timezones:   timezoneExists,
		Install: func(ctx context.Context, l installer.Layout, a installer.Answers, p installer.Progress) error {
			err := installer.Execute(ctx, env, l, a, p)
			if err != nil {
				fmt.Fprintf(logf, "FEHLER: %v\n", err)
			}
			return err
		},
		Reboot:  func() error { return runQuiet(ctx, runner, "systemctl", "reboot") },
		LogPath: *logPath,
	})
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func discoverTargets(ctx context.Context, r sysexec.Runner) ([]installer.Target, error) {
	res, err := r.Run(ctx, nil, "lsblk", "--json", "--bytes", "--tree", "-o", storage.LsblkColumns)
	if err != nil {
		return nil, err
	}
	disks, err := storage.ParseLsblk(res.Stdout, storage.IDOptions{})
	if err != nil {
		return nil, err
	}
	mi, _ := os.ReadFile("/proc/self/mountinfo")
	return installer.Targets(disks, installer.LiveMediumDevice(mi, disks)), nil
}

// interfaces liefert physische Netzwerkschnittstellen (mit Gerät), sortiert.
func interfaces() []string {
	entries, _ := os.ReadDir("/sys/class/net")
	var out []string
	for _, e := range entries {
		if e.Name() == "lo" {
			continue
		}
		if _, err := os.Stat(filepath.Join("/sys/class/net", e.Name(), "device")); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func timezoneExists(tz string) bool {
	if tz == "" || strings.Contains(tz, "..") || strings.HasPrefix(tz, "/") {
		return false
	}
	st, err := os.Stat(filepath.Join("/usr/share/zoneinfo", tz))
	return err == nil && !st.IsDir()
}

func runQuiet(ctx context.Context, r sysexec.Runner, name string, args ...string) error {
	res, err := r.Run(ctx, nil, name, args...)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%s fehlgeschlagen: %s", name, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

// unattended führt die Installation ohne Rückfragen aus und liefert den Exit-Code.
func unattended(ctx context.Context, env installer.Env, targets []installer.Target, data []byte, findErr error, logf io.Writer) int {
	out := io.MultiWriter(os.Stdout, logf)
	if serial, err := os.OpenFile("/dev/ttyS0", os.O_WRONLY, 0); err == nil {
		defer serial.Close()
		out = io.MultiWriter(os.Stdout, logf, serial)
	}
	fail := func(err error) int {
		fmt.Fprintf(out, "[BetterNAS] Installation fehlgeschlagen: %v\n", err)
		return 1
	}
	if findErr != nil {
		return fail(findErr)
	}
	a, err := installer.LoadAnswers(data)
	if err != nil {
		return fail(err)
	}
	if err := installer.CheckUnattended(a, targets, timezoneExists); err != nil {
		return fail(err)
	}
	var target installer.Target
	for _, t := range targets {
		if t.Device == a.TargetDevice {
			target = t
		}
	}
	l, err := installer.PlanLayout(target, a)
	if err != nil {
		return fail(err)
	}
	fmt.Fprintf(out, "[BetterNAS] Unbeaufsichtigte Installation auf %s\n", a.TargetDevice)
	err = installer.Execute(ctx, env, l, a, func(s string, p int) {
		fmt.Fprintf(out, "[BetterNAS] %s %d%%\n", s, p)
	})
	if err != nil {
		return fail(err)
	}
	fmt.Fprintln(out, "[BetterNAS] Installation abgeschlossen")
	_ = runQuiet(ctx, env.Runner, "systemctl", "reboot")
	return 0
}

// loggingRunner protokolliert Befehle und Fehlerausgaben – nie die Standardeingabe (Passwörter).
type loggingRunner struct {
	next sysexec.Runner
	log  io.Writer
}

func (l *loggingRunner) Run(ctx context.Context, stdin []byte, name string, args ...string) (sysexec.Result, error) {
	start := time.Now()
	res, err := l.next.Run(ctx, stdin, name, args...)
	fmt.Fprintf(l.log, "$ %s  (exit %d, %s)\n", sysexec.CommandLine(name, args...), res.ExitCode, time.Since(start).Round(time.Millisecond))
	if res.ExitCode != 0 && len(res.Stderr) > 0 {
		fmt.Fprintf(l.log, "%s\n", strings.TrimSpace(string(res.Stderr)))
	}
	if err != nil {
		fmt.Fprintf(l.log, "start fehlgeschlagen: %v\n", err)
	}
	return res, err
}
