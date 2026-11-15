// Package rpc ist das zeilenbasierte JSON-Protokoll zwischen API und Agent über einen Unix-Socket.
// Jede Verbindung trägt genau einen Aufruf oder ein Abonnement.
package rpc

import (
	"encoding/json"

	"github.com/betternas/betternas/internal/apierr"
)

// MaxLine ist die maximale Größe einer Nachricht.
const MaxLine = 1 << 20

// Request ist ein Aufruf.
type Request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response ist die Antwort auf einen Aufruf.
type Response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *apierr.Error   `json:"error,omitempty"`
}

// Event ist eine Nachricht eines Abonnements.
type Event struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
}
