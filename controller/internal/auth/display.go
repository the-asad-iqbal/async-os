package auth

import (
	"errors"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/the-asad-iqbal/async-os/controller/internal/store"
)

// Display is a revocable, read-only link for a kiosk screen. Only a hash
// of its token is stored.
type Display struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
	LastSeen  time.Time `json:"last_seen,omitzero"`
}

// ErrDisplayNotFound is returned for unknown or revoked display links.
var ErrDisplayNotFound = errors.New("display link not found or revoked")

const displayTouchInterval = 5 * time.Minute

// CreateDisplay makes a display link. The token is returned once.
func (s *Service) CreateDisplay(name, createdBy string) (Display, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return Display{}, "", errors.New("display name must be 1 to 64 characters")
	}
	token, err := randomToken()
	if err != nil {
		return Display{}, "", err
	}
	id, err := randomID(6)
	if err != nil {
		return Display{}, "", err
	}
	d := Display{ID: id, Name: name, CreatedAt: s.now().UTC(), CreatedBy: createdBy}
	err = s.store.Update(func(tx *bolt.Tx) error {
		return store.PutJSON(tx, store.BucketDisplays, tokenKey(token), d)
	})
	return d, token, err
}

// ListDisplays returns every display link.
func (s *Service) ListDisplays() ([]Display, error) {
	out := []Display{}
	err := s.store.View(func(tx *bolt.Tx) error {
		return tx.Bucket(store.BucketDisplays).ForEach(func(_, v []byte) error {
			var d Display
			if err := jsonUnmarshal(v, &d); err != nil {
				return err
			}
			out = append(out, d)
			return nil
		})
	})
	return out, err
}

// RevokeDisplay deletes a display link by ID.
func (s *Service) RevokeDisplay(id string) error {
	return s.store.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(store.BucketDisplays)
		var key []byte
		err := b.ForEach(func(k, v []byte) error {
			var d Display
			if jsonUnmarshal(v, &d) == nil && d.ID == id {
				key = append([]byte(nil), k...)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if key == nil {
			return ErrDisplayNotFound
		}
		return b.Delete(key)
	})
}

// AuthenticateDisplay resolves a display token.
func (s *Service) AuthenticateDisplay(token string) (Display, error) {
	if token == "" {
		return Display{}, ErrDisplayNotFound
	}
	key := tokenKey(token)
	var d Display
	if err := s.store.View(func(tx *bolt.Tx) error { return store.GetJSON(tx, store.BucketDisplays, key, &d) }); err != nil {
		return Display{}, ErrDisplayNotFound
	}
	if now := s.now().UTC(); now.Sub(d.LastSeen) > displayTouchInterval {
		d.LastSeen = now
		_ = s.store.Update(func(tx *bolt.Tx) error { return store.PutJSON(tx, store.BucketDisplays, key, d) })
	}
	return d, nil
}

// DisplayUser is the principal a display acts as.
func DisplayUser(id, name string) User {
	return User{ID: "display:" + id, Username: "display", DisplayName: name, Role: RoleDisplay}
}
