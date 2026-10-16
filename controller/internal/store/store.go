// Package store persists controller state in a single bbolt database on
// the data partition.
package store

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Buckets.
var (
	BucketMeta     = []byte("meta")
	BucketUsers    = []byte("users")
	BucketSessions = []byte("sessions")
	BucketDisplays = []byte("displays")
	BucketAudit    = []byte("audit")
	BucketStats5m  = []byte("stats_5m")
	BucketStats1h  = []byte("stats_1h")
)

// ErrNotFound is returned when a key does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps the database.
type Store struct {
	db *bolt.DB
}

// Open opens or creates the database at path.
func Open(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{BucketMeta, BucketUsers, BucketSessions, BucketDisplays, BucketAudit, BucketStats5m, BucketStats1h} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// View runs a read-only transaction.
func (s *Store) View(fn func(tx *bolt.Tx) error) error { return s.db.View(fn) }

// Update runs a read-write transaction.
func (s *Store) Update(fn func(tx *bolt.Tx) error) error { return s.db.Update(fn) }

// GetJSON decodes the value at bucket/key into v.
func GetJSON(tx *bolt.Tx, bucket, key []byte, v any) error {
	data := tx.Bucket(bucket).Get(key)
	if data == nil {
		return ErrNotFound
	}
	return json.Unmarshal(data, v)
}

// PutJSON encodes v and stores it at bucket/key.
func PutJSON(tx *bolt.Tx, bucket, key []byte, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return tx.Bucket(bucket).Put(key, data)
}

// TimeKey encodes t as a big-endian key so keys sort by time.
func TimeKey(t time.Time) []byte {
	k := make([]byte, 8)
	binary.BigEndian.PutUint64(k, uint64(t.Unix()))
	return k
}

// KeyTime decodes a TimeKey.
func KeyTime(k []byte) time.Time {
	return time.Unix(int64(binary.BigEndian.Uint64(k)), 0).UTC()
}

// SequenceKey encodes a monotonically increasing ID.
func SequenceKey(id uint64) []byte {
	k := make([]byte, 8)
	binary.BigEndian.PutUint64(k, id)
	return k
}
