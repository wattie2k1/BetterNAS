// Package api ist die HTTP-/WebSocket-Schnittstelle der Weboberfläche.
package api

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/rpc"
	"github.com/betternas/betternas/internal/shares"
	"github.com/betternas/betternas/internal/storage"
	"github.com/betternas/betternas/internal/system"
	"github.com/betternas/betternas/internal/users"
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
	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		apierr.Write(w, apierr.New(apierr.NotFound, "Unbekannte Schnittstelle"))
	})

	root := http.NewServeMux()
	root.HandleFunc("POST /api/v1/auth/login", deps.Auth.Login)
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
