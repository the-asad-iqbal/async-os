package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/the-asad-iqbal/async-os/controller/internal/store"
)

// Errors returned to API callers. They are deliberately vague where detail
// would help an attacker.
var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrLockedOut          = errors.New("too many failed attempts, try again later")
	ErrSetupDone          = errors.New("setup is already complete")
	ErrBadSetupCode       = errors.New("invalid setup code")
	ErrUsernameTaken      = errors.New("username is already taken")
	ErrInvalidUsername    = errors.New("username must be 3 to 32 lowercase letters, digits, dots, dashes or underscores")
	ErrInvalidRole        = errors.New("unknown role")
	ErrLastOwner          = errors.New("the last active owner cannot be removed, disabled or demoted")
	ErrUserNotFound       = errors.New("user not found")
	ErrSessionInvalid     = errors.New("session expired or invalid")
)

// Session lifetimes.
const (
	SessionIdleTimeout     = 8 * time.Hour
	SessionAbsoluteTimeout = 72 * time.Hour
	sessionTouchInterval   = time.Minute
)

// Login throttling: per username and per client address.
const (
	maxUserFailures = 5
	maxIPFailures   = 20
	lockoutWindow   = 15 * time.Minute
)

// User is an account.
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"display_name"`
	Role         Role      `json:"role"`
	Disabled     bool      `json:"disabled"`
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastLoginAt  time.Time `json:"last_login_at,omitzero"`
}

// Public is a user without secrets, for API responses.
type Public struct {
	ID          string       `json:"id"`
	Username    string       `json:"username"`
	DisplayName string       `json:"display_name"`
	Role        Role         `json:"role"`
	Disabled    bool         `json:"disabled"`
	CreatedAt   time.Time    `json:"created_at"`
	LastLoginAt *time.Time   `json:"last_login_at,omitempty"`
	Permissions []Permission `json:"permissions"`
}

// Public strips secrets.
func (u User) Public() Public {
	p := Public{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Role: u.Role, Disabled: u.Disabled,
		CreatedAt: u.CreatedAt, Permissions: u.Role.Permissions()}
	if !u.LastLoginAt.IsZero() {
		t := u.LastLoginAt
		p.LastLoginAt = &t
	}
	return p
}

// Session is a logged-in browser.
type Session struct {
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
	IP        string    `json:"ip"`
}

// Service manages accounts and sessions.
type Service struct {
	store         *store.Store
	setupCodePath string
	now           func() time.Time

	mu       sync.Mutex
	failures map[string][]time.Time // "u:name" or "ip:addr" -> failure times
}

// NewService returns the auth service. setupCodePath is where the one-time
// setup code is written while no user exists; the device console shows it.
func NewService(s *store.Store, setupCodePath string) *Service {
	return &Service{store: s, setupCodePath: setupCodePath, now: time.Now, failures: map[string][]time.Time{}}
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

// NeedsSetup reports whether no user exists yet.
func (s *Service) NeedsSetup() (bool, error) {
	empty := false
	err := s.store.View(func(tx *bolt.Tx) error {
		k, _ := tx.Bucket(store.BucketUsers).Cursor().First()
		empty = k == nil
		return nil
	})
	return empty, err
}

// EnsureSetupCode writes a fresh setup code when setup is pending and
// none exists, and removes it once setup is done.
func (s *Service) EnsureSetupCode() error {
	needed, err := s.NeedsSetup()
	if err != nil {
		return err
	}
	if !needed {
		if err := os.Remove(s.setupCodePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if _, err := os.Stat(s.setupCodePath); err == nil {
		return nil
	}
	code, err := newSetupCode()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.setupCodePath), 0o750); err != nil {
		return err
	}
	return os.WriteFile(s.setupCodePath, []byte(code+"\n"), 0o640)
}

// newSetupCode returns 12 characters in groups of four, from an alphabet
// without look-alike characters.
func newSetupCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var b strings.Builder
	for i, r := range raw {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(alphabet[int(r)%len(alphabet)])
	}
	return b.String(), nil
}

// CompleteSetup creates the first owner, guarded by the setup code.
func (s *Service) CompleteSetup(code, username, displayName, password, ip string) (User, error) {
	if s.limited("ip:"+ip, maxIPFailures) {
		return User{}, ErrLockedOut
	}
	data, err := os.ReadFile(s.setupCodePath)
	if err != nil {
		if needed, _ := s.NeedsSetup(); !needed {
			return User{}, ErrSetupDone
		}
		return User{}, ErrBadSetupCode
	}
	normalize := func(c string) string {
		return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(c), "-", ""))
	}
	if subtle.ConstantTimeCompare([]byte(normalize(code)), []byte(normalize(string(data)))) != 1 {
		s.fail("ip:" + ip)
		return User{}, ErrBadSetupCode
	}

	var created User
	err = s.store.Update(func(tx *bolt.Tx) error {
		if k, _ := tx.Bucket(store.BucketUsers).Cursor().First(); k != nil {
			return ErrSetupDone
		}
		u, err := s.newUser(tx, username, displayName, RoleOwner, password)
		created = u
		return err
	})
	if err != nil {
		return User{}, err
	}
	_ = os.Remove(s.setupCodePath)
	return created, nil
}

func (s *Service) newUser(tx *bolt.Tx, username, displayName string, role Role, password string) (User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !usernamePattern.MatchString(username) {
		return User{}, ErrInvalidUsername
	}
	if !role.Assignable() {
		return User{}, ErrInvalidRole
	}
	if err := checkPasswordPolicy(username, password); err != nil {
		return User{}, err
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = username
	}
	if len(displayName) > 64 {
		return User{}, errors.New("display name must be at most 64 characters")
	}
	if _, err := findByUsername(tx, username); err == nil {
		return User{}, ErrUsernameTaken
	}
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}
	id, err := randomID(8)
	if err != nil {
		return User{}, err
	}
	now := s.now().UTC()
	u := User{ID: id, Username: username, DisplayName: displayName, Role: role, PasswordHash: hash, CreatedAt: now, UpdatedAt: now}
	return u, store.PutJSON(tx, store.BucketUsers, []byte(u.ID), u)
}

func findByUsername(tx *bolt.Tx, username string) (User, error) {
	var found User
	err := tx.Bucket(store.BucketUsers).ForEach(func(_, v []byte) error {
		var u User
		if err := jsonUnmarshal(v, &u); err != nil {
			return err
		}
		if u.Username == username {
			found = u
			return errStop
		}
		return nil
	})
	if errors.Is(err, errStop) {
		return found, nil
	}
	if err != nil {
		return User{}, err
	}
	return User{}, ErrUserNotFound
}

var errStop = errors.New("stop")

// Login checks credentials and returns a new session token.
func (s *Service) Login(username, password, ip string) (User, string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if s.limited("u:"+username, maxUserFailures) || s.limited("ip:"+ip, maxIPFailures) {
		return User{}, "", ErrLockedOut
	}

	var u User
	err := s.store.View(func(tx *bolt.Tx) error {
		var err error
		u, err = findByUsername(tx, username)
		return err
	})
	if err != nil {
		verifyPassword(dummyHash, password)
		s.fail("u:"+username, "ip:"+ip)
		return User{}, "", ErrInvalidCredentials
	}
	if !verifyPassword(u.PasswordHash, password) || u.Disabled {
		s.fail("u:"+username, "ip:"+ip)
		return User{}, "", ErrInvalidCredentials
	}
	s.clear("u:" + username)

	token, err := randomToken()
	if err != nil {
		return User{}, "", err
	}
	now := s.now().UTC()
	err = s.store.Update(func(tx *bolt.Tx) error {
		u.LastLoginAt = now
		if err := store.PutJSON(tx, store.BucketUsers, []byte(u.ID), u); err != nil {
			return err
		}
		return store.PutJSON(tx, store.BucketSessions, tokenKey(token), Session{UserID: u.ID, CreatedAt: now, LastSeen: now, IP: ip})
	})
	if err != nil {
		return User{}, "", err
	}
	return u, token, nil
}

// Authenticate resolves a session token to its user, extending the idle
// timeout.
func (s *Service) Authenticate(token string) (User, error) {
	if token == "" {
		return User{}, ErrSessionInvalid
	}
	key := tokenKey(token)
	now := s.now().UTC()

	var u User
	var sess Session
	err := s.store.View(func(tx *bolt.Tx) error {
		if err := store.GetJSON(tx, store.BucketSessions, key, &sess); err != nil {
			return ErrSessionInvalid
		}
		if err := store.GetJSON(tx, store.BucketUsers, []byte(sess.UserID), &u); err != nil {
			return ErrSessionInvalid
		}
		return nil
	})
	if err != nil {
		return User{}, err
	}
	expired := now.Sub(sess.LastSeen) > SessionIdleTimeout || now.Sub(sess.CreatedAt) > SessionAbsoluteTimeout
	if expired || u.Disabled {
		_ = s.store.Update(func(tx *bolt.Tx) error { return tx.Bucket(store.BucketSessions).Delete(key) })
		return User{}, ErrSessionInvalid
	}
	if now.Sub(sess.LastSeen) > sessionTouchInterval {
		sess.LastSeen = now
		_ = s.store.Update(func(tx *bolt.Tx) error { return store.PutJSON(tx, store.BucketSessions, key, sess) })
	}
	return u, nil
}

// Logout ends a session.
func (s *Service) Logout(token string) error {
	return s.store.Update(func(tx *bolt.Tx) error { return tx.Bucket(store.BucketSessions).Delete(tokenKey(token)) })
}

// PruneSessions deletes expired sessions.
func (s *Service) PruneSessions() error {
	now := s.now().UTC()
	return s.store.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(store.BucketSessions)
		var stale [][]byte
		err := b.ForEach(func(k, v []byte) error {
			var sess Session
			if err := jsonUnmarshal(v, &sess); err != nil || now.Sub(sess.LastSeen) > SessionIdleTimeout || now.Sub(sess.CreatedAt) > SessionAbsoluteTimeout {
				stale = append(stale, append([]byte(nil), k...))
			}
			return nil
		})
		for _, k := range stale {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return err
	})
}

// ListUsers returns every user.
func (s *Service) ListUsers() ([]User, error) {
	var users []User
	err := s.store.View(func(tx *bolt.Tx) error {
		return tx.Bucket(store.BucketUsers).ForEach(func(_, v []byte) error {
			var u User
			if err := jsonUnmarshal(v, &u); err != nil {
				return err
			}
			users = append(users, u)
			return nil
		})
	})
	return users, err
}

// CreateUser adds an account.
func (s *Service) CreateUser(username, displayName string, role Role, password string) (User, error) {
	var u User
	err := s.store.Update(func(tx *bolt.Tx) error {
		var err error
		u, err = s.newUser(tx, username, displayName, role, password)
		return err
	})
	return u, err
}

// UserChanges are optional edits to an account.
type UserChanges struct {
	DisplayName *string `json:"display_name"`
	Role        *Role   `json:"role"`
	Disabled    *bool   `json:"disabled"`
}

// UpdateUser edits an account. Role changes and disabling end the user's
// sessions.
func (s *Service) UpdateUser(id string, c UserChanges) (User, error) {
	var u User
	err := s.store.Update(func(tx *bolt.Tx) error {
		if err := store.GetJSON(tx, store.BucketUsers, []byte(id), &u); err != nil {
			return ErrUserNotFound
		}
		revoke := false
		if c.DisplayName != nil {
			name := strings.TrimSpace(*c.DisplayName)
			if name == "" || len(name) > 64 {
				return errors.New("display name must be 1 to 64 characters")
			}
			u.DisplayName = name
		}
		if c.Role != nil && *c.Role != u.Role {
			if !c.Role.Assignable() {
				return ErrInvalidRole
			}
			if u.Role == RoleOwner && !u.Disabled {
				if err := ensureAnotherOwner(tx, u.ID); err != nil {
					return err
				}
			}
			u.Role, revoke = *c.Role, true
		}
		if c.Disabled != nil && *c.Disabled != u.Disabled {
			if *c.Disabled && u.Role == RoleOwner {
				if err := ensureAnotherOwner(tx, u.ID); err != nil {
					return err
				}
			}
			u.Disabled = *c.Disabled
			if u.Disabled {
				revoke = true
			}
		}
		u.UpdatedAt = s.now().UTC()
		if revoke {
			if err := revokeSessions(tx, u.ID); err != nil {
				return err
			}
		}
		return store.PutJSON(tx, store.BucketUsers, []byte(u.ID), u)
	})
	return u, err
}

// DeleteUser removes an account and its sessions.
func (s *Service) DeleteUser(id string) error {
	return s.store.Update(func(tx *bolt.Tx) error {
		var u User
		if err := store.GetJSON(tx, store.BucketUsers, []byte(id), &u); err != nil {
			return ErrUserNotFound
		}
		if u.Role == RoleOwner && !u.Disabled {
			if err := ensureAnotherOwner(tx, u.ID); err != nil {
				return err
			}
		}
		if err := revokeSessions(tx, u.ID); err != nil {
			return err
		}
		return tx.Bucket(store.BucketUsers).Delete([]byte(id))
	})
}

// ChangePassword changes the caller's own password after checking the
// current one. Other sessions of the user end; keepToken stays valid.
func (s *Service) ChangePassword(id, current, next, keepToken string) error {
	return s.store.Update(func(tx *bolt.Tx) error {
		var u User
		if err := store.GetJSON(tx, store.BucketUsers, []byte(id), &u); err != nil {
			return ErrUserNotFound
		}
		if !verifyPassword(u.PasswordHash, current) {
			return ErrInvalidCredentials
		}
		return s.setPassword(tx, u, next, keepToken)
	})
}

// ResetPassword sets another user's password (owner action) and ends all
// of their sessions.
func (s *Service) ResetPassword(id, next string) error {
	return s.store.Update(func(tx *bolt.Tx) error {
		var u User
		if err := store.GetJSON(tx, store.BucketUsers, []byte(id), &u); err != nil {
			return ErrUserNotFound
		}
		return s.setPassword(tx, u, next, "")
	})
}

func (s *Service) setPassword(tx *bolt.Tx, u User, password, keepToken string) error {
	if err := checkPasswordPolicy(u.Username, password); err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	u.PasswordHash, u.UpdatedAt = hash, s.now().UTC()
	if err := revokeSessionsExcept(tx, u.ID, keepToken); err != nil {
		return err
	}
	return store.PutJSON(tx, store.BucketUsers, []byte(u.ID), u)
}

func ensureAnotherOwner(tx *bolt.Tx, exceptID string) error {
	found := false
	err := tx.Bucket(store.BucketUsers).ForEach(func(_, v []byte) error {
		var u User
		if err := jsonUnmarshal(v, &u); err != nil {
			return err
		}
		if u.ID != exceptID && u.Role == RoleOwner && !u.Disabled {
			found = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !found {
		return ErrLastOwner
	}
	return nil
}

func revokeSessions(tx *bolt.Tx, userID string) error {
	return revokeSessionsExcept(tx, userID, "")
}

func revokeSessionsExcept(tx *bolt.Tx, userID, keepToken string) error {
	keep := ""
	if keepToken != "" {
		keep = string(tokenKey(keepToken))
	}
	b := tx.Bucket(store.BucketSessions)
	var doomed [][]byte
	err := b.ForEach(func(k, v []byte) error {
		var sess Session
		if err := jsonUnmarshal(v, &sess); err == nil && sess.UserID == userID && string(k) != keep {
			doomed = append(doomed, append([]byte(nil), k...))
		}
		return nil
	})
	for _, k := range doomed {
		if err := b.Delete(k); err != nil {
			return err
		}
	}
	return err
}

// limited reports whether key has too many recent failures.
func (s *Service) limited(key string, limit int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := s.now().Add(-lockoutWindow)
	recent := s.failures[key][:0]
	for _, t := range s.failures[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(s.failures, key)
		return false
	}
	s.failures[key] = recent
	return len(recent) >= limit
}

func (s *Service) fail(keys ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		s.failures[k] = append(s.failures[k], s.now())
	}
}

func (s *Service) clear(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failures, key)
}

// randomToken returns 32 random bytes, base64url encoded.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomID(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// tokenKey stores only a hash of the token, so a copy of the database does
// not hand out live sessions.
func tokenKey(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func jsonUnmarshal(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("corrupt record: %w", err)
	}
	return nil
}
