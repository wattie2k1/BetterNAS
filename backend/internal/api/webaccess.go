package api

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/auth"
)

func (s *server) registerWebAccess(mux *http.ServeMux) {
	c := s.deps.Certs
	record := func(r *http.Request, action string, err error) {
		res := "ok"
		if err != nil {
			res = "error"
		}
		_ = audit.Record(r.Context(), s.deps.DB, audit.Entry{Actor: auth.SessionFrom(r.Context()).Username, Action: action, Target: "tls", Result: res})
	}
	mux.HandleFunc("GET /api/v1/system/certificate", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, c.Info())
	})
	mux.HandleFunc("POST /api/v1/system/certificate/regenerate", func(w http.ResponseWriter, r *http.Request) {
		host, _ := os.Hostname()
		err := c.Regenerate(host)
		record(r, "system.certificate.regenerate", err)
		if err != nil {
			apierr.Write(w, apierr.New(apierr.Internal, "Das Zertifikat konnte nicht erzeugt werden"))
			return
		}
		writeJSON(w, http.StatusOK, c.Info())
	})
	mux.HandleFunc("PUT /api/v1/system/certificate", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Cert string `json:"cert"`
			Key  string `json:"key"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		err := c.Install([]byte(in.Cert), []byte(in.Key))
		record(r, "system.certificate.install", err)
		if err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, c.Info())
	})
}
