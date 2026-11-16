package api

import (
	"net/http"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/shares"
)

func (s *server) registerShares(mux *http.ServeMux) {
	m := s.deps.Shares
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }

	mux.HandleFunc("GET /api/v1/shares", func(w http.ResponseWriter, r *http.Request) {
		list, err := m.List(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("GET /api/v1/shares/{name}", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Get(r.Context(), r.PathValue("name"))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	})
	mux.HandleFunc("POST /api/v1/shares", func(w http.ResponseWriter, r *http.Request) {
		var in shares.Share
		if !decodeBody(w, r, &in) {
			return
		}
		id, err := m.Create(r.Context(), actor(r), in)
		accepted(w, id, err)
	})
	mux.HandleFunc("PUT /api/v1/shares/{name}", func(w http.ResponseWriter, r *http.Request) {
		var in shares.Share
		if !decodeBody(w, r, &in) {
			return
		}
		id, err := m.Update(r.Context(), actor(r), r.PathValue("name"), in)
		accepted(w, id, err)
	})
	mux.HandleFunc("DELETE /api/v1/shares/{name}", func(w http.ResponseWriter, r *http.Request) {
		id, err := m.Delete(r.Context(), actor(r), r.PathValue("name"))
		accepted(w, id, err)
	})
}
