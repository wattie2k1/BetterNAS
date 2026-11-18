package api

import (
	"encoding/json"
	"net/http"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
)

// powerWords sind die Wörter, die zur Bestätigung eingegeben werden müssen.
var powerWords = map[string]string{"reboot": "NEUSTARTEN", "poweroff": "HERUNTERFAHREN"}

func (s *server) registerSystemSettings(mux *http.ServeMux) {
	m := s.deps.SystemSettings
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }
	mux.HandleFunc("GET /api/v1/system/settings", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Get(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	})
	mux.HandleFunc("GET /api/v1/system/timezones", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Timezones(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	})
	mux.HandleFunc("PUT /api/v1/system/settings", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		change := map[string]any{}
		for _, k := range []string{"hostname", "timezone", "ntp", "ntpServers"} {
			if v, ok := in[k]; ok {
				change[k] = v
			}
		}
		if err := m.Set(r.Context(), actor(r), change); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/system/power", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Action  string `json:"action"`
			Confirm string `json:"confirm"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		word, ok := powerWords[in.Action]
		if !ok {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Unbekannte Aktion"))
			return
		}
		if in.Confirm != word {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Zur Bestätigung muss das Wort „"+word+"“ eingegeben werden"))
			return
		}
		if err := m.Power(r.Context(), actor(r), in.Action); err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
