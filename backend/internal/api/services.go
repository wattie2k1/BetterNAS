package api

import (
	"encoding/json"
	"net/http"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
)

func (s *server) registerServices(mux *http.ServeMux) {
	m := s.deps.SystemSettings
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }
	get := func(path string, fn func(r *http.Request) (any, error)) {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			v, err := fn(r)
			if err != nil {
				apierr.Write(w, err)
				return
			}
			writeJSON(w, http.StatusOK, v)
		})
	}
	put := func(path string, fn func(r *http.Request, in map[string]any) error) {
		mux.HandleFunc("PUT "+path, func(w http.ResponseWriter, r *http.Request) {
			var in map[string]any
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
				apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
				return
			}
			if err := fn(r, in); err != nil {
				apierr.Write(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
	get("/api/v1/services", func(r *http.Request) (any, error) { return m.Services(r.Context()) })
	mux.HandleFunc("POST /api/v1/services/{unit}/{action}", func(w http.ResponseWriter, r *http.Request) {
		if err := m.ServiceAction(r.Context(), actor(r), r.PathValue("unit"), r.PathValue("action")); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	get("/api/v1/settings/smb", func(r *http.Request) (any, error) { return m.SMB(r.Context()) })
	put("/api/v1/settings/smb", func(r *http.Request, in map[string]any) error { return m.SetSMB(r.Context(), actor(r), in) })
	get("/api/v1/settings/docker", func(r *http.Request) (any, error) { return m.Docker(r.Context()) })
	put("/api/v1/settings/docker", func(r *http.Request, in map[string]any) error { return m.SetDocker(r.Context(), actor(r), in) })

	get("/api/v1/settings/ssh", func(r *http.Request) (any, error) { return m.SSH(r.Context()) })
	put("/api/v1/settings/ssh", func(r *http.Request, in map[string]any) error { return m.SetSSH(r.Context(), actor(r), in) })
	get("/api/v1/settings/ssh/keys/{user}", func(r *http.Request) (any, error) { return m.SSHKeys(r.Context(), r.PathValue("user")) })
	mux.HandleFunc("POST /api/v1/settings/ssh/keys/{user}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		if err := m.AddSSHKey(r.Context(), actor(r), r.PathValue("user"), in.Key); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /api/v1/settings/ssh/keys/{user}", func(w http.ResponseWriter, r *http.Request) {
		if err := m.RemoveSSHKey(r.Context(), actor(r), r.PathValue("user"), r.URL.Query().Get("fingerprint")); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	get("/api/v1/firewall", func(r *http.Request) (any, error) { return m.Firewall(r.Context()) })
	put("/api/v1/firewall", func(r *http.Request, in map[string]any) error {
		return m.SetFirewall(r.Context(), actor(r), auth.ClientIP(r), in)
	})
	mux.HandleFunc("POST /api/v1/firewall/confirm", func(w http.ResponseWriter, r *http.Request) {
		if err := m.ConfirmFirewall(r.Context(), actor(r)); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/firewall/revert", func(w http.ResponseWriter, r *http.Request) {
		if err := m.RevertFirewall(r.Context(), actor(r)); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
