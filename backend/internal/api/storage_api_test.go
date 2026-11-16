package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/betternas/betternas/internal/auth"
	"github.com/betternas/betternas/internal/db"
	"github.com/betternas/betternas/internal/events"
	"github.com/betternas/betternas/internal/jobs"
	"github.com/betternas/betternas/internal/storage"
)

// agentStub liefert eine Datenplatte d1 und akzeptiert jedes storage.apply.
type agentStub struct{}

func (agentStub) Call(_ context.Context, method string, _, result any) error {
	var out any
	switch method {
	case "disk.list":
		out = []storage.Disk{
			{ID: "d1", Device: "/dev/sda", Serial: "S1", SizeBytes: 1e12, Manageable: true, Partitions: []storage.Partition{}},
			{ID: "d2", Device: "/dev/sdb", Serial: "S2", SizeBytes: 1e12, Manageable: true, Partitions: []storage.Partition{}},
		}
	case "disk.format":
		out = map[string]string{"fsUuid": "u1", "label": "bnas-1"}
	default:
		out = map[string]any{}
	}
	if result != nil {
		b, _ := json.Marshal(out)
		return json.Unmarshal(b, result)
	}
	return nil
}

type storageAPI struct {
	h      http.Handler
	cookie *http.Cookie
	csrf   string
	jobs   *jobs.Manager
}

func newStorageAPI(t *testing.T) *storageAPI {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hub := events.NewHub()
	jm := jobs.NewManager(d, hub)
	store := auth.NewStore(d, time.Hour)
	token, csrf, _ := store.Create(context.Background(), "anna")
	h := NewServer(Config{WebDir: t.TempDir(), Dev: true}, Deps{
		DB: d, Jobs: jm, Hub: hub,
		Auth:    auth.NewHandlers(d, store, nil, nil, false),
		Storage: storage.NewManager(d, agentStub{}, jm, hub),
	})
	return &storageAPI{h: h, cookie: &http.Cookie{Name: auth.CookieName, Value: token}, csrf: csrf, jobs: jm}
}

func (s *storageAPI) do(method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "192.168.1.10:1"
	r.AddCookie(s.cookie)
	r.Header.Set("X-CSRF-Token", s.csrf)
	w := httptest.NewRecorder()
	s.h.ServeHTTP(w, r)
	return w
}

func (s *storageAPI) waitJob(t *testing.T, body []byte) {
	t.Helper()
	var r struct{ JobID string }
	json.Unmarshal(body, &r)
	for i := 0; i < 300; i++ {
		j, _ := s.jobs.Get(context.Background(), r.JobID)
		if j.Status == jobs.StatusSucceeded {
			return
		}
		if j.Status == jobs.StatusFailed {
			t.Fatal(j.Log)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout")
}

func TestStorageRoutes(t *testing.T) {
	s := newStorageAPI(t)
	if w := s.do("GET", "/api/v1/disks", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"role":"unassigned"`) {
		t.Fatalf("disks %d %s", w.Code, w.Body)
	}
	w := s.do("POST", "/api/v1/disks/d1/format/preview", "")
	var p struct{ ConfirmToken string }
	json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != 200 || p.ConfirmToken == "" {
		t.Fatalf("preview %d %s", w.Code, w.Body)
	}
	w = s.do("POST", "/api/v1/disks/d1/format", `{"fsType":"ext4","confirmSerial":"S1","confirmToken":"`+p.ConfirmToken+`"}`)
	if w.Code != 202 || !strings.Contains(w.Body.String(), "jobId") {
		t.Fatalf("format %d %s", w.Code, w.Body)
	}
	s.waitJob(t, w.Body.Bytes())

	w = s.do("POST", "/api/v1/pools", `{"name":"media","branches":["d1"]}`)
	if w.Code != 202 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	s.waitJob(t, w.Body.Bytes())
	if w := s.do("POST", "/api/v1/pools", `{"name":"../x","branches":["d1"]}`); w.Code != 400 || !strings.Contains(w.Body.String(), `"invalid_params"`) {
		t.Fatalf("invalid %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/pools", `kaputt`); w.Code != 400 {
		t.Fatalf("bad json %d", w.Code)
	}
	if w := s.do("GET", "/api/v1/pools/media", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"path":"/srv/pools/media"`) {
		t.Fatalf("get %d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/pools/nope", ""); w.Code != 404 {
		t.Fatalf("get missing %d", w.Code)
	}
	if w := s.do("PATCH", "/api/v1/pools/media", `{"branches":["d1"],"createPolicy":"mfs"}`); w.Code != 202 {
		t.Fatalf("patch %d %s", w.Code, w.Body)
	} else {
		s.waitJob(t, w.Body.Bytes())
	}
	if w := s.do("POST", "/api/v1/disks/d1/release", ""); w.Code != 409 || !strings.Contains(w.Body.String(), `"disk_in_use"`) {
		t.Fatalf("release %d %s", w.Code, w.Body)
	}
	if w := s.do("DELETE", "/api/v1/pools/media", ""); w.Code != 202 {
		t.Fatalf("delete %d %s", w.Code, w.Body)
	}
}

func TestFormatPassesRole(t *testing.T) {
	s := newStorageAPI(t)
	w := s.do("POST", "/api/v1/disks/d1/format/preview", "")
	var p struct{ ConfirmToken string }
	json.Unmarshal(w.Body.Bytes(), &p)
	w = s.do("POST", "/api/v1/disks/d1/format", `{"fsType":"ext4","role":"parity","confirmSerial":"S1","confirmToken":"`+p.ConfirmToken+`"}`)
	if w.Code != 202 {
		t.Fatalf("format %d %s", w.Code, w.Body)
	}
	s.waitJob(t, w.Body.Bytes())
	if w := s.do("GET", "/api/v1/disks", ""); !strings.Contains(w.Body.String(), `"role":"parity"`) {
		t.Fatalf("disks %s", w.Body)
	}
	if w := s.do("POST", "/api/v1/pools", `{"name":"media","branches":["d1"]}`); w.Code != 400 {
		t.Fatalf("pool with parity disk %d %s", w.Code, w.Body)
	}
}
