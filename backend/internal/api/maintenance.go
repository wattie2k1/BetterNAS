package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/version"
)

// resetWord muss zur Bestätigung der Werkseinstellungen eingegeben werden.
const resetWord = "ZURÜCKSETZEN"

// logSources sind die Journale, die der Agent liefert (Namen wie in agent.logUnits).
var logSources = map[string]bool{"api": true, "agent": true, "samba": true, "docker": true, "libvirt": true}

func atoiDefault(v string, def, min, max int) int {
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		return def
	}
	return n
}

func (s *server) registerMaintenance(mux *http.ServeMux) {
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }

	mux.HandleFunc("GET /api/v1/logs/audit", func(w http.ResponseWriter, r *http.Request) {
		list, err := audit.List(r.Context(), s.deps.DB, atoiDefault(r.URL.Query().Get("limit"), 200, 1, 1000))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		if list == nil {
			list = []audit.StoredEntry{}
		}
		writeJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("GET /api/v1/logs/{source}", func(w http.ResponseWriter, r *http.Request) {
		source := r.PathValue("source")
		if !logSources[source] {
			apierr.Write(w, apierr.New(apierr.NotFound, "Unbekannte Protokollquelle"))
			return
		}
		var out struct {
			Text string `json:"text"`
		}
		lines := atoiDefault(r.URL.Query().Get("lines"), 200, 10, 2000)
		if err := s.deps.Agent.Call(r.Context(), "logs.read", map[string]any{"source": source, "lines": lines}, &out); err != nil {
			apierr.Write(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Query().Get("download") == "1" {
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="betternas-%s-%s.log"`, source, time.Now().Format("20060102-150405")))
		}
		_, _ = w.Write([]byte(out.Text))
	})
	mux.HandleFunc("GET /api/v1/support-bundle", func(w http.ResponseWriter, r *http.Request) {
		files := map[string]string{}
		if err := s.deps.Agent.Call(r.Context(), "support.collect", nil, &files); err != nil {
			apierr.Write(w, err)
			return
		}
		host, _ := os.Hostname()
		files["info.txt"] = fmt.Sprintf("BetterNAS %s\nHost: %s\nErzeugt: %s\nErzeugt von: %s\n", version.Version, host, time.Now().Format(time.RFC3339), actor(r))
		if list, err := audit.List(r.Context(), s.deps.DB, 1000); err == nil {
			b, _ := json.MarshalIndent(list, "", "  ")
			files["audit.json"] = string(b)
		}
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		names := make([]string, 0, len(files))
		for n := range files {
			names = append(names, n)
		}
		sort.Strings(names)
		now := time.Now()
		for _, n := range names {
			body := []byte(files[n])
			if err := tw.WriteHeader(&tar.Header{Name: "betternas-support/" + n, Mode: 0o644, Size: int64(len(body)), ModTime: now}); err != nil {
				apierr.Write(w, err)
				return
			}
			_, _ = tw.Write(body)
		}
		if err := tw.Close(); err != nil {
			apierr.Write(w, err)
			return
		}
		if err := gz.Close(); err != nil {
			apierr.Write(w, err)
			return
		}
		_ = audit.Record(r.Context(), s.deps.DB, audit.Entry{Actor: actor(r), Action: "support.bundle", Target: "download", Result: "ok"})
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="betternas-support-%s.tar.gz"`, time.Now().Format("20060102-150405")))
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(buf.Bytes())
	})
	if s.deps.Backup != nil {
		mux.HandleFunc("POST /api/v1/system/factory-reset", func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				Confirm string `json:"confirm"`
			}
			if !decodeBody(w, r, &in) {
				return
			}
			if in.Confirm != resetWord {
				apierr.Write(w, apierr.New(apierr.InvalidParams, "Zur Bestätigung muss das Wort „"+resetWord+"“ eingegeben werden"))
				return
			}
			if err := s.deps.Backup.FactoryReset(r.Context(), actor(r)); err != nil {
				apierr.Write(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
}
