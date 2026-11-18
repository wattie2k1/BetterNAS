package api

import (
	"encoding/json"
	"net/http"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/netcfg"
)

func (s *server) registerNetwork(mux *http.ServeMux) {
	m := s.deps.Network
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }
	done := func(w http.ResponseWriter, err error) {
		if err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
	mux.HandleFunc("GET /api/v1/network", func(w http.ResponseWriter, r *http.Request) {
		st, err := m.IPStatus(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("PUT /api/v1/network", func(w http.ResponseWriter, r *http.Request) {
		var in netcfg.IPConfig
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		done(w, m.IPApply(r.Context(), actor(r), in))
	})
	mux.HandleFunc("POST /api/v1/network/confirm", func(w http.ResponseWriter, r *http.Request) {
		done(w, m.IPConfirm(r.Context(), actor(r)))
	})
	mux.HandleFunc("POST /api/v1/network/revert", func(w http.ResponseWriter, r *http.Request) {
		done(w, m.IPRevert(r.Context(), actor(r)))
	})
	mux.HandleFunc("GET /api/v1/network/bridge", func(w http.ResponseWriter, r *http.Request) {
		st, err := m.Status(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("POST /api/v1/network/bridge/enable", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Interface string `json:"interface"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		done(w, m.Enable(r.Context(), actor(r), in.Interface))
	})
	mux.HandleFunc("POST /api/v1/network/bridge/confirm", func(w http.ResponseWriter, r *http.Request) {
		done(w, m.Confirm(r.Context(), actor(r)))
	})
	mux.HandleFunc("POST /api/v1/network/bridge/disable", func(w http.ResponseWriter, r *http.Request) {
		done(w, m.Disable(r.Context(), actor(r)))
	})
}
