package api

import (
	"net/http"
	"time"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/audit"
	"github.com/betternas/betternas/internal/auth"
)

func (s *server) registerTOTP(mux *http.ServeMux) {
	st := s.deps.TOTP
	// Fehlversuche beim Abschalten je Benutzer (eine gestohlene Sitzung soll den Code nicht durchprobieren können).
	disableLimit := auth.NewRateLimiter(5, 5*time.Minute)
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }
	record := func(r *http.Request, action, target string, err error) {
		result := "ok"
		if err != nil {
			result = "error"
		}
		_ = audit.Record(r.Context(), s.deps.DB, audit.Entry{Actor: actor(r), Action: action, Target: target, Result: result})
	}

	mux.HandleFunc("GET /api/v1/auth/totp", func(w http.ResponseWriter, r *http.Request) {
		on, err := st.Enabled(r.Context(), actor(r))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": on})
	})
	mux.HandleFunc("POST /api/v1/auth/totp/setup", func(w http.ResponseWriter, r *http.Request) {
		user := actor(r)
		secret, err := auth.NewSecret()
		if err == nil {
			err = st.BeginSetup(r.Context(), user, secret)
		}
		record(r, "auth.totp.setup", user, err)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "uri": auth.OTPAuthURI("BetterNAS", user, secret)})
	})
	mux.HandleFunc("POST /api/v1/auth/totp/enable", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Code string `json:"code"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		user := actor(r)
		secret, ok, err := st.Pending(r.Context(), user)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		if !ok {
			apierr.Write(w, apierr.New(apierr.Conflict, "Starte zuerst die Einrichtung."))
			return
		}
		step, valid := auth.VerifyTOTP(secret, in.Code, time.Now(), 0)
		if !valid {
			err := apierr.New(apierr.InvalidParams, "Der Code stimmt nicht. Prüfe die Uhrzeit deines Geräts und versuche es erneut.")
			record(r, "auth.totp.enable", user, err)
			apierr.Write(w, err)
			return
		}
		codes, err := auth.NewRecoveryCodes(10)
		if err == nil {
			hashes := make([]string, len(codes))
			for i, c := range codes {
				hashes[i] = auth.HashRecoveryCode(c)
			}
			err = st.Enable(r.Context(), user, step, hashes)
		}
		record(r, "auth.totp.enable", user, err)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
	})
	mux.HandleFunc("POST /api/v1/auth/totp/disable", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Code string `json:"code"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		user := actor(r)
		if disableLimit.Blocked(user) {
			apierr.Write(w, apierr.New(apierr.RateLimited, "Zu viele Fehlversuche. Bitte in einigen Minuten erneut versuchen."))
			return
		}
		ok, err := st.Verify(r.Context(), user, in.Code, time.Now())
		if err == nil && !ok {
			disableLimit.Fail(user)
			err = apierr.New(apierr.Forbidden, "Code ungültig")
		}
		if err == nil {
			err = st.Disable(r.Context(), user)
		}
		record(r, "auth.totp.disable", user, err)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /api/v1/users/{name}/totp", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == actor(r) {
			apierr.Write(w, apierr.New(apierr.Conflict, "Die eigene Zwei-Faktor-Anmeldung schaltest du im Profil ab."))
			return
		}
		err := st.Disable(r.Context(), name)
		record(r, "auth.totp.reset", name, err)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
