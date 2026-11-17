package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/betternas/betternas/internal/audit"
)

func (s *storageAPI) formatAs(t *testing.T, id, serial, role string) {
	t.Helper()
	w := s.do("POST", "/api/v1/disks/"+id+"/format/preview", "")
	var p struct{ ConfirmToken string }
	json.Unmarshal(w.Body.Bytes(), &p)
	w = s.do("POST", "/api/v1/disks/"+id+"/format", `{"fsType":"ext4","role":"`+role+`","confirmSerial":"`+serial+`","confirmToken":"`+p.ConfirmToken+`"}`)
	if w.Code != 202 {
		t.Fatalf("format %d %s", w.Code, w.Body)
	}
	s.waitJob(t, w.Body.Bytes())
}

func TestSnapraidRoutes(t *testing.T) {
	s := newStorageAPI(t)
	s.formatAs(t, "d1", "S1", "data")
	s.formatAs(t, "d2", "S2", "parity")
	w := s.do("POST", "/api/v1/pools", `{"name":"media","branches":["d1"]}`)
	s.waitJob(t, w.Body.Bytes())

	// vor dem Einrichten
	if w := s.do("POST", "/api/v1/pools/media/snapraid/sync", `{}`); w.Code != 409 {
		t.Fatalf("sync before enable %d %s", w.Code, w.Body)
	}
	if w := s.do("PUT", "/api/v1/pools/media/snapraid", `{"parity":[]}`); w.Code != 400 {
		t.Fatalf("no parity %d %s", w.Code, w.Body)
	}
	if w := s.do("PUT", "/api/v1/pools/nope/snapraid", `{"parity":["d2"]}`); w.Code != 404 {
		t.Fatalf("pool %d %s", w.Code, w.Body)
	}

	w = s.do("PUT", "/api/v1/pools/media/snapraid", `{"parity":["d2"],"deleteThreshold":100}`)
	if w.Code != 202 {
		t.Fatalf("enable %d %s", w.Code, w.Body)
	}
	s.waitJob(t, w.Body.Bytes())
	if w := s.do("GET", "/api/v1/pools/media", ""); !strings.Contains(w.Body.String(), `"snapraid":{"parity":["d2"]`) || !strings.Contains(w.Body.String(), `"deleteThreshold":100`) {
		t.Fatalf("pool %s", w.Body)
	}
	if w := s.do("GET", "/api/v1/pools/media/snapraid/status", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatalf("status %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/pools/media/snapraid/foo", `{}`); w.Code != 404 && w.Code != 405 {
		t.Fatalf("unknown action %d", w.Code)
	}
	if w := s.do("POST", "/api/v1/pools/media/snapraid/diff", ``); w.Code != 202 {
		t.Fatalf("diff %d %s", w.Code, w.Body)
	}
	w = s.do("DELETE", "/api/v1/pools/media/snapraid", "")
	if w.Code != 202 {
		t.Fatalf("disable %d %s", w.Code, w.Body)
	}
}

func TestSnapraidEndpointsRequireAuthAndCSRF(t *testing.T) {
	s := newStorageAPI(t)
	for _, c := range [][2]string{{"PUT", "/api/v1/pools/media/snapraid"}, {"DELETE", "/api/v1/pools/media/snapraid"},
		{"GET", "/api/v1/pools/media/snapraid/status"}, {"POST", "/api/v1/pools/media/snapraid/sync"}} {
		r := httptest.NewRequest(c[0], c[1], strings.NewReader(`{}`))
		r.RemoteAddr = "192.168.1.10:1"
		w := httptest.NewRecorder()
		s.h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%v without session: %d", c, w.Code)
		}
		r = httptest.NewRequest(c[0], c[1], strings.NewReader(`{}`))
		r.RemoteAddr = "192.168.1.10:1"
		r.AddCookie(s.cookie)
		w = httptest.NewRecorder()
		s.h.ServeHTTP(w, r)
		if c[0] != "GET" && w.Code != http.StatusForbidden {
			t.Errorf("%v without CSRF: %d", c, w.Code)
		}
	}
}

func TestReplaceAndRecoveryRoutes(t *testing.T) {
	s := newStorageAPI(t)
	s.formatAs(t, "d1", "S1", "data")
	w := s.do("POST", "/api/v1/pools", `{"name":"media","branches":["d1"]}`)
	s.waitJob(t, w.Body.Bytes())

	replace := func(pool, body string) *httptest.ResponseRecorder {
		return s.do("POST", "/api/v1/pools/"+pool+"/snapraid/replace", body)
	}
	if w := replace("media", `{"oldDisk":"d1","newDisk":"d2"}`); w.Code != 409 {
		t.Fatalf("ohne SnapRAID: %d %s", w.Code, w.Body)
	}
	if w := replace("media", `{}`); w.Code != 400 {
		t.Fatalf("leer: %d %s", w.Code, w.Body)
	}
	if w := replace("media", `{"oldDisk":"d1","newDisk":"d1"}`); w.Code != 400 {
		t.Fatalf("gleiche Platte: %d %s", w.Code, w.Body)
	}
	if w := replace("nope", `{"oldDisk":"d1","newDisk":"d2"}`); w.Code != 404 {
		t.Fatalf("unbekannter Pool: %d %s", w.Code, w.Body)
	}
	if w := s.do("POST", "/api/v1/pools/media/snapraid/recovery/retry", ""); w.Code != 409 {
		t.Fatalf("retry ohne SnapRAID: %d %s", w.Code, w.Body)
	}
	if w := s.do("DELETE", "/api/v1/pools/media/snapraid/recovery", ""); w.Code != 204 {
		t.Fatalf("abbrechen: %d %s", w.Code, w.Body)
	}
	if w := s.do("DELETE", "/api/v1/pools/nope/snapraid/recovery", ""); w.Code != 404 {
		t.Fatalf("abbrechen, Pool unbekannt: %d", w.Code)
	}
	list, _ := audit.List(context.Background(), s.db, 50)
	found := false
	for _, a := range list {
		if a.Action == "snapraid.recovery.cancel" && a.Target == "media" && a.Actor == "anna" {
			found = true
		}
	}
	if !found {
		t.Fatal("kein Audit-Eintrag")
	}
}

func TestReplaceRoutesRequireAuthAndCSRF(t *testing.T) {
	s := newStorageAPI(t)
	for _, c := range [][2]string{{"POST", "/api/v1/pools/media/snapraid/replace"}, {"POST", "/api/v1/pools/media/snapraid/recovery/retry"}, {"DELETE", "/api/v1/pools/media/snapraid/recovery"}} {
		r := httptest.NewRequest(c[0], c[1], strings.NewReader(`{}`))
		r.RemoteAddr = "192.168.1.10:1"
		w := httptest.NewRecorder()
		s.h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%v ohne Sitzung: %d", c, w.Code)
		}
		r = httptest.NewRequest(c[0], c[1], strings.NewReader(`{}`))
		r.RemoteAddr = "192.168.1.10:1"
		r.AddCookie(s.cookie)
		w = httptest.NewRecorder()
		s.h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%v ohne CSRF: %d", c, w.Code)
		}
	}
}
