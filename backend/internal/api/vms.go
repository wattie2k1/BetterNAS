package api

import (
	"encoding/json"
	"net/http"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/vm"
)

const maxVMBody = 64 << 10

func (s *server) registerVMs(mux *http.ServeMux) {
	m := s.deps.VMs
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

	mux.HandleFunc("GET /api/v1/vms/status", func(w http.ResponseWriter, r *http.Request) {
		st, err := m.Status(r.Context())
		reply(w, st, err)
	})
	mux.HandleFunc("GET /api/v1/vms/networks", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Networks(r.Context())
		reply(w, v, err)
	})
	mux.HandleFunc("GET /api/v1/vms/isos", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.ISOs(r.Context())
		reply(w, v, err)
	})
	mux.HandleFunc("GET /api/v1/vms", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.List(r.Context())
		reply(w, v, err)
	})
	mux.HandleFunc("POST /api/v1/vms", func(w http.ResponseWriter, r *http.Request) {
		var in vm.CreateInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxVMBody)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		if err := m.Create(r.Context(), actor(r), in); err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"name": in.Name})
	})
	for action := range vm.Actions {
		mux.HandleFunc("POST /api/v1/vms/{name}/"+action, func(w http.ResponseWriter, r *http.Request) {
			noContent(w, m.Action(r.Context(), actor(r), r.PathValue("name"), action))
		})
	}
	mux.HandleFunc("DELETE /api/v1/vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		noContent(w, m.Delete(r.Context(), actor(r), r.PathValue("name"), r.URL.Query().Get("disks") == "1"))
	})
}
