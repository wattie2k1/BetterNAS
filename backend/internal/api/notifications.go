package api

import (
	"net/http"
	"strconv"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/monitor"
	"github.com/betternas/betternas/internal/notify"
)

func (s *server) registerNotifications(mux *http.ServeMux) {
	ns := s.deps.Notify
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }
	record := func(r *http.Request, action, target string, err error) {
		result := "ok"
		if err != nil {
			result = "error"
		}
		_ = audit.Record(r.Context(), s.deps.DB, audit.Entry{Actor: actor(r), Action: action, Target: target, Result: result})
	}

	mux.HandleFunc("GET /api/v1/notifications/config", func(w http.ResponseWriter, r *http.Request) {
		c, err := notify.LoadConfig(r.Context(), s.deps.DB)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c.Public())
	})
	mux.HandleFunc("PUT /api/v1/notifications/config", func(w http.ResponseWriter, r *http.Request) {
		var in notify.Config
		if !decodeBody(w, r, &in) {
			return
		}
		err := notify.SaveConfig(r.Context(), s.deps.DB, in)
		record(r, "notifications.config", "", err)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		c, err := notify.LoadConfig(r.Context(), s.deps.DB)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c.Public())
	})
	mux.HandleFunc("POST /api/v1/notifications/test", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Channel string `json:"channel"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		err := ns.SendTest(r.Context(), in.Channel)
		record(r, "notifications.test", in.Channel, err)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/notifications/history", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit == 0 {
			limit = 50
		}
		h, err := ns.History(r.Context(), limit)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, h)
	})
	mux.HandleFunc("GET /api/v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		if s.deps.Monitor == nil {
			writeJSON(w, http.StatusOK, []monitor.Active{})
			return
		}
		a, err := s.deps.Monitor.Active(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a)
	})
}
