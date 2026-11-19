package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/storage"
)

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(v); err != nil {
		apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
		return false
	}
	return true
}

func accepted(w http.ResponseWriter, jobID string, err error) {
	if err != nil {
		apierr.Write(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
}

func (s *server) registerStorage(mux *http.ServeMux) {
	m := s.deps.Storage
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }

	mux.HandleFunc("GET /api/v1/disks", func(w http.ResponseWriter, r *http.Request) {
		list, err := m.ListDisks(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("PUT /api/v1/disks/{id}/spindown", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Minutes int `json:"minutes"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		if err := m.SetSpindown(r.Context(), actor(r), r.PathValue("id"), in.Minutes); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/disks/{id}/selftest", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Kind string `json:"kind"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		if err := m.StartSelfTest(r.Context(), actor(r), r.PathValue("id"), in.Kind); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /api/v1/disks/{id}/selftest", func(w http.ResponseWriter, r *http.Request) {
		if err := m.AbortSelfTest(r.Context(), actor(r), r.PathValue("id")); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/disks/selftest/schedule", func(w http.ResponseWriter, r *http.Request) {
		sc, err := m.SelfTestSchedule(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, sc)
	})
	mux.HandleFunc("PUT /api/v1/disks/selftest/schedule", func(w http.ResponseWriter, r *http.Request) {
		var in storage.SelfTestSchedule
		if !decodeBody(w, r, &in) {
			return
		}
		if err := m.SetSelfTestSchedule(r.Context(), actor(r), in); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/disks/{id}/history", func(w http.ResponseWriter, r *http.Request) {
		days := 0
		if v := r.URL.Query().Get("days"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültiger Zeitraum."))
				return
			}
			days = n
		}
		h, err := m.History(r.Context(), r.PathValue("id"), days)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, h)
	})
	mux.HandleFunc("POST /api/v1/disks/{id}/format/preview", func(w http.ResponseWriter, r *http.Request) {
		p, err := m.PreviewFormat(r.Context(), r.PathValue("id"))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	})
	mux.HandleFunc("POST /api/v1/disks/{id}/format", func(w http.ResponseWriter, r *http.Request) {
		var req storage.FormatRequest
		if !decodeBody(w, r, &req) {
			return
		}
		id, err := m.Format(r.Context(), actor(r), r.PathValue("id"), req)
		accepted(w, id, err)
	})
	mux.HandleFunc("POST /api/v1/disks/{id}/import", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Role string `json:"role"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		id, err := m.Import(r.Context(), actor(r), r.PathValue("id"), req.Role)
		accepted(w, id, err)
	})
	mux.HandleFunc("POST /api/v1/disks/{id}/release", func(w http.ResponseWriter, r *http.Request) {
		id, err := m.Release(r.Context(), actor(r), r.PathValue("id"))
		accepted(w, id, err)
	})

	mux.HandleFunc("GET /api/v1/pools", func(w http.ResponseWriter, r *http.Request) {
		list, err := m.ListPools(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("GET /api/v1/pools/{name}", func(w http.ResponseWriter, r *http.Request) {
		list, err := m.ListPools(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		for _, p := range list {
			if p.Name == r.PathValue("name") {
				writeJSON(w, http.StatusOK, p)
				return
			}
		}
		apierr.Write(w, apierr.New(apierr.NotFound, "Pool nicht gefunden"))
	})
	mux.HandleFunc("POST /api/v1/pools", func(w http.ResponseWriter, r *http.Request) {
		var in storage.PoolInput
		if !decodeBody(w, r, &in) {
			return
		}
		id, err := m.CreatePool(r.Context(), actor(r), in)
		accepted(w, id, err)
	})
	mux.HandleFunc("PATCH /api/v1/pools/{name}", func(w http.ResponseWriter, r *http.Request) {
		var in storage.PoolInput
		if !decodeBody(w, r, &in) {
			return
		}
		id, err := m.UpdatePool(r.Context(), actor(r), r.PathValue("name"), in)
		accepted(w, id, err)
	})
	mux.HandleFunc("DELETE /api/v1/pools/{name}", func(w http.ResponseWriter, r *http.Request) {
		id, err := m.DeletePool(r.Context(), actor(r), r.PathValue("name"))
		accepted(w, id, err)
	})
}
