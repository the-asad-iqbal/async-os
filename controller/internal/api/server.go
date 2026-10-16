// Package api serves the controller's JSON API and web UI.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/the-asad-iqbal/async-os/controller/internal/audit"
	"github.com/the-asad-iqbal/async-os/controller/internal/auth"
	"github.com/the-asad-iqbal/async-os/controller/internal/devices"
	"github.com/the-asad-iqbal/async-os/controller/internal/plant"
	"github.com/the-asad-iqbal/async-os/controller/internal/stats"
)

const (
	sessionCookie = "asyncos_session"
	displayCookie = "asyncos_display"
	// csrfHeader must accompany every state-changing request. Browsers
	// cannot add custom headers to cross-site requests without a CORS
	// preflight, which this server never approves.
	csrfHeader  = "X-AsyncOS-CSRF"
	maxBodySize = 1 << 20
)

// Deps are the services the API exposes.
type Deps struct {
	Auth     *auth.Service
	Audit    *audit.Log
	Engine   *plant.Engine
	Stats    *stats.Recorder
	Library  *devices.Library
	SaveSite func(plant.Site) error
	Reboot   func() error
	Web      fs.FS
	Version  string
	Log      *slog.Logger
	// LocalDisplay grants read-only display access to loopback requests
	// without a session: the kiosk browser on the device's own screen.
	// Off unless that kiosk is installed.
	LocalDisplay bool
}

// Server is the HTTP handler.
type Server struct {
	Deps
	started time.Time
	mux     *http.ServeMux

	acksMu sync.Mutex
	acks   map[string]Ack
	// siteMu serializes configuration changes.
	siteMu sync.Mutex
}

// Ack records who acknowledged an alarm.
type Ack struct {
	By   string    `json:"by"`
	Time time.Time `json:"time"`
}

// New builds the server.
func New(d Deps) *Server {
	s := &Server{Deps: d, started: time.Now(), mux: http.NewServeMux(), acks: map[string]Ack{}}
	s.routes()
	return s
}

type handlerFunc func(w http.ResponseWriter, r *http.Request, u *auth.User) error

func (s *Server) routes() {
	// Public.
	s.mux.HandleFunc("GET /api/v1/health", s.public(s.health))
	s.mux.HandleFunc("GET /api/v1/setup", s.public(s.getSetup))
	s.mux.HandleFunc("POST /api/v1/setup", s.public(s.postSetup))
	s.mux.HandleFunc("POST /api/v1/auth/login", s.public(s.login))

	// Any signed-in user.
	s.mux.HandleFunc("POST /api/v1/kiosk/claim", s.public(s.claimDisplay))
	s.handle("POST /api/v1/auth/logout", "", s.logout)
	s.handle("GET /api/v1/auth/me", "", s.me)
	s.handle("POST /api/v1/auth/password", "", s.changePassword)

	s.handle("GET /api/v1/snapshot", auth.PermDashboardView, s.getSnapshot)
	s.handle("GET /api/v1/live", auth.PermDashboardView, s.live)
	s.handle("GET /api/v1/system", auth.PermDashboardView, s.getSystem)
	s.handle("GET /api/v1/energy", auth.PermEnergyView, s.getEnergy)
	s.handle("GET /api/v1/energy/summary", auth.PermEnergyView, s.getEnergySummary)
	s.handle("GET /api/v1/site", auth.PermDevicesView, s.getSite)
	s.handle("GET /api/v1/profiles", auth.PermDevicesView, s.getProfiles)
	s.handle("GET /api/v1/alarms", auth.PermAlarmsView, s.getAlarms)

	s.handle("POST /api/v1/alarms/ack", auth.PermAlarmsAck, s.ackAlarm)
	s.handle("PUT /api/v1/site/export-limit", auth.PermControlSetpoint, s.putExportLimit)

	s.handle("PUT /api/v1/site/control", auth.PermControlConfigure, s.putControl)
	s.handle("PUT /api/v1/site", auth.PermSiteConfigure, s.putSite)
	s.handle("POST /api/v1/commissioning/probe", auth.PermCommissioning, s.probe)
	s.handle("POST /api/v1/commissioning/read", auth.PermCommissioning, s.readRaw)
	s.handle("GET /api/v1/audit", auth.PermAuditView, s.getAudit)
	s.handle("POST /api/v1/system/reboot", auth.PermSystemReboot, s.reboot)

	s.handle("GET /api/v1/users", auth.PermUsersManage, s.listUsers)
	s.handle("POST /api/v1/users", auth.PermUsersManage, s.createUser)
	s.handle("PATCH /api/v1/users/{id}", auth.PermUsersManage, s.updateUser)
	s.handle("DELETE /api/v1/users/{id}", auth.PermUsersManage, s.deleteUser)
	s.handle("POST /api/v1/users/{id}/password", auth.PermUsersManage, s.resetPassword)
	s.handle("GET /api/v1/displays", auth.PermUsersManage, s.listDisplays)
	s.handle("POST /api/v1/displays", auth.PermUsersManage, s.createDisplay)
	s.handle("DELETE /api/v1/displays/{id}", auth.PermUsersManage, s.revokeDisplay)

	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint")
	})
	if s.Web != nil {
		s.mux.Handle("/", s.webHandler())
	}
}

