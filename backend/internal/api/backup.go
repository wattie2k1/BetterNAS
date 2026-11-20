package api

import (
	"context"
	"io"
	"net/http"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/backup"
	"github.com/betternas/betternas/internal/jobs"
)

// maxBackupUpload begrenzt hochgeladene Archive (in Tests verkleinert).
var maxBackupUpload int64 = 256 << 20

func (s *server) registerBackup(mux *http.ServeMux) {
	m := s.deps.Backup
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }

	mux.HandleFunc("GET /api/v1/backups/config", func(w http.ResponseWriter, r *http.Request) {
		c, err := m.Config(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	})
	mux.HandleFunc("PUT /api/v1/backups/config", func(w http.ResponseWriter, r *http.Request) {
		var in backup.Config
		if !decodeBody(w, r, &in) {
			return
		}
		if err := m.SetConfig(r.Context(), actor(r), in); err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, in)
	})
	mux.HandleFunc("GET /api/v1/backups/status", func(w http.ResponseWriter, r *http.Request) {
		st, err := m.Status(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("POST /api/v1/backups/appdata", func(w http.ResponseWriter, r *http.Request) {
		who := actor(r)
		id := s.deps.Jobs.Start("backup.appdata", "appdata", who, func(ctx context.Context, log jobs.Logger) error {
			log.Printf("Sichere appdata …")
			res, err := m.CreateAppdata(ctx, who)
			if err != nil {
				return err
			}
			log.Printf("Fertig: %s", res.Info.Name)
			if res.Changed {
				log.Printf("Hinweis: Einige Dateien haben sich während der Sicherung geändert (laufende Container). Das Archiv kann daher Datenbanken in einem Zwischenzustand enthalten.")
			}
			return nil
		})
		accepted(w, id, nil)
	})
	mux.HandleFunc("GET /api/v1/backups", func(w http.ResponseWriter, r *http.Request) {
		l, err := m.List(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, l)
	})
	mux.HandleFunc("POST /api/v1/backups", func(w http.ResponseWriter, r *http.Request) {
		info, err := m.Create(r.Context(), actor(r))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, info)
	})
	mux.HandleFunc("GET /api/v1/backups/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		f, err := m.Open(r.Context(), name)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		defer f.Close()
		m.Audit(r.Context(), actor(r), "backup.download", name)
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		_, _ = io.Copy(w, f)
	})
	mux.HandleFunc("DELETE /api/v1/backups/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := m.Delete(r.Context(), actor(r), r.PathValue("name")); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/backups/restore/preview", func(w http.ResponseWriter, r *http.Request) {
		p, err := m.PreviewUpload(r.Context(), http.MaxBytesReader(w, r.Body, maxBackupUpload))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	})
	mux.HandleFunc("POST /api/v1/backups/{name}/restore/preview", func(w http.ResponseWriter, r *http.Request) {
		p, err := m.PreviewStored(r.Context(), r.PathValue("name"))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	})
	mux.HandleFunc("POST /api/v1/backups/restore", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Token string `json:"token"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		if err := m.Restore(r.Context(), actor(r), in.Token); err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "restarting"})
	})
}
