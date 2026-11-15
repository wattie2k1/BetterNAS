// Package apierr definiert die einheitlichen Fehlercodes von API und Agent.
package apierr

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Fehlercodes.
const (
	InvalidParams = "invalid_params"
	Unauthorized  = "unauthorized"
	Forbidden     = "forbidden"
	NotFound      = "not_found"
	Conflict      = "conflict"
	PoolBusy      = "pool_busy"
	DiskInUse     = "disk_in_use"
	SystemDisk    = "system_disk"
	RateLimited   = "rate_limited"
	Internal      = "internal"
)

// Error ist ein Fehler mit Code, deutscher Meldung und optionalen Details.
type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// New erzeugt einen Fehler.
func New(code, message string) *Error { return &Error{Code: code, Message: message} }

// With ergänzt ein Detail und gibt den Fehler zurück.
func (e *Error) With(key string, value any) *Error {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details[key] = value
	return e
}

// From liefert den enthaltenen *Error oder einen generischen internen Fehler ohne Interna.
func From(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return New(Internal, "Interner Fehler")
}

// HTTPStatus bildet einen Fehlercode auf einen HTTP-Status ab.
func HTTPStatus(code string) int {
	switch code {
	case InvalidParams:
		return http.StatusBadRequest
	case Unauthorized:
		return http.StatusUnauthorized
	case Forbidden:
		return http.StatusForbidden
	case NotFound:
		return http.StatusNotFound
	case Conflict, PoolBusy, DiskInUse, SystemDisk:
		return http.StatusConflict
	case RateLimited:
		return http.StatusTooManyRequests
	}
	return http.StatusInternalServerError
}

// Write schreibt err im Fehlerformat {"error":{...}}.
func Write(w http.ResponseWriter, err error) {
	e := From(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(HTTPStatus(e.Code))
	_ = json.NewEncoder(w).Encode(map[string]*Error{"error": e})
}
