// Command betternas-agent führt privilegierte Operationen für die BetterNAS-API aus (läuft als root).
package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

	"github.com/betternas/betternas/internal/agent"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/sysexec"
	"github.com/betternas/betternas/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "snapraid-run" {
		d := agent.Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/")}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		code := agent.SnapraidCLI(ctx, d, os.Args[2:], os.Stderr)
		stop()
		os.Exit(code)
	}
	socket := flag.String("socket", "/run/betternas/agent.sock", "Pfad des Unix-Sockets")
	group := flag.String("socket-group", "betternas", "Gruppe, die den Socket nutzen darf")
	apiUser := flag.String("api-user", "betternas", "Systembenutzer der API (darf verbinden)")
	allowLoop := flag.Bool("allow-loop", false, "Loop-Geräte als Datenträger zulassen (nur Tests)")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(log)

	allowed := []uint32{0}
	if u, err := user.Lookup(*apiUser); err == nil {
		uid, _ := strconv.ParseUint(u.Uid, 10, 32)
		allowed = append(allowed, uint32(uid))
	} else {
		log.Warn("API-Benutzer unbekannt, nur root darf verbinden", "user", *apiUser)
	}

	_ = os.Remove(*socket)
	l, err := net.Listen("unix", *socket)
	if err != nil {
		log.Error("socket", "err", err)
		os.Exit(1)
	}
	if g, err := user.LookupGroup(*group); err == nil {
		gid, _ := strconv.Atoi(g.Gid)
		if err := os.Chown(*socket, 0, gid); err != nil {
			log.Warn("chown socket", "err", err)
		}
	}
	if err := os.Chmod(*socket, 0o660); err != nil {
		log.Error("chmod socket", "err", err)
		os.Exit(1)
	}

	s := rpc.NewServer(allowed)
	agent.Register(s, agent.Deps{Runner: sysexec.NewRunner(), FS: sysexec.NewRootFS("/"), AllowLoop: *allowLoop, Udev: agent.KernelUevents{}})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	log.Info("betternas-agent gestartet", "version", version.Version, "socket", *socket)
	if err := s.Serve(l); err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
