package api

import (
	"encoding/json"
	"net/http"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/docker"
)

// maxStackBody: compose.yaml (256 KiB) + .env (64 KiB) plus JSON-Overhead.
const maxStackBody = 512 << 10

func decodeStack(w http.ResponseWriter, r *http.Request, in *docker.StackInput) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxStackBody)).Decode(in); err != nil {
		apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
		return false
	}
	return true
}

func (s *server) registerDocker(mux *http.ServeMux) {
	m := s.deps.Docker
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }
	reply := func(w http.ResponseWriter, v any, err error) {
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
	noContent := func(w http.ResponseWriter, err error) {
		if err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}

	mux.HandleFunc("GET /api/v1/docker/status", func(w http.ResponseWriter, r *http.Request) {
		st, err := m.Status(r.Context())
		reply(w, st, err)
	})
	mux.HandleFunc("GET /api/v1/stacks", func(w http.ResponseWriter, r *http.Request) {
		list, err := m.ListStacks(r.Context())
		reply(w, list, err)
	})
	mux.HandleFunc("POST /api/v1/stacks", func(w http.ResponseWriter, r *http.Request) {
		var in docker.StackInput
		if !decodeStack(w, r, &in) {
			return
		}
		if err := m.CreateStack(r.Context(), actor(r), in); err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"name": in.Name})
	})
	mux.HandleFunc("GET /api/v1/stacks/{name}", func(w http.ResponseWriter, r *http.Request) {
		sf, err := m.GetStack(r.Context(), r.PathValue("name"))
		reply(w, sf, err)
	})
	mux.HandleFunc("PUT /api/v1/stacks/{name}", func(w http.ResponseWriter, r *http.Request) {
		var in docker.StackInput
		if !decodeStack(w, r, &in) {
			return
		}
		in.Name = r.PathValue("name")
		noContent(w, m.UpdateStack(r.Context(), actor(r), in))
	})
	mux.HandleFunc("DELETE /api/v1/stacks/{name}", func(w http.ResponseWriter, r *http.Request) {
		noContent(w, m.DeleteStack(r.Context(), actor(r), r.PathValue("name")))
	})
	mux.HandleFunc("POST /api/v1/stacks/{name}/validate", func(w http.ResponseWriter, r *http.Request) {
		c, err := m.ValidateStack(r.Context(), r.PathValue("name"))
		reply(w, c, err)
	})
	for action := range docker.StackActions {
		mux.HandleFunc("POST /api/v1/stacks/{name}/"+action, func(w http.ResponseWriter, r *http.Request) {
			id, err := m.RunStack(r.Context(), actor(r), r.PathValue("name"), action)
			accepted(w, id, err)
		})
	}
	mux.HandleFunc("GET /api/v1/containers", func(w http.ResponseWriter, r *http.Request) {
		list, err := m.ListContainers(r.Context())
		reply(w, list, err)
	})
	for action := range docker.ContainerActions {
		mux.HandleFunc("POST /api/v1/containers/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			noContent(w, m.ContainerAction(r.Context(), actor(r), r.PathValue("id"), action))
		})
	}
}
