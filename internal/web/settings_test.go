package web

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/leganck/wakehub/internal/clientlink"
	"github.com/leganck/wakehub/internal/config"
	"github.com/leganck/wakehub/internal/device"
)

func newTestServer(t *testing.T, enableAuth bool) *Server {
	t.Helper()
	store, err := config.Open(filepath.Join(t.TempDir(), "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = store.UpdateSettings(func(s *config.Settings) error {
		s.BasicAuthEnable = enableAuth
		s.BasicAuthUser = "admin"
		s.BasicAuthPassword = "admin"
		s.ClientToken = "fixed-token"
		return nil
	})
	hub := clientlink.NewHub(store.Settings().ClientToken)
	svc := device.NewService(store, hub)
	return New(store, svc, hub, nil)
}

func withBasic(req *http.Request, user, pass string) {
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
}

func TestSettingsGETMasksPassword(t *testing.T) {
	srv := newTestServer(t, true)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	withBasic(req, "admin", "admin")
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("code %d body %s", rr.Code, rr.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["basicAuthPassword"]; ok {
		t.Fatal("must not return basicAuthPassword")
	}
	if m["basicAuthPasswordSet"] != true {
		t.Fatalf("passwordSet: %v", m["basicAuthPasswordSet"])
	}
	if m["clientToken"] != "fixed-token" {
		t.Fatalf("token: %v", m["clientToken"])
	}
}

func TestSettingsPUTKeepsPasswordWhenEmpty(t *testing.T) {
	srv := newTestServer(t, true)
	body := map[string]any{
		"listen":          ":8080",
		"bemfaUID":        "",
		"clientToken":     "fixed-token",
		"wsPath":          "/api/ws/client",
		"basicAuthEnable": true,
		"basicAuthUser":   "admin",
		// omit / empty password
		"basicAuthPassword": "",
	}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(b))
	withBasic(req, "admin", "admin")
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("code %d %s", rr.Code, rr.Body.String())
	}
	// still accepts admin:admin
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	withBasic(req2, "admin", "admin")
	srv.Handler().ServeHTTP(rr2, req2)
	if rr2.Code != 200 {
		t.Fatalf("auth broken after empty password put: %d", rr2.Code)
	}
}

func TestSettingsPUTReadonlyWhenLuciManaged(t *testing.T) {
	srv := newTestServer(t, true)
	_ = srv.store.UpdateSettings(func(s *config.Settings) error {
		s.GlobalManagedByLuci = true
		s.WebGlobalMode = config.WebGlobalModeReadonly
		return nil
	})
	body := map[string]any{
		"listen": ":9999", "clientToken": "fixed-token", "wsPath": "/api/ws/client",
		"basicAuthEnable": true, "basicAuthUser": "admin",
	}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(b))
	withBasic(req, "admin", "admin")
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatalf("want 403, got %d %s", rr.Code, rr.Body.String())
	}
}

func TestSettingsPUTUpdatesPassword(t *testing.T) {
	srv := newTestServer(t, true)
	body := map[string]any{
		"listen":             ":8080",
		"clientToken":        "fixed-token",
		"wsPath":             "/api/ws/client",
		"basicAuthEnable":    true,
		"basicAuthUser":      "admin",
		"basicAuthPassword":  "newpass",
	}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(b))
	withBasic(req, "admin", "admin")
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("code %d %s", rr.Code, rr.Body.String())
	}
	// old pass fails
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	withBasic(req2, "admin", "admin")
	srv.Handler().ServeHTTP(rr2, req2)
	if rr2.Code != 401 {
		t.Fatalf("old pass should fail, got %d", rr2.Code)
	}
	// new pass ok
	rr3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	withBasic(req3, "admin", "newpass")
	srv.Handler().ServeHTTP(rr3, req3)
	if rr3.Code != 200 {
		t.Fatalf("new pass should work, got %d", rr3.Code)
	}
}
