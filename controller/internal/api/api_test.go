package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/the-asad-iqbal/async-os/controller/internal/audit"
	"github.com/the-asad-iqbal/async-os/controller/internal/auth"
	"github.com/the-asad-iqbal/async-os/controller/internal/devices"
	"github.com/the-asad-iqbal/async-os/controller/internal/plant"
	"github.com/the-asad-iqbal/async-os/controller/internal/stats"
	"github.com/the-asad-iqbal/async-os/controller/internal/store"
)

type testEnv struct {
	t        *testing.T
	server   *httptest.Server
	auth     *auth.Service
	codePath string
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	lib, err := devices.LoadLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := plant.NewEngine(lib, plant.DefaultSite(), quiet)
	if err != nil {
		t.Fatal(err)
	}
	codePath := filepath.Join(dir, "setup-code")
	authService := auth.NewService(st, codePath)
	srv := New(Deps{
		Auth: authService, Audit: audit.New(st, quiet, 1000), Engine: engine,
		Stats: stats.NewRecorder(st, quiet), Library: lib,
		SaveSite: func(plant.Site) error { return nil },
		Web:      fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>AsyncOS</title>")}},
		Version:  "test", Log: quiet,
	})
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	return &testEnv{t: t, server: hs, auth: authService, codePath: codePath}
}

// client is a browser with its own cookie jar.
type client struct {
	env  *testEnv
	http *http.Client
}

