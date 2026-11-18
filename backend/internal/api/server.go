// Package api ist die HTTP-/WebSocket-Schnittstelle der Weboberfläche.
package api

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/backup"
	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/monitor"
	"github.com/betternas/betternas/internal/netcfg"
	"github.com/betternas/betternas/internal/notify"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/shares"
	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/system"
	"github.com/betternas/betternas/internal/users"
	"github.com/betternas/betternas/internal/vm"
)

// Config ist die Konfiguration des API-Servers.
type Config struct {
	Listen, DBPath, AgentSocket, TLSDir, WebDir string
	Dev                                         bool
}

// Deps sind die Abhängigkeiten des API-Servers.
type Deps struct {
	DB    *sql.DB
	Agent *rpc.Client
	Jobs  *jobs.Manager
	Hub   *events.Hub
	Auth  *auth.Handlers
	// Storage ist optional (nil = keine Speicher-Routen).
	Storage *storage.Manager
	// Users ist optional (nil = keine Benutzer-Routen).
	Users *users.Manager
	// Shares ist optional (nil = keine Freigaben-Routen).
	Shares *shares.Manager
	// Updates ist optional (nil = keine Update-Routen).
	Updates *system.Updates
	// Docker ist optional (nil = keine Docker-Routen).
	Docker *docker.Manager
	// VMs ist optional (nil = keine VM-Routen).
	VMs *vm.Manager
	// Network ist optional (nil = keine Netzwerk-Routen).
	Network *netcfg.Manager
	// Notify ist optional (nil = keine Benachrichtigungs-Routen).
	Notify *notify.Service
	// TOTP ist optional (nil = keine 2FA-Routen).
	TOTP *auth.TOTPStore
	// Backup ist optional (nil = keine Backup-Routen).
	Backup *backup.Manager
	// SystemSettings ist optional (nil = keine Hostname-/Zeit-/Energie-Routen).
	SystemSettings *system.Settings
	// Certs ist optional (nil = keine Zertifikat-Routen).
	Certs *CertStore
	// Monitor ist optional (nil = keine aktiven Meldungen).
	Monitor *monitor.Monitor
}

type server struct {
	cfg  Config
	deps Deps
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// NewServer baut den HTTP-Handler mit allen Routen.
func NewServer(cfg Config, deps Deps) http.Handler {
	s := &server{cfg: cfg, deps: deps}
	api := http.NewServeMux()
	api.HandleFunc("POST /api/v1/auth/logout", deps.Auth.Logout)
	api.HandleFunc("GET /api/v1/auth/me", deps.Auth.Me)
	api.HandleFunc("GET /api/v1/system/info", s.systemInfo)
	api.HandleFunc("GET /api/v1/jobs", s.listJobs)
	api.HandleFunc("GET /api/v1/jobs/{id}", s.getJob)
	api.HandleFunc("GET /api/v1/ws", s.websocket)
	s.registerSetup(api)
	s.registerMaintenance(api)
	deps.Auth.RegisterSecurity(api)
	if deps.Storage != nil {
		s.registerStorage(api)
		s.registerSnapraid(api)
	}
	if deps.Users != nil {
		s.registerUsers(api)
	}
	if deps.Shares != nil {
		s.registerShares(api)
	}
	if deps.Updates != nil {
		s.registerUpdates(api)
	}
	if deps.TOTP != nil {
		s.registerTOTP(api)
	}
	if deps.Backup != nil {
		s.registerBackup(api)
	}
	if deps.Notify != nil {
		s.registerNotifications(api)
	}
	if deps.VMs != nil {
		s.registerVMs(api)
	}
	if deps.Network != nil {
		s.registerNetwork(api)
	}
	if deps.SystemSettings != nil {
		s.registerSystemSettings(api)
		s.registerServices(api)
	}
	if deps.Certs != nil {
		s.registerWebAccess(api)
	}
	if deps.Docker != nil {
		s.registerDocker(api)
		s.registerDockerStreams(api)
	}
	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		apierr.Write(w, apierr.New(apierr.NotFound, "Unbekannte Schnittstelle"))
	})

	root := http.NewServeMux()
	root.HandleFunc("POST /api/v1/auth/login", deps.Auth.Login)
	root.HandleFunc("POST /api/v1/auth/login/totp", deps.Auth.LoginTOTP)
	root.Handle("/api/", deps.Auth.RequireSession(api))
	root.Handle("/", spaHandler(cfg.WebDir))
	return securityHeaders(auth.LANOnly(root))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
