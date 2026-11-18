package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)

func (s *storageAPI) putIcon(path, contentType string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest("PUT", path, strings.NewReader(string(body)))
	r.RemoteAddr = "192.168.1.10:1"
	r.AddCookie(s.cookie)
	r.Header.Set("X-CSRF-Token", s.csrf)
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	s.h.ServeHTTP(w, r)
	return w
}

func TestStackIconUploadAndServe(t *testing.T) {
	s := newDockerAPI(t)
	if w := s.do("GET", "/api/v1/stacks/media/icon", ""); w.Code != 404 {
		t.Fatalf("ohne Icon: %d", w.Code)
	}
	if w := s.putIcon("/api/v1/stacks/media/icon", "image/png", pngBytes); w.Code != 204 {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	w := s.do("GET", "/api/v1/stacks/media/icon", "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("abruf: %d %v", w.Code, w.Header())
	}
	if l := s.do("GET", "/api/v1/stacks", ""); !strings.Contains(l.Body.String(), `"icon":"`) {
		t.Fatalf("Liste ohne Icon-Version: %s", l.Body)
	}
	if w := s.do("DELETE", "/api/v1/stacks/media/icon", ""); w.Code != 204 {
		t.Fatalf("löschen: %d", w.Code)
	}
	if w := s.do("GET", "/api/v1/stacks/media/icon", ""); w.Code != 404 {
		t.Fatalf("nach Löschen: %d", w.Code)
	}
}

func TestStackIconRejectsNonImagesAndOversize(t *testing.T) {
	s := newDockerAPI(t)
	if w := s.putIcon("/api/v1/stacks/media/icon", "image/png", []byte("<html><script>alert(1)</script></html>")); w.Code != 400 {
		t.Fatalf("HTML muss abgelehnt werden: %d", w.Code)
	}
	big := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 600<<10)...)
	if w := s.putIcon("/api/v1/stacks/media/icon", "image/png", big); w.Code != 400 {
		t.Fatalf("zu groß: %d", w.Code)
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"></svg>`
	if w := s.putIcon("/api/v1/stacks/media/icon", "image/svg+xml", []byte(svg)); w.Code != 204 {
		t.Fatalf("SVG: %d %s", w.Code, w.Body)
	}
}

func TestStackIconFromURL(t *testing.T) {
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok.png" {
			w.Write(pngBytes)
			return
		}
		http.NotFound(w, r)
	}))
	defer img.Close()
	s := newDockerAPI(t)
	if w := s.putIcon("/api/v1/stacks/media/icon", "application/json", []byte(`{"url":"`+img.URL+`/ok.png"}`)); w.Code != 204 {
		t.Fatalf("url: %d %s", w.Code, w.Body)
	}
	if w := s.do("GET", "/api/v1/stacks/media/icon", ""); w.Code != 200 {
		t.Fatalf("abruf: %d", w.Code)
	}
	for _, bad := range []string{`{"url":"` + img.URL + `/fehlt.png"}`, `{"url":"file:///etc/passwd"}`, `{"url":"ftp://x/y.png"}`} {
		if w := s.putIcon("/api/v1/stacks/media/icon", "application/json", []byte(bad)); w.Code != 400 {
			t.Fatalf("%s sollte abgelehnt werden: %d", bad, w.Code)
		}
	}
}

func TestStackIconRemovedWithStack(t *testing.T) {
	s := newDockerAPI(t)
	s.putIcon("/api/v1/stacks/media/icon", "image/png", pngBytes)
	if w := s.do("DELETE", "/api/v1/stacks/media", ""); w.Code != 204 {
		t.Fatalf("stack löschen: %d", w.Code)
	}
	if w := s.do("GET", "/api/v1/stacks/media/icon", ""); w.Code != 404 {
		t.Fatalf("Icon blieb: %d", w.Code)
	}
}