// ServeHTTP adds security headers to every response.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.Set("Cache-Control", "no-store")
	}
	s.mux.ServeHTTP(w, r)
}

// httpError carries a status code to the response.
type httpError struct {
	status int
	msg    string
}

func (e *httpError) Error() string { return e.msg }

func errorf(status int, format string, args ...any) error {
	return &httpError{status: status, msg: fmt.Sprintf(format, args...)}
}

func (s *Server) respond(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	var he *httpError
	if errors.As(err, &he) {
		writeError(w, he.status, he.msg)
		return
	}
	s.Log.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func (s *Server) public(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Header.Get(csrfHeader) != "1" {
			writeError(w, http.StatusForbidden, "missing "+csrfHeader+" header")
			return
		}
		s.respond(w, r, h(w, r, nil))
	}
}

// handle registers a route that needs a session and, unless perm is
// empty, a permission.
func (s *Server) handle(pattern string, perm auth.Permission, h handlerFunc) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Header.Get(csrfHeader) != "1" {
			writeError(w, http.StatusForbidden, "missing "+csrfHeader+" header")
			return
		}
		u, err := s.principal(r)
		if err != nil {
			clearSessionCookie(w, r)
			writeError(w, http.StatusUnauthorized, "sign in required")
			return
		}
		if perm != "" && !u.Role.Can(perm) {
			writeError(w, http.StatusForbidden, "your role does not allow this")
			return
		}
		s.respond(w, r, h(w, r, &u))
	})
}

// principal resolves who is calling: a signed-in user, a display link, or
// the device's own screen (loopback requests without a session), which
// gets read-only display access.
func (s *Server) principal(r *http.Request) (auth.User, error) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return s.Auth.Authenticate(c.Value)
	}
	if c, err := r.Cookie(displayCookie); err == nil {
		d, err := s.Auth.AuthenticateDisplay(c.Value)
		if err != nil {
			return auth.User{}, err
		}
		return auth.DisplayUser(d.ID, d.Name), nil
	}
	if ip := net.ParseIP(clientIP(r)); s.LocalDisplay && ip != nil && ip.IsLoopback() {
		return auth.DisplayUser("local", "Device screen"), nil
	}
	return auth.User{}, auth.ErrSessionInvalid
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodySize))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errorf(http.StatusBadRequest, "invalid request body: %v", err)
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errorf(http.StatusBadRequest, "invalid request body: trailing data")
	}
	return nil
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil,
		SameSite: http.SameSiteStrictMode, MaxAge: int(auth.SessionAbsoluteTimeout.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

// record writes an audit entry for an action by u.
func (s *Server) record(r *http.Request, u *auth.User, action, target string, details map[string]any, err error) {
	e := audit.Entry{Action: action, Target: target, Details: details, IP: clientIP(r), Success: err == nil}
	if u != nil {
		e.UserID, e.Username, e.Role = u.ID, u.Username, string(u.Role)
	}
	if err != nil {
		e.Error = err.Error()
	}
	s.Audit.Record(e)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request, _ *auth.User) error {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.Version})
	return nil
}

// webHandler serves the embedded UI. Unknown paths get index.html so the
// UI's client-side routes work on reload.
func (s *Server) webHandler() http.Handler {
	files := http.FileServerFS(s.Web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(s.Web, path); err == nil {
				if strings.HasPrefix(path, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=3600")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, s.Web, "index.html")
	})
}
