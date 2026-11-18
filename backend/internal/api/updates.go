package api

import (
	"net/http"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
)

func (s *server) registerUpdates(mux *http.ServeMux) {
	m := s.deps.Updates
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }

	mux.HandleFunc("GET /api/v1/updates", func(w http.ResponseWriter, r *http.Request) {
		o, err := m.Get(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, o)
	})
	mux.HandleFunc("GET /api/v1/updates/run", func(w http.ResponseWriter, r *http.Request) {
		run, err := m.RunStatus(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, run)
	})
	mux.HandleFunc("POST /api/v1/updates/check", func(w http.ResponseWriter, r *http.Request) {
		id, err := m.Check(r.Context(), actor(r))
		accepted(w, id, err)
	})
	mux.HandleFunc("PUT /api/v1/updates/channel", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Channel string `json:"channel"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		if err := m.SetChannel(r.Context(), actor(r), in.Channel); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/updates/install", func(w http.ResponseWriter, r *http.Request) {
		unit, err := m.Install(r.Context(), actor(r))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"unit": unit})
	})
}