func (e *testEnv) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{env: e, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path string, body any) (int, []byte) {
	c.env.t.Helper()
	var r io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, c.env.server.URL+path, r)
	req.Header.Set("X-AsyncOS-CSRF", "1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		c.env.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

const ownerPassword = "owner password 123"

func (e *testEnv) setupOwner() *client {
	e.t.Helper()
	if err := e.auth.EnsureSetupCode(); err != nil {
		e.t.Fatal(err)
	}
	code, _ := os.ReadFile(e.codePath)
	c := e.client()
	status, body := c.do("POST", "/api/v1/setup", map[string]string{
		"code": strings.TrimSpace(string(code)), "username": "owner", "password": ownerPassword,
	})
	if status != http.StatusCreated {
		e.t.Fatalf("setup: %d %s", status, body)
	}
	return c
}

func (e *testEnv) userClient(owner *client, username string, role auth.Role) *client {
	e.t.Helper()
	password := username + " password 123"
	if status, body := owner.do("POST", "/api/v1/users", map[string]any{"username": username, "role": role, "password": password}); status != http.StatusCreated {
		e.t.Fatalf("create %s: %d %s", username, status, body)
	}
	c := e.client()
	if status, body := c.do("POST", "/api/v1/auth/login", map[string]string{"username": username, "password": password}); status != http.StatusOK {
		e.t.Fatalf("login %s: %d %s", username, status, body)
	}
	return c
}

func TestSetupFlow(t *testing.T) {
	env := newEnv(t)
	c := env.client()

	if status, body := c.do("GET", "/api/v1/setup", nil); status != 200 || !strings.Contains(string(body), `"needed":true`) {
		t.Fatalf("setup status: %d %s", status, body)
	}
	env.auth.EnsureSetupCode()
	if status, _ := c.do("POST", "/api/v1/setup", map[string]string{"code": "AAAA-BBBB-CCCC", "username": "owner", "password": ownerPassword}); status != http.StatusBadRequest {
		t.Fatalf("wrong code accepted: %d", status)
	}
	owner := env.setupOwner()
	if status, body := owner.do("GET", "/api/v1/auth/me", nil); status != 200 || !strings.Contains(string(body), `"role":"owner"`) {
		t.Fatalf("me: %d %s", status, body)
	}
	if status, body := c.do("GET", "/api/v1/setup", nil); !strings.Contains(string(body), `"needed":false`) {
		t.Fatalf("setup still needed: %d %s", status, body)
	}
}

func TestCSRFHeaderRequired(t *testing.T) {
	env := newEnv(t)
	req, _ := http.NewRequest("POST", env.server.URL+"/api/v1/auth/login", strings.NewReader(`{"username":"x","password":"y"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST without CSRF header: %d", resp.StatusCode)
	}
}

func TestSessionCookieAndSecurityHeaders(t *testing.T) {
	env := newEnv(t)
	env.setupOwner()
	req, _ := http.NewRequest("POST", env.server.URL+"/api/v1/auth/login", strings.NewReader(`{"username":"owner","password":"`+ownerPassword+`"}`))
	req.Header.Set("X-AsyncOS-CSRF", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	cookie := resp.Header.Get("Set-Cookie")
	for _, want := range []string{"asyncos_session=", "HttpOnly", "SameSite=Strict"} {
		if !strings.Contains(cookie, want) {
			t.Errorf("cookie %q lacks %s", cookie, want)
		}
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP = %q", csp)
	}
	if resp.Header.Get("X-Frame-Options") != "DENY" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Error("missing security headers")
	}
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	env := newEnv(t)
	c := env.client()
	for _, path := range []string{"/api/v1/snapshot", "/api/v1/site", "/api/v1/users", "/api/v1/audit"} {
		if status, _ := c.do("GET", path, nil); status != http.StatusUnauthorized {
			t.Errorf("GET %s without a session: %d", path, status)
		}
	}
}

// TestRolePermissionMatrix checks every protected endpoint against every
// role: allowed requests must not be refused for permission, forbidden
// ones must get 403.
func TestRolePermissionMatrix(t *testing.T) {
	env := newEnv(t)
	owner := env.setupOwner()
	clients := map[auth.Role]*client{
		auth.RoleOwner:     owner,
		auth.RoleInstaller: env.userClient(owner, "installer", auth.RoleInstaller),
		auth.RoleOperator:  env.userClient(owner, "operator", auth.RoleOperator),
		auth.RoleViewer:    env.userClient(owner, "viewer", auth.RoleViewer),
	}
	site := plant.DefaultSite()
	endpoints := []struct {
		method, path string
		body         any
		perm         auth.Permission
	}{
		{"GET", "/api/v1/snapshot", nil, auth.PermDashboardView},
		{"GET", "/api/v1/energy/summary", nil, auth.PermEnergyView},
		{"GET", "/api/v1/site", nil, auth.PermDevicesView},
		{"GET", "/api/v1/profiles", nil, auth.PermDevicesView},
		{"GET", "/api/v1/alarms", nil, auth.PermAlarmsView},
		{"POST", "/api/v1/alarms/ack", map[string]string{"key": "none"}, auth.PermAlarmsAck},
		{"PUT", "/api/v1/site/export-limit", map[string]float64{"export_limit_w": 0}, auth.PermControlSetpoint},
		{"PUT", "/api/v1/site/control", site.Control, auth.PermControlConfigure},
		{"PUT", "/api/v1/site", map[string]any{"name": "Site", "timezone": "UTC", "buses": []any{}, "devices": []any{}}, auth.PermSiteConfigure},
		{"POST", "/api/v1/commissioning/read", map[string]any{"bus": map[string]any{"id": "x", "transport": "tcp", "host": "127.0.0.1", "port": 1, "timeout_ms": 100}, "unit_id": 1, "table": "holding", "address": 0, "quantity": 1}, auth.PermCommissioning},
		{"GET", "/api/v1/audit", nil, auth.PermAuditView},
		{"GET", "/api/v1/users", nil, auth.PermUsersManage},
	}
	for _, ep := range endpoints {
		for role, c := range clients {
			status, body := c.do(ep.method, ep.path, ep.body)
			allowed := role.Can(ep.perm)
			if allowed && (status == http.StatusForbidden || status == http.StatusUnauthorized) {
				t.Errorf("%s %s as %s: refused (%d %s)", ep.method, ep.path, role, status, body)
			}
			if !allowed && status != http.StatusForbidden {
				t.Errorf("%s %s as %s: got %d, want 403", ep.method, ep.path, role, status)
			}
		}
	}
}

func TestOperatorExportLimitIsBounded(t *testing.T) {
	env := newEnv(t)
	owner := env.setupOwner()
	installer := env.userClient(owner, "installer", auth.RoleInstaller)
	operator := env.userClient(owner, "operator", auth.RoleOperator)

	if status, body := installer.do("PUT", "/api/v1/site", map[string]any{
		"name": "Site", "timezone": "UTC", "buses": []any{}, "devices": []any{}, "operator_export_limit_max_w": 5000,
	}); status != 200 {
		t.Fatalf("installer set bound: %d %s", status, body)
	}
	if status, body := operator.do("PUT", "/api/v1/site/export-limit", map[string]float64{"export_limit_w": 3000}); status != 200 {
		t.Fatalf("within bound: %d %s", status, body)
	}
	if status, _ := operator.do("PUT", "/api/v1/site/export-limit", map[string]float64{"export_limit_w": 8000}); status != http.StatusForbidden {
		t.Fatalf("above bound: %d", status)
	}
	if status, _ := installer.do("PUT", "/api/v1/site/export-limit", map[string]float64{"export_limit_w": 8000}); status != 200 {
		t.Fatalf("installer above operator bound: %d", status)
	}
}

func TestResponsesNeverLeakSecrets(t *testing.T) {
	env := newEnv(t)
	owner := env.setupOwner()
	env.userClient(owner, "viewer", auth.RoleViewer)
	_, body := owner.do("GET", "/api/v1/users", nil)
	if strings.Contains(string(body), "password") || strings.Contains(string(body), "pbkdf2") {
		t.Fatalf("user list leaks secrets: %s", body)
	}
}

func TestChangesAreAudited(t *testing.T) {
	env := newEnv(t)
	owner := env.setupOwner()
	env.userClient(owner, "operator", auth.RoleOperator)
	_, body := owner.do("GET", "/api/v1/audit", nil)
	var entries []audit.Entry
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Action] = true
	}
	for _, want := range []string{"setup.complete", "user.create", "auth.login"} {
		if !seen[want] {
			t.Errorf("no %s entry in %v", want, seen)
		}
	}
}

func TestInvalidSiteIsRejected(t *testing.T) {
	env := newEnv(t)
	owner := env.setupOwner()
	status, body := owner.do("PUT", "/api/v1/site", map[string]any{
		"name": "Site", "timezone": "UTC",
		"buses":   []any{map[string]any{"id": "bus", "transport": "rtu", "device": "/dev/sda", "baud_rate": 9600, "parity": "N", "stop_bits": 1, "timeout_ms": 500}},
		"devices": []any{},
	})
	if status != http.StatusBadRequest || !strings.Contains(string(body), "serial device") {
		t.Fatalf("non-serial device path accepted: %d %s", status, body)
	}
	if status, _ := owner.do("PUT", "/api/v1/site", map[string]any{"name": "Site", "unknown_field": 1}); status != http.StatusBadRequest {
		t.Fatalf("unknown field accepted: %d", status)
	}
}

func TestUIIsServedForClientRoutes(t *testing.T) {
	env := newEnv(t)
	resp, err := http.Get(env.server.URL + "/energy")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "AsyncOS") {
		t.Fatalf("UI route: %d %s", resp.StatusCode, body)
	}
	if status, _ := env.client().do("GET", "/api/v1/nope", nil); status != http.StatusNotFound {
		t.Fatalf("unknown API path: %d", status)
	}
}
