package web

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/leganck/wakehub/internal/clientlink"
	"github.com/leganck/wakehub/internal/config"
	"github.com/leganck/wakehub/internal/device"
)

func TestBasicAuth(t *testing.T) {
	dir := t.TempDir()
	store, err := config.Open(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = store.UpdateSettings(func(s *config.Settings) error {
		s.BasicAuthEnable = true
		s.BasicAuthUser = "admin"
		s.BasicAuthPassword = "admin"
		return nil
	})
	hub := clientlink.NewHub("")
	svc := device.NewService(store, hub)
	srv := New(store, svc, hub, nil)
	h := srv.Handler()

	// no auth -> 401
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}

	// wrong pass -> 401
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:wrong")))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 wrong pass, got %d", rr.Code)
	}

	// ok
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:admin")))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// WS path exempt
	_ = store.UpdateSettings(func(s *config.Settings) error {
		s.WSPath = "/api/ws/client"
		return nil
	})
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/ws/client", nil)
	// upgrade will fail but should not be 401
	h.ServeHTTP(rr, req)
	if rr.Code == http.StatusUnauthorized {
		t.Fatal("ws path should not require basic auth")
	}
}
