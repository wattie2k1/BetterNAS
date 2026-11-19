package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/tlsmgr"
)

// registerTLS bindet lokale Zertifizierungsstelle und Let's Encrypt an.
func (s *server) registerTLS(mux *http.ServeMux) {
	m := s.deps.TLS
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }
	record := func(ctx context.Context, who, action string, err error) {
		res := "ok"
		if err != nil {
			res = "error"
		}
		_ = audit.Record(ctx, s.deps.DB, audit.Entry{Actor: who, Action: action, Target: "tls", Result: res})
	}
	status := func(w http.ResponseWriter, r *http.Request, code int) {
		st, err := m.Status(r.Context())
		if err != nil {
			apierr.Write(w, apierr.New(apierr.Internal, "Der Zertifikatsstatus ist nicht lesbar"))
			return
		}
		writeJSON(w, code, st)
	}

	mux.HandleFunc("GET /api/v1/system/tls", func(w http.ResponseWriter, r *http.Request) {
		status(w, r, http.StatusOK)
	})

	// Lokale CA: einrichten, Namen ändern oder (mit resetCA) komplett neu anlegen.
	mux.HandleFunc("POST /api/v1/system/tls/localca", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Names   []string `json:"names"`
			ResetCA bool     `json:"resetCA"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		err := m.EnableLocalCA(r.Context(), in.Names, in.ResetCA)
		action := "system.tls.localca"
		if in.ResetCA {
			action = "system.tls.localca.reset"
		}
		record(r.Context(), actor(r), action, err)
		if err != nil {
			var ae *apierr.Error
			if errors.As(err, &ae) {
				apierr.Write(w, err)
			} else {
				apierr.Write(w, apierr.New(apierr.Internal, "Die lokale Zertifizierungsstelle konnte nicht eingerichtet werden"))
			}
			return
		}
		status(w, r, http.StatusOK)
	})
	mux.HandleFunc("GET /api/v1/system/tls/ca.crt", func(w http.ResponseWriter, r *http.Request) {
		b, err := m.CACert()
		if err != nil {
			apierr.Write(w, err)
			return
		}
		record(r.Context(), actor(r), "system.tls.ca.download", nil)
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		w.Header().Set("Content-Disposition", `attachment; filename="betternas-ca.crt"`)
		_, _ = w.Write(b)
	})

	// Let's Encrypt: Einstellungen speichern, anfordern, entfernen.
	mux.HandleFunc("PUT /api/v1/system/tls/acme", func(w http.ResponseWriter, r *http.Request) {
		var in tlsmgr.ACMEInput
		if !decodeBody(w, r, &in) {
			return
		}
		err := m.SaveACME(r.Context(), in)
		record(r.Context(), actor(r), "system.tls.acme.save", err)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		status(w, r, http.StatusOK)
	})
	mux.HandleFunc("DELETE /api/v1/system/tls/acme", func(w http.ResponseWriter, r *http.Request) {
		err := m.RemoveACME(r.Context())
		record(r.Context(), actor(r), "system.tls.acme.remove", err)
		if err != nil {
			apierr.Write(w, apierr.New(apierr.Internal, "Die Einstellungen konnten nicht gelöscht werden"))
			return
		}
		status(w, r, http.StatusOK)
	})
	mux.HandleFunc("POST /api/v1/system/tls/acme/issue", func(w http.ResponseWriter, r *http.Request) {
		st, err := m.Status(r.Context())
		if err != nil {
			apierr.Write(w, apierr.New(apierr.Internal, "Der Zertifikatsstatus ist nicht lesbar"))
			return
		}
		if !st.ACME.Configured {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Let's Encrypt ist noch nicht eingerichtet."))
			return
		}
		if st.ACME.Busy {
			apierr.Write(w, apierr.New(apierr.Conflict, "Es läuft bereits eine Zertifikatsanforderung."))
			return
		}
		who := actor(r)
		id := s.deps.Jobs.Start("tls.acme", "tls", who, func(ctx context.Context, log jobs.Logger) error {
			err := m.IssueACME(ctx, log)
			record(context.WithoutCancel(ctx), who, "system.tls.acme.issue", err)
			return err
		})
		accepted(w, id, nil)
	})
}
