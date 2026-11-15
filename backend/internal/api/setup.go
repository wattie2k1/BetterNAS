package api

import (
	"database/sql"
	"errors"
	"net/http"
	"os"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/auth"
)

const settingSetupCompleted = "setup.completed"

func (s *server) registerSetup(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/setup", func(w http.ResponseWriter, r *http.Request) {
		var v string
		err := s.deps.DB.QueryRowContext(r.Context(), `SELECT value FROM settings WHERE key = ?`, settingSetupCompleted).Scan(&v)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			apierr.Write(w, err)
			return
		}
		host, _ := os.Hostname()
		if host == "" {
			host = "betternas"
		}
		writeJSON(w, http.StatusOK, map[string]any{"completed": v == "1", "hostname": host})
	})
	mux.HandleFunc("POST /api/v1/setup/complete", func(w http.ResponseWriter, r *http.Request) {
		_, err := s.deps.DB.ExecContext(r.Context(),
			`INSERT INTO settings(key, value) VALUES (?, '1') ON CONFLICT(key) DO UPDATE SET value = '1'`, settingSetupCompleted)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		_ = audit.Record(r.Context(), s.deps.DB, audit.Entry{Actor: auth.SessionFrom(r.Context()).Username, Action: "setup.complete", Target: "setup", Result: "ok"})
		w.WriteHeader(http.StatusNoContent)
	})
}
