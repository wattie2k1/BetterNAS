package api

import (
	"net/http"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/storage"
)

func (s *server) registerSnapraid(mux *http.ServeMux) {
	m := s.deps.Storage
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }

	mux.HandleFunc("PUT /api/v1/pools/{name}/snapraid", func(w http.ResponseWriter, r *http.Request) {
		var in storage.SnapraidInput
		if !decodeBody(w, r, &in) {
			return
		}
		id, err := m.EnableSnapraid(r.Context(), actor(r), r.PathValue("name"), in)
		accepted(w, id, err)
	})
	mux.HandleFunc("DELETE /api/v1/pools/{name}/snapraid", func(w http.ResponseWriter, r *http.Request) {
		id, err := m.DisableSnapraid(r.Context(), actor(r), r.PathValue("name"))
		accepted(w, id, err)
	})
	mux.HandleFunc("GET /api/v1/pools/{name}/snapraid/status", func(w http.ResponseWriter, r *http.Request) {
		st, err := m.SnapraidStatus(r.Context(), r.PathValue("name"))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("POST /api/v1/pools/{name}/snapraid/replace", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OldDisk string `json:"oldDisk"`
			NewDisk string `json:"newDisk"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		id, err := m.ReplaceDisk(r.Context(), actor(r), r.PathValue("name"), req.OldDisk, req.NewDisk)
		accepted(w, id, err)
	})
	mux.HandleFunc("POST /api/v1/pools/{name}/snapraid/recovery/retry", func(w http.ResponseWriter, r *http.Request) {
		id, err := m.RetryFix(r.Context(), actor(r), r.PathValue("name"))
		accepted(w, id, err)
	})
	mux.HandleFunc("DELETE /api/v1/pools/{name}/snapraid/recovery", func(w http.ResponseWriter, r *http.Request) {
		if err := m.CancelRecovery(r.Context(), actor(r), r.PathValue("name")); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	for _, action := range []string{"sync", "scrub", "diff"} {
		mux.HandleFunc("POST /api/v1/pools/{name}/snapraid/"+action, func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Force bool `json:"force"`
			}
			if !decodeBody(w, r, &req) {
				return
			}
			id, err := m.RunSnapraid(r.Context(), actor(r), r.PathValue("name"), action, req.Force)
			accepted(w, id, err)
		})
	}
}
