package api

import (
	"net/http"
	"strconv"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/version"
)

func (s *server) systemInfo(w http.ResponseWriter, r *http.Request) {
	var info map[string]any
	if err := s.deps.Agent.Call(r.Context(), "system.info", nil, &info); err != nil {
		apierr.Write(w, err)
		return
	}
	info["version"] = version.Version
	writeJSON(w, http.StatusOK, info)
}

func (s *server) listJobs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	list, err := s.deps.Jobs.List(r.Context(), limit)
	if err != nil {
		apierr.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *server) getJob(w http.ResponseWriter, r *http.Request) {
	j, err := s.deps.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		apierr.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}
