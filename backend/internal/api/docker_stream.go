package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/docker"
	"github.com/betternas/betternas/internal/rpc"
)

const maxWSMessage = 64 << 10

func queryInt(r *http.Request, key string) (int, bool, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return 0, false, nil
	}
	n, err := strconv.Atoi(v)
	return n, true, err
}

// eventBytes liefert den Inhalt eines „data“-Ereignisses.
func eventBytes(ev rpc.Event) []byte {
	var b []byte
	_ = json.Unmarshal(ev.Data, &b)
	return b
}

func eventText(ev rpc.Event) string {
	var s string
	_ = json.Unmarshal(ev.Data, &s)
	return s
}

func (s *server) registerDockerStreams(mux *http.ServeMux) {
	m := s.deps.Docker
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }

	mux.HandleFunc("GET /api/v1/containers/stats", func(w http.ResponseWriter, r *http.Request) {
		list, err := m.Stats(r.Context())
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	})

	// Logs: Textframes mit dem Protokoll, bis der Client die Verbindung schließt oder der Container endet.
	mux.HandleFunc("GET /api/v1/containers/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !docker.ContainerIDRe.MatchString(id) {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Container-ID"))
			return
		}
		params := map[string]any{"id": id}
		if n, ok, err := queryInt(r, "tail"); err != nil || (ok && (n < 0 || n > 5000)) {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Zeilenzahl (0 bis 5000)"))
			return
		} else if ok {
			params["tail"] = n
		}
		s.bridge(w, r, "container.logs", params, false)
	})

	// Shell: Binärframes = Terminalein-/ausgabe, Textframes = JSON-Steuerung ({"type":"resize","rows":…,"cols":…}).
	mux.HandleFunc("GET /api/v1/containers/{id}/shell", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !docker.ContainerIDRe.MatchString(id) {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Container-ID"))
			return
		}
		params := map[string]any{"id": id}
		for _, k := range []string{"rows", "cols"} {
			n, ok, err := queryInt(r, k)
			if err != nil || (ok && (n < 1 || n > 500)) {
				apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Terminalgröße"))
				return
			}
			if ok {
				params[k] = n
			}
		}
		m.AuditShell(r.Context(), actor(r), id)
		s.bridge(w, r, "container.shell", params, true)
	})
}

// bridge verbindet einen WebSocket mit einer Agent-Sitzung. Bei interactive gehen Eingaben des Browsers an den Agent.
func (s *server) bridge(w http.ResponseWriter, r *http.Request, method string, params any, interactive bool) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(maxWSMessage)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sess, err := s.deps.Docker.OpenSession(ctx, method, params)
	if err != nil {
		_ = c.Close(websocket.StatusInternalError, apierr.From(err).Message)
		return
	}
	defer sess.Close()

	// Browser → Agent (Lesen erkennt auch das Ende der Verbindung).
	go func() {
		defer cancel()
		for {
			typ, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			if !interactive {
				continue
			}
			switch typ {
			case websocket.MessageBinary:
				raw, _ := json.Marshal(b)
				if sess.Send(rpc.Event{Event: "input", Data: raw}) != nil {
					return
				}
			case websocket.MessageText:
				var ctl struct {
					Type       string `json:"type"`
					Rows, Cols int
				}
				if json.Unmarshal(b, &ctl) == nil && ctl.Type == "resize" {
					raw, _ := json.Marshal(map[string]int{"rows": ctl.Rows, "cols": ctl.Cols})
					if sess.Send(rpc.Event{Event: "resize", Data: raw}) != nil {
						return
					}
				}
			}
		}
	}()

	typ := websocket.MessageText
	if interactive {
		typ = websocket.MessageBinary
	}
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	write := func(t websocket.MessageType, b []byte) error {
		wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
		defer wcancel()
		return c.Write(wctx, t, b)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		case ev, ok := <-sess.Events():
			if !ok {
				_ = c.Close(websocket.StatusNormalClosure, "")
				return
			}
			switch ev.Event {
			case "data":
				if write(typ, eventBytes(ev)) != nil {
					return
				}
			case "error":
				_ = write(websocket.MessageText, []byte("[Fehler] "+eventText(ev)))
				_ = c.Close(websocket.StatusInternalError, "Fehler")
				return
			case "exit":
				_ = c.Close(websocket.StatusNormalClosure, "beendet")
				return
			}
		}
	}
}
