package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/the-asad-iqbal/async-os/controller/internal/store"
)

func init() { pbkdf2Iterations = 1000 }

func newService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewService(st, filepath.Join(dir, "setup-code"))
}

func setupOwner(t *testing.T, s *Service) User {
	t.Helper()
	if err := s.EnsureSetupCode(); err != nil {
		t.Fatal(err)
	}
	code, _ := os.ReadFile(s.setupCodePath)
	u, err := s.CompleteSetup(string(code), "owner", "Site Owner", "correct horse battery", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestSetupRequiresCodeAndRunsOnce(t *testing.T) {
	s := newService(t)
	if needed, _ := s.NeedsSetup(); !needed {
		t.Fatal("fresh store should need setup")
	}
	if err := s.EnsureSetupCode(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.setupCodePath)
	if err != nil {
		t.Fatal(err)
	}
	code := strings.TrimSpace(string(raw))
	if len(code) != 14 || strings.Count(code, "-") != 2 {
		t.Fatalf("setup code %q", code)
	}

	if _, err := s.CompleteSetup("WRONG-CODE-0000", "owner", "", "correct horse battery", "ip"); !errors.Is(err, ErrBadSetupCode) {
		t.Fatalf("wrong code: %v", err)
	}
	// Codes are accepted without dashes and in lower case.
	compact := strings.ToLower(strings.ReplaceAll(code, "-", ""))
	u, err := s.CompleteSetup(compact, "Owner", "", "correct horse battery", "ip")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != RoleOwner || u.Username != "owner" {
		t.Fatalf("user = %+v", u)
	}
	if _, err := os.Stat(s.setupCodePath); !os.IsNotExist(err) {
		t.Fatal("setup code not removed")
	}
	if _, err := s.CompleteSetup(code, "second", "", "correct horse battery", "ip"); !errors.Is(err, ErrSetupDone) {
		t.Fatalf("second setup: %v", err)
	}
}

func TestLoginAndLockout(t *testing.T) {
	s := newService(t)
	setupOwner(t, s)

	if _, _, err := s.Login("owner", "wrong password!", "1.1.1.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, _, err := s.Login("nobody", "whatever12345", "1.1.1.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user: %v", err)
	}
	_, token, err := s.Login("OWNER", "correct horse battery", "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if u, err := s.Authenticate(token); err != nil || u.Username != "owner" {
		t.Fatalf("authenticate: %v, %v", u, err)
	}

	for range maxUserFailures {
		s.Login("owner", "nope nope nope", "2.2.2.2")
	}
	if _, _, err := s.Login("owner", "correct horse battery", "3.3.3.3"); !errors.Is(err, ErrLockedOut) {
		t.Fatalf("expected lockout, got %v", err)
	}
	// The lockout expires.
	s.now = func() time.Time { return time.Now().Add(lockoutWindow + time.Minute) }
	if _, _, err := s.Login("owner", "correct horse battery", "3.3.3.3"); err != nil {
		t.Fatalf("after lockout window: %v", err)
	}
}

func TestSessionTimeouts(t *testing.T) {
	s := newService(t)
	setupOwner(t, s)
	_, token, _ := s.Login("owner", "correct horse battery", "ip")

	base := time.Now()
	s.now = func() time.Time { return base.Add(SessionIdleTimeout - time.Minute) }
	if _, err := s.Authenticate(token); err != nil {
		t.Fatalf("within idle timeout: %v", err)
	}
	s.now = func() time.Time { return base.Add(2*SessionIdleTimeout + time.Minute) }
	if _, err := s.Authenticate(token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("idle session still valid: %v", err)
	}
	if _, err := s.Authenticate("forged"); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal("forged token accepted")
	}
}

func TestLastOwnerIsProtected(t *testing.T) {
	s := newService(t)
	owner := setupOwner(t, s)

	viewer := RoleViewer
	disabled := true
	if _, err := s.UpdateUser(owner.ID, UserChanges{Role: &viewer}); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("demote last owner: %v", err)
	}
	if _, err := s.UpdateUser(owner.ID, UserChanges{Disabled: &disabled}); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("disable last owner: %v", err)
	}
	if err := s.DeleteUser(owner.ID); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("delete last owner: %v", err)
	}

	if _, err := s.CreateUser("second", "", RoleOwner, "another long password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateUser(owner.ID, UserChanges{Role: &viewer}); err != nil {
		t.Fatalf("demote with another owner present: %v", err)
	}
}

