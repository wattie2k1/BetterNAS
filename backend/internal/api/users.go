package api

import (
	"net/http"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/users"
)

func (s *server) registerUsers(mux *http.ServeMux) {
	m := s.deps.Users
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }
	done := func(w http.ResponseWriter, status int, body any, err error) {
		switch {
		case err != nil:
			apierr.Write(w, err)
		case body == nil:
			w.WriteHeader(status)
		default:
			writeJSON(w, status, body)
		}
	}

	mux.HandleFunc("GET /api/v1/users", func(w http.ResponseWriter, r *http.Request) {
		a, err := m.List(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.Users)
	})
	mux.HandleFunc("POST /api/v1/users", func(w http.ResponseWriter, r *http.Request) {
		var in users.UserInput
		if !decodeBody(w, r, &in) {
			return
		}
		done(w, http.StatusCreated, map[string]string{"name": in.Name}, m.CreateUser(r.Context(), actor(r), in))
	})
	mux.HandleFunc("PATCH /api/v1/users/{name}", func(w http.ResponseWriter, r *http.Request) {
		var in users.UserUpdate
		if !decodeBody(w, r, &in) {
			return
		}
		done(w, http.StatusNoContent, nil, m.UpdateUser(r.Context(), actor(r), r.PathValue("name"), in))
	})
	mux.HandleFunc("PUT /api/v1/users/{name}/password", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Password string `json:"password"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		done(w, http.StatusNoContent, nil, m.SetPassword(r.Context(), actor(r), r.PathValue("name"), in.Password))
	})
	mux.HandleFunc("DELETE /api/v1/users/{name}", func(w http.ResponseWriter, r *http.Request) {
		done(w, http.StatusNoContent, nil, m.DeleteUser(r.Context(), actor(r), r.PathValue("name")))
	})

	mux.HandleFunc("GET /api/v1/groups", func(w http.ResponseWriter, r *http.Request) {
		a, err := m.List(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.Groups)
	})
	mux.HandleFunc("POST /api/v1/groups", func(w http.ResponseWriter, r *http.Request) {
		var in users.GroupInput
		if !decodeBody(w, r, &in) {
			return
		}
		done(w, http.StatusCreated, map[string]string{"name": in.Name}, m.CreateGroup(r.Context(), actor(r), in))
	})
	mux.HandleFunc("PATCH /api/v1/groups/{name}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Members []string `json:"members"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		done(w, http.StatusNoContent, nil, m.SetMembers(r.Context(), actor(r), r.PathValue("name"), in.Members))
	})
	mux.HandleFunc("DELETE /api/v1/groups/{name}", func(w http.ResponseWriter, r *http.Request) {
		done(w, http.StatusNoContent, nil, m.DeleteGroup(r.Context(), actor(r), r.PathValue("name")))
	})
}
