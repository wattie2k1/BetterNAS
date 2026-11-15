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
	"syscall"
	"time"

	"github.com/betternas/betternas/internal/api"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/version"
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
	handlers := auth.NewHandlers(d, auth.NewStore(d, 12*time.Hour), auth.AgentVerifier{Agent: agent}, auth.SystemGroupChecker, !cfg.Dev)
	sm := storage.NewManager(d, agent, jm, hub)
	handler := api.NewServer(cfg, api.Deps{DB: d, Agent: agent, Jobs: jm, Hub: hub, Auth: handlers, Storage: sm})

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
