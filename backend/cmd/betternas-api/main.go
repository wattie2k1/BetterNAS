// Command betternas-api stellt Weboberfläche und API bereit (läuft unprivilegiert).
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/betternas/betternas/internal/api"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/backup"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/monitor"
	"github.com/betternas/betternas/internal/notify"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/shares"
	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/system"
	"github.com/betternas/betternas/internal/users"
	"github.com/betternas/betternas/internal/version"
	"github.com/betternas/betternas/internal/vm"
)

func main() {
	cfg := api.Config{}
	flag.StringVar(&cfg.Listen, "listen", ":443", "Adresse für HTTPS")
	flag.StringVar(&cfg.DBPath, "db", "/var/lib/betternas/betternas.db", "SQLite-Datenbank")
	flag.StringVar(&cfg.AgentSocket, "agent-socket", "/run/betternas/agent.sock", "Socket des Agents")
	flag.StringVar(&cfg.TLSDir, "tls-dir", "/etc/betternas/tls", "Verzeichnis für Zertifikat und Schlüssel")
	flag.StringVar(&cfg.WebDir, "web-dir", "/usr/share/betternas/web", "Verzeichnis der Weboberfläche")
	flag.BoolVar(&cfg.Dev, "dev", false, "Entwicklungsmodus: HTTP auf :8080, Cookie ohne Secure")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(log)

	// Eine vorgemerkte Wiederherstellung wird vor dem Öffnen der DB eingesetzt.
	stateDir := filepath.Dir(cfg.DBPath)
	if applied, err := backup.ApplyPending(filepath.Join(stateDir, "restore"), cfg.DBPath, 3); err != nil {
		log.Error("wiederherstellung einsetzen", "err", err)
	} else if applied {
		log.Info("wiederherstellung eingesetzt – alle Sitzungen sind ungültig")
	}

	d, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Error("datenbank", "err", err)
		os.Exit(1)
	}
	defer d.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hub := events.NewHub()
	jm := jobs.NewManager(d, hub)
	if err := jm.MarkInterrupted(ctx); err != nil {
		log.Warn("jobs", "err", err)
	}
	agent := rpc.NewClient(cfg.AgentSocket)
	go api.ForwardAgentEvents(ctx, agent, hub)
	totpStore := auth.NewTOTPStore(d)
	handlers := auth.NewHandlers(d, auth.NewStore(d, 12*time.Hour), auth.AgentVerifier{Agent: agent}, auth.SystemGroupChecker, !cfg.Dev).WithTOTP(totpStore)
	// Freigaben-Manager und Storage/Benutzer verweisen aufeinander; die Funktionen lesen shm erst bei Aufruf.
	var shm *shares.Manager
	sm := storage.NewManager(d, agent, jm, hub, func(ctx context.Context, pool string) ([]string, error) {
		return shm.PoolDependents(ctx, pool)
	})
	um := users.NewManager(d, agent, hub, func(ctx context.Context, kind, name string) ([]string, error) {
		return shm.PrincipalUsage(ctx, kind, name)
	})
	shm = shares.NewManager(d, agent, jm, hub)
	// Beim Start den gespeicherten Zustand anwenden. Agent und Pools stehen evtl. erst nach einigen Sekunden bereit.
	go func() {
		// Reihenfolge ist wichtig: Freigaben brauchen eingehängte Pools.
		for _, step := range []struct {
			name string
			fn   func(context.Context) error
		}{{"mount-units", sm.Reconcile}, {"freigaben", shm.Reconcile}, {"ruhezustand", sm.ReconcileSpindown}} {
			name, fn := step.name, step.fn
			ok := false
			for i := 0; i < 10 && !ok; i++ {
				if err := fn(ctx); err == nil {
					ok = true
				} else if i == 9 {
					log.Warn(name+" abgleichen", "err", err)
				} else {
					time.Sleep(time.Second)
				}
			}
		}
	}()
	// hdparm behält die Ruhezeit nicht über Neustarts/Hotplug: bei Plattenänderungen (entprellt 5 s) erneut anwenden.
	go func() {
		ch, cancel := hub.Subscribe()
		defer cancel()
		var timer *time.Timer
		fire := make(chan struct{}, 1)
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-ch:
				if ev.Type == "disks.changed" && timer == nil {
					timer = time.AfterFunc(5*time.Second, func() { fire <- struct{}{} })
				}
			case <-fire:
				timer = nil
				if err := sm.ReconcileSpindown(ctx); err != nil {
					log.Warn("ruhezustand", "err", err)
				}
			}
		}
	}()
	ns := notify.NewService(d, hub)
	mon := monitor.New(d, sm, ns, hub)
	go mon.Run(ctx)
	bm := backup.NewManager(d, agent, filepath.Join(stateDir, "backups"), version.Version, hub)
	go bm.Run(ctx, ns)
	handler := api.NewServer(cfg, api.Deps{DB: d, Agent: agent, Jobs: jm, Hub: hub, Auth: handlers, Storage: sm, Users: um, Shares: shm, Updates: system.NewUpdates(d, agent, jm, hub), Docker: docker.NewManager(d, agent, jm, hub), VMs: vm.NewManager(d, agent, hub), Notify: ns, Monitor: mon, Backup: bm})

	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	log.Info("betternas-api gestartet", "version", version.Version, "dev", cfg.Dev)
	if cfg.Dev {
		srv.Addr = ":8080"
		err = srv.ListenAndServe()
	} else {
		host, _ := os.Hostname()
		cert, key, cerr := api.EnsureSelfSignedCert(cfg.TLSDir, host)
		if cerr != nil {
			log.Error("zertifikat", "err", cerr)
			os.Exit(1)
		}
		srv.Addr = cfg.Listen
		err = srv.ListenAndServeTLS(cert, key)
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