func TestDisablingAndRoleChangesEndSessions(t *testing.T) {
	s := newService(t)
	setupOwner(t, s)
	op, err := s.CreateUser("operator1", "Operator", RoleOperator, "operator password")
	if err != nil {
		t.Fatal(err)
	}
	_, token, _ := s.Login("operator1", "operator password", "ip")

	installer := RoleInstaller
	if _, err := s.UpdateUser(op.ID, UserChanges{Role: &installer}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal("session survived a role change")
	}

	_, token, _ = s.Login("operator1", "operator password", "ip")
	disabled := true
	s.UpdateUser(op.ID, UserChanges{Disabled: &disabled})
	if _, err := s.Authenticate(token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal("session survived disabling")
	}
	if _, _, err := s.Login("operator1", "operator password", "ip"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled user logged in: %v", err)
	}
}

func TestChangePasswordKeepsCurrentSessionOnly(t *testing.T) {
	s := newService(t)
	owner := setupOwner(t, s)
	_, keep, _ := s.Login("owner", "correct horse battery", "ip")
	_, other, _ := s.Login("owner", "correct horse battery", "ip")

	if err := s.ChangePassword(owner.ID, "wrong current pw", "a brand new password", keep); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong current password: %v", err)
	}
	if err := s.ChangePassword(owner.ID, "correct horse battery", "short", keep); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak password: %v", err)
	}
	if err := s.ChangePassword(owner.ID, "correct horse battery", "a brand new password", keep); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(keep); err != nil {
		t.Fatal("current session ended")
	}
	if _, err := s.Authenticate(other); err == nil {
		t.Fatal("other session survived a password change")
	}
}

func TestUsernameRules(t *testing.T) {
	s := newService(t)
	setupOwner(t, s)
	if _, err := s.CreateUser("Owner", "", RoleViewer, "some long password"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("duplicate username: %v", err)
	}
	if _, err := s.CreateUser("a b", "", RoleViewer, "some long password"); !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("invalid username: %v", err)
	}
	if _, err := s.CreateUser("viewer", "", Role("root"), "some long password"); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("invalid role: %v", err)
	}
	if _, err := s.CreateUser("viewer", "", RoleViewer, "viewer"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak password: %v", err)
	}
}

func TestRolePermissions(t *testing.T) {
	cases := []struct {
		role Role
		perm Permission
		want bool
	}{
		{RoleViewer, PermDashboardView, true},
		{RoleViewer, PermAlarmsAck, false},
		{RoleOperator, PermControlSetpoint, true},
		{RoleOperator, PermControlConfigure, false},
		{RoleInstaller, PermSiteConfigure, true},
		{RoleInstaller, PermUsersManage, false},
		{RoleOwner, PermUsersManage, true},
		{Role("root"), PermDashboardView, false},
	}
	for _, c := range cases {
		if got := c.role.Can(c.perm); got != c.want {
			t.Errorf("%s can %s = %v, want %v", c.role, c.perm, got, c.want)
		}
	}
}

func TestPasswordHashFormat(t *testing.T) {
	hash, err := hashPassword("some password")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "pbkdf2-sha256$") || !verifyPassword(hash, "some password") || verifyPassword(hash, "other password") {
		t.Fatalf("hash %q", hash)
	}
	if verifyPassword("garbage", "x") {
		t.Fatal("garbage hash verified")
	}
}

func TestDisplayLinks(t *testing.T) {
	s := newService(t)
	d, token, err := s.CreateDisplay("Control room TV", "owner")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.AuthenticateDisplay(token)
	if err != nil || got.ID != d.ID {
		t.Fatalf("authenticate display: %v %v", got, err)
	}
	if err := s.RevokeDisplay(d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateDisplay(token); !errors.Is(err, ErrDisplayNotFound) {
		t.Fatal("revoked display still works")
	}
	if RoleDisplay.Can(PermAlarmsAck) || !RoleDisplay.Can(PermDashboardView) {
		t.Fatal("display role permissions wrong")
	}
	setupOwner(t, s)
	if _, err := s.CreateUser("wallscreen", "", RoleDisplay, "a long password here"); !errors.Is(err, ErrInvalidRole) {
		t.Fatal("display role assigned to a user")
	}
}
