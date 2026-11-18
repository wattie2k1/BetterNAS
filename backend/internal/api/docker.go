package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

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
	mux.HandleFunc("GET /api/v1/docker/updates", func(w http.ResponseWriter, r *http.Request) {
		rep, err := m.Updates(r.Context())
		reply(w, rep, err)
	})
	mux.HandleFunc("POST /api/v1/docker/updates/check", func(w http.ResponseWriter, r *http.Request) {
		id, err := m.CheckUpdates(r.Context(), actor(r))
		accepted(w, id, err)
	})
	mux.HandleFunc("GET /api/v1/stacks/{name}/backup-plan", func(w http.ResponseWriter, r *http.Request) {
		p, err := m.BackupPlan(r.Context(), r.PathValue("name"))
		reply(w, p, err)
	})
	mux.HandleFunc("GET /api/v1/docker/folders", func(w http.ResponseWriter, r *http.Request) {
		l, err := m.Folders(r.Context(), r.URL.Query().Get("path"))
		reply(w, l, err)
	})
	mux.HandleFunc("POST /api/v1/docker/folders", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Path string `json:"path"`
			Name string `json:"name"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		f, err := m.CreateFolder(r.Context(), actor(r), in.Path, in.Name)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, f)
	})
	mux.HandleFunc("GET /api/v1/docker/devices", func(w http.ResponseWriter, r *http.Request) {
		l, err := m.HostDevices(r.Context())
		reply(w, l, err)
	})
	mux.HandleFunc("GET /api/v1/docker/autoupdate", func(w http.ResponseWriter, r *http.Request) {
		c, err := m.AutoUpdate(r.Context())
		reply(w, c, err)
	})
	mux.HandleFunc("PUT /api/v1/docker/autoupdate", func(w http.ResponseWriter, r *http.Request) {
		var in docker.AutoUpdateConfig
		if !decodeBody(w, r, &in) {
			return
		}
		if err := m.SetAutoUpdate(r.Context(), actor(r), in); err != nil {
			apierr.Write(w, err)
			return
		}
		c, err := m.AutoUpdate(r.Context())
		reply(w, c, err)
	})
	mux.HandleFunc("POST /api/v1/docker/autoupdate/run", func(w http.ResponseWriter, r *http.Request) {
		var n docker.Notifier // nil-Interface, wenn keine Benachrichtigungen eingerichtet sind
		if s.deps.Notify != nil {
			n = s.deps.Notify
		}
		id, err := m.StartAutoUpdate(r.Context(), actor(r), n)
		accepted(w, id, err)
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
	mux.HandleFunc("GET /api/v1/stacks/{name}/icon", func(w http.ResponseWriter, r *http.Request) {
		ic, err := m.GetIcon(r.Context(), r.PathValue("name"))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		h := w.Header()
		h.Set("Content-Type", ic.ContentType)
		// SVG darf nie Skripte ausführen, auch wenn jemand die Adresse direkt öffnet.
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
		h.Set("Cache-Control", "private, max-age=86400")
		h.Set("ETag", `"`+ic.Version+`"`)
		_, _ = w.Write(ic.Data)
	})
	mux.HandleFunc("PUT /api/v1/stacks/{name}/icon", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			var in struct {
				URL string `json:"url"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&in); err != nil {
				apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
				return
			}
			noContent(w, m.SetIconFromURL(r.Context(), actor(r), name, in.URL))
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, docker.MaxIconBytes+1))
		if err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Das Icon ist zu groß (höchstens 512 KiB)"))
			return
		}
		noContent(w, m.SetIcon(r.Context(), actor(r), name, data))
	})
	mux.HandleFunc("DELETE /api/v1/stacks/{name}/icon", func(w http.ResponseWriter, r *http.Request) {
		noContent(w, m.DeleteIcon(r.Context(), actor(r), r.PathValue("name")))
	})
	mux.HandleFunc("POST /api/v1/stacks/{name}/validate", func(w http.ResponseWriter, r *http.Request) {
		c, err := m.ValidateStack(r.Context(), r.PathValue("name"))
		reply(w, c, err)
	})
	for action := range docker.StackActions {
		mux.HandleFunc("POST /api/v1/stacks/{name}/"+action, func(w http.ResponseWriter, r *http.Request) {
			// Optionaler Body mit Zusatzwünschen: {"backup":true} beim Update, {"restoreData":true} beim Rollback.
			var opts docker.RunOptions
			if r.ContentLength != 0 {
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&opts); err != nil && !errors.Is(err, io.EOF) {
					apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
					return
				}
			}
			id, err := m.RunStackOpts(r.Context(), actor(r), r.PathValue("name"), action, opts)
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

	lists := map[string]func(w http.ResponseWriter, r *http.Request){
		docker.KindImage: func(w http.ResponseWriter, r *http.Request) {
			v, err := m.Images(r.Context())
			reply(w, v, err)
		},
		docker.KindNetwork: func(w http.ResponseWriter, r *http.Request) {
			v, err := m.Networks(r.Context())
			reply(w, v, err)
		},
		docker.KindVolume: func(w http.ResponseWriter, r *http.Request) {
			v, err := m.Volumes(r.Context())
			reply(w, v, err)
		},
	}
	for kind, plural := range map[string]string{docker.KindImage: "images", docker.KindNetwork: "networks", docker.KindVolume: "volumes"} {
		mux.HandleFunc("GET /api/v1/docker/"+plural, lists[kind])
		// ref steht in der Query, weil Image-Referenzen Schrägstriche enthalten.
		mux.HandleFunc("DELETE /api/v1/docker/"+plural, func(w http.ResponseWriter, r *http.Request) {
			noContent(w, m.RemoveResource(r.Context(), actor(r), kind, r.URL.Query().Get("ref")))
		})
		mux.HandleFunc("POST /api/v1/docker/"+plural+"/prune", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				All bool `json:"all"`
			}
			if !decodeBody(w, r, &req) {
				return
			}
			res, err := m.PruneResources(r.Context(), actor(r), kind, req.All)
			reply(w, res, err)
		})
	}
}
