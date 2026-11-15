package apierr

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestHTTPStatus(t *testing.T) {
	cases := map[string]int{
		InvalidParams: 400, Unauthorized: 401, Forbidden: 403, NotFound: 404,
		Conflict: 409, PoolBusy: 409, DiskInUse: 409, SystemDisk: 409,
		RateLimited: 429, Internal: 500, "unbekannt": 500,
	}
	for code, want := range cases {
		if got := HTTPStatus(code); got != want {
			t.Errorf("%s: got %d want %d", code, got, want)
		}
	}
}

func TestWriteAPIError(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, New(PoolBusy, "Pool wird verwendet").With("unit", "srv-pools-media.mount"))
	if rec.Code != 409 || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	var body struct {
		Error Error `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "pool_busy" || body.Error.Message != "Pool wird verwendet" || body.Error.Details["unit"] != "srv-pools-media.mount" {
		t.Fatalf("%+v", body.Error)
	}
}

func TestWriteForeignErrorHidesDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, errors.New("open /etc/shadow: permission denied"))
	if rec.Code != 500 || contains(rec.Body.String(), "shadow") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestFromWraps(t *testing.T) {
	inner := New(NotFound, "x")
	wrapped := errors.Join(errors.New("ctx"), inner)
	if From(wrapped).Code != NotFound {
		t.Fatal("wrapped apierr not found")
	}
	if From(errors.New("y")).Code != Internal {
		t.Fatal("foreign error must map to internal")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
