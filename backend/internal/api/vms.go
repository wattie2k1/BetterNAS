package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/betternas/betternas/internal/apierr"
	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/vm"
)

const maxVMBody = 64 << 10

func (s *server) registerVMs(mux *http.ServeMux) {
	m := s.deps.VMs
	actor := func(r *http.Request) string { return auth.SessionFrom(r.Context()).Username }
	reply := func(w http.ResponseWriter, v any, err error) {
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
	noContent := func(w http.ResponseWriter, err error) {
		if err != nil {
			apierr.Write(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}

	mux.HandleFunc("GET /api/v1/vms/status", func(w http.ResponseWriter, r *http.Request) {
		st, err := m.Status(r.Context())
		reply(w, st, err)
	})
	mux.HandleFunc("GET /api/v1/vms/networks", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Networks(r.Context())
		reply(w, v, err)
	})
	mux.HandleFunc("GET /api/v1/vms/isos", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.ISOs(r.Context())
		reply(w, v, err)
	})
	mux.HandleFunc("GET /api/v1/vms/interfaces", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Interfaces(r.Context())
		reply(w, v, err)
	})
	mux.HandleFunc("PUT /api/v1/vms/isos/{file}", func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > vm.MaxUploadBytes {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Die Datei ist größer als 50 GiB"))
			return
		}
		body := http.MaxBytesReader(w, r.Body, vm.MaxUploadBytes)
		noContent(w, m.UploadISO(r.Context(), actor(r), r.PathValue("file"), body))
	})
	mux.HandleFunc("DELETE /api/v1/vms/isos/{file}", func(w http.ResponseWriter, r *http.Request) {
		noContent(w, m.DeleteISO(r.Context(), actor(r), r.PathValue("file")))
	})
	mux.HandleFunc("GET /api/v1/vms/{name}/snapshots", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Snapshots(r.Context(), r.PathValue("name"))
		reply(w, v, err)
	})
	mux.HandleFunc("POST /api/v1/vms/{name}/snapshots", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxVMBody)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		if err := m.SnapshotAction(r.Context(), actor(r), "create", r.PathValue("name"), in.Name); err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"name": in.Name})
	})
	mux.HandleFunc("POST /api/v1/vms/{name}/snapshots/{snap}/revert", func(w http.ResponseWriter, r *http.Request) {
		noContent(w, m.SnapshotAction(r.Context(), actor(r), "revert", r.PathValue("name"), r.PathValue("snap")))
	})
	mux.HandleFunc("DELETE /api/v1/vms/{name}/snapshots/{snap}", func(w http.ResponseWriter, r *http.Request) {
		noContent(w, m.SnapshotAction(r.Context(), actor(r), "delete", r.PathValue("name"), r.PathValue("snap")))
	})
	mux.HandleFunc("GET /api/v1/vms/{name}/config", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Config(r.Context(), r.PathValue("name"))
		reply(w, v, err)
	})
	mux.HandleFunc("POST /api/v1/vms/{name}/config", func(w http.ResponseWriter, r *http.Request) {
		var in vm.ConfigUpdate
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxVMBody)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		noContent(w, m.Configure(r.Context(), actor(r), r.PathValue("name"), in))
	})
	mux.HandleFunc("GET /api/v1/vms/usb", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.USBDevices(r.Context())
		reply(w, v, err)
	})
	mux.HandleFunc("GET /api/v1/vms/{name}/disks", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Disks(r.Context(), r.PathValue("name"))
		reply(w, v, err)
	})
	mux.HandleFunc("POST /api/v1/vms/{name}/live", func(w http.ResponseWriter, r *http.Request) {
		var in vm.LiveUpdate
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxVMBody)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		noContent(w, m.Live(r.Context(), actor(r), r.PathValue("name"), in))
	})
	mux.HandleFunc("POST /api/v1/vms/{name}/rename", func(w http.ResponseWriter, r *http.Request) {
		var in vm.Rename
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxVMBody)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		noContent(w, m.Rename(r.Context(), actor(r), r.PathValue("name"), in))
	})
	mux.HandleFunc("POST /api/v1/vms/{name}/disks", func(w http.ResponseWriter, r *http.Request) {
		var in vm.DiskAdd
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxVMBody)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		file, err := m.AddDisk(r.Context(), actor(r), r.PathValue("name"), in)
		if err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"path": file})
	})
	mux.HandleFunc("POST /api/v1/vms/{name}/disks/remove", func(w http.ResponseWriter, r *http.Request) {
		var in vm.DiskRemove
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxVMBody)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		noContent(w, m.RemoveDisk(r.Context(), actor(r), r.PathValue("name"), in))
	})
	mux.HandleFunc("POST /api/v1/vms/{name}/disks/resize", func(w http.ResponseWriter, r *http.Request) {
		var in vm.DiskResize
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxVMBody)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		noContent(w, m.ResizeDisk(r.Context(), actor(r), r.PathValue("name"), in))
	})
	mux.HandleFunc("POST /api/v1/vms/{name}/clone", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxVMBody)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		if err := m.Clone(r.Context(), actor(r), r.PathValue("name"), in.Name); err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"name": in.Name})
	})
	// Konsole: binäre WebSocket-Frames ↔ VNC-Verbindung (RFB) auf 127.0.0.1 (noVNC-Protokoll „binary“).
	mux.HandleFunc("GET /api/v1/vms/{name}/console", func(w http.ResponseWriter, r *http.Request) {
		port, err := m.ConsolePort(r.Context(), actor(r), r.PathValue("name"))
		if err != nil {
			apierr.Write(w, err)
			return
		}
		proxyVNC(w, r, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	})
	mux.HandleFunc("GET /api/v1/vms", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.List(r.Context())
		reply(w, v, err)
	})
	mux.HandleFunc("POST /api/v1/vms", func(w http.ResponseWriter, r *http.Request) {
		var in vm.CreateInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxVMBody)).Decode(&in); err != nil {
			apierr.Write(w, apierr.New(apierr.InvalidParams, "Ungültige Anfrage"))
			return
		}
		if err := m.Create(r.Context(), actor(r), in); err != nil {
			apierr.Write(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"name": in.Name})
	})
	for action := range vm.Actions {
		mux.HandleFunc("POST /api/v1/vms/{name}/"+action, func(w http.ResponseWriter, r *http.Request) {
			noContent(w, m.Action(r.Context(), actor(r), r.PathValue("name"), action))
		})
	}
	mux.HandleFunc("DELETE /api/v1/vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		noContent(w, m.Delete(r.Context(), actor(r), r.PathValue("name"), r.URL.Query().Get("disks") == "1"))
	})
}

// proxyVNC verbindet einen WebSocket mit einer lokalen VNC-Verbindung.
func proxyVNC(w http.ResponseWriter, r *http.Request, addr string) {
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(r.Context(), "tcp", addr)
	if err != nil {
		apierr.Write(w, apierr.New(apierr.Internal, "Die VNC-Anzeige ist nicht erreichbar"))
		return
	}
	defer conn.Close()
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"binary"}})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() { // Browser → VNC
		defer cancel()
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			if _, err := conn.Write(b); err != nil {
				return
			}
		}
	}()
	go func() { // VNC → Browser
		defer cancel()
		buf := make([]byte, 32<<10)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
				werr := c.Write(wctx, websocket.MessageBinary, buf[:n])
				wcancel()
				if werr != nil {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					_ = c.Close(websocket.StatusInternalError, "VNC-Verbindung verloren")
				}
				return
			}
		}
	}()
	<-ctx.Done()
	_ = c.Close(websocket.StatusNormalClosure, "")
}
