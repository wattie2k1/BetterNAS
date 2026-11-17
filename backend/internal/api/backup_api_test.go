package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/audit"
)

func (s *storageAPI) doRaw(method, path string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.RemoteAddr = "192.168.1.10:1"
	r.AddCookie(s.cookie)
	r.Header.Set("X-CSRF-Token", s.csrf)
	w := httptest.NewRecorder()
	s.h.ServeHTTP(w, r)
	return w
}

func createBackup(t *testing.T, s *storageAPI) string {
	t.Helper()
	w := s.do("POST", "/api/v1/backups", "")
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	var info struct{ Name string }
	json.Unmarshal(w.Body.Bytes(), &info)
	return info.Name
}

func TestBackupRoutes(t *testing.T) {
	s := newStorageAPI(t)
	name := createBackup(t, s)
	if w := s.do("GET", "/api/v1/backups", ""); w.Code != 200 || !strings.Contains(w.Body.String(), name) {
		t.Fatalf("list %d %s", w.Code, w.Body)
	}
	w := s.do("GET", "/api/v1/backups/"+name, "")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") || w.Body.Len() < 100 {
		t.Fatalf("download %d %q len=%d", w.Code, w.Header().Get("Content-Disposition"), w.Body.Len())
	}
	if w := s.do("GET", "/api/v1/backups/foo.tar.gz", ""); w.Code != 400 {
		t.Fatalf("ungültiger Name %d", w.Code)
	}
	if w := s.do("DELETE", "/api/v1/backups/"+name, ""); w.Code != 204 {
		t.Fatalf("delete %d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/backups", ""); strings.Contains(w.Body.String(), name) {
		t.Fatal("nicht gelöscht")
	}
	list, _ := audit.List(context.Background(), s.db, 50)
	var acts []string
	for _, a := range list {
		acts = append(acts, a.Action)
	}
	for _, want := range []string{"backup.create", "backup.download", "backup.delete"} {
		if !strings.Contains(strings.Join(acts, " "), want) {
			t.Errorf("Audit %s fehlt (%v)", want, acts)
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/backups", nil)
	r.RemoteAddr = "192.168.1.10:1"
	w2 := httptest.NewRecorder()
	s.h.ServeHTTP(w2, r)
	if w2.Code != 401 {
		t.Fatalf("ohne Sitzung %d", w2.Code)
	}
}

func TestRestoreRoutesFlow(t *testing.T) {
	s := newStorageAPI(t)
	name := createBackup(t, s)
	dl := s.do("GET", "/api/v1/backups/"+name, "")
	// Upload-Vorschau
	w := s.doRaw("POST", "/api/v1/backups/restore/preview", dl.Body.Bytes())
	if w.Code != 200 {
		t.Fatalf("preview %d %s", w.Code, w.Body)
	}
	var p struct{ Token string }
	json.Unmarshal(w.Body.Bytes(), &p)
	if p.Token == "" {
		t.Fatal("kein Token")
	}
	if w := s.do("POST", "/api/v1/backups/restore", `{"token":"`+p.Token+`"}`); w.Code != 202 {
		t.Fatalf("restore %d %s", w.Code, w.Body)
	}
	if len(s.bk.calls) != 1 || s.bk.calls[0] != "system.restart-api" {
		t.Fatalf("Agent: %v", s.bk.calls)
	}
	// Müll → 400, unbekannter Token → 404
	if w := s.doRaw("POST", "/api/v1/backups/restore/preview", []byte("müll")); w.Code != 400 {
		t.Fatalf("müll %d", w.Code)
	}
	if w := s.do("POST", "/api/v1/backups/restore", `{"token":"x"}`); w.Code != 404 {
		t.Fatalf("token %d", w.Code)
	}
	// Vorschau aus gespeichertem Backup
	if w := s.do("POST", "/api/v1/backups/"+name+"/restore/preview", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("stored preview %d %s", w.Code, w.Body)
	}
}

func TestRestorePreviewTooLarge(t *testing.T) {
	old := maxBackupUpload
	maxBackupUpload = 64
	defer func() { maxBackupUpload = old }()
	s := newStorageAPI(t)
	name := createBackup(t, s)
	dl := s.do("GET", "/api/v1/backups/"+name, "")
	if w := s.doRaw("POST", "/api/v1/backups/restore/preview", dl.Body.Bytes()); w.Code != 400 && w.Code != 413 {
		t.Fatalf("%d", w.Code)
	}
}
