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
	mux.HandleFunc("GET /api/v1/disks/io", func(w http.ResponseWriter, r *http.Request) {
		st, err := m.DiskIO()
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
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
	mux.HandleFunc("PUT /api/v1/disks/{id}/note", func(w http.ResponseWriter, r *http.Request) {
		var in storage.DiskNote
		if !decodeBody(w, r, &in) {
			return
		}
		if err := m.SetNote(r.Context(), actor(r), r.PathValue("id"), in); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/disks/{id}/power", func(w http.ResponseWriter, r *http.Request) {
		days := 0
		if v := r.URL.Query().Get("days"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültiger Zeitraum."))
				return
			}
			days = n
		}
		st, err := m.PowerStats(r.Context(), r.PathValue("id"), days)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("GET /api/v1/disks/{id}/tuning", func(w http.ResponseWriter, r *http.Request) {
		t, err := m.Tuning(r.Context(), r.PathValue("id"))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, t)
	})
	mux.HandleFunc("PUT /api/v1/disks/{id}/tuning", func(w http.ResponseWriter, r *http.Request) {
		var in storage.TuningSettings
		if !decodeBody(w, r, &in) {
			return
		}
		if err := m.SetTuning(r.Context(), actor(r), r.PathValue("id"), in); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/disks/{id}/dismiss", func(w http.ResponseWriter, r *http.Request) {
		if err := m.DismissNew(r.Context(), actor(r), r.PathValue("id")); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/disks/{id}/identify", func(w http.ResponseWriter, r *http.Request) {
		if err := m.StartIdentify(r.Context(), actor(r), r.PathValue("id")); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /api/v1/disks/{id}/identify", func(w http.ResponseWriter, r *http.Request) {
		m.StopIdentify(r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/disks/{id}/fscheck", func(w http.ResponseWriter, r *http.Request) {
		if err := m.StartFsCheck(r.Context(), actor(r), r.PathValue("id")); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("POST /api/v1/disks/{id}/bench", func(w http.ResponseWriter, r *http.Request) {
		if err := m.StartBench(r.Context(), actor(r), r.PathValue("id")); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("POST /api/v1/disks/{id}/erase/preview", func(w http.ResponseWriter, r *http.Request) {
		p, err := m.PreviewErase(r.Context(), r.PathValue("id"))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	})
	mux.HandleFunc("POST /api/v1/disks/{id}/erase", func(w http.ResponseWriter, r *http.Request) {
		var in storage.EraseRequest
		if !decodeBody(w, r, &in) {
			return
		}
		if err := m.StartErase(r.Context(), actor(r), r.PathValue("id"), in); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("DELETE /api/v1/disks/{id}/erase", func(w http.ResponseWriter, r *http.Request) {
		if err := m.AbortErase(r.Context(), actor(r), r.PathValue("id")); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /api/v1/disks/{id}/spare", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Spare bool `json:"spare"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		if err := m.SetSpare(r.Context(), actor(r), r.PathValue("id"), in.Spare); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
	mux.HandleFunc("GET /api/v1/pools/{name}/btrfs", func(w http.ResponseWriter, r *http.Request) {
		st, err := m.BtrfsStatus(r.Context(), r.PathValue("name"))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("POST /api/v1/pools/{name}/btrfs/scrub", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action string `json:"action"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		if req.Action == "" {
			req.Action = "start"
		}
		if err := m.BtrfsScrub(r.Context(), actor(r), r.PathValue("name"), req.Action); err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/v1/pools/preview", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Branches []string `json:"branches"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		p, err := m.PreviewBtrfsPool(r.Context(), req.Branches)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
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
		id, err := m.DeletePoolConfirm(r.Context(), actor(r), r.PathValue("name"), r.URL.Query().Get("confirm"))
		accepted(w, id, err)
	})
}
