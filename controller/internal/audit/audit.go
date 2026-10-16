// Package audit records who changed what, when and from where.
package audit

import (
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/the-asad-iqbal/async-os/controller/internal/store"
)

// Entry is one audited action.
type Entry struct {
	ID       uint64         `json:"id"`
	Time     time.Time      `json:"time"`
	UserID   string         `json:"user_id,omitempty"`
	Username string         `json:"username,omitempty"`
	Role     string         `json:"role,omitempty"`
	Action   string         `json:"action"`
	Target   string         `json:"target,omitempty"`
	Details  map[string]any `json:"details,omitempty"`
	IP       string         `json:"ip,omitempty"`
	Success  bool           `json:"success"`
	Error    string         `json:"error,omitempty"`
}

// Log is an append-only audit trail with a size cap.
type Log struct {
	store *store.Store
	log   *slog.Logger
	max   int
}

// New returns an audit log keeping at most max entries.
func New(s *store.Store, log *slog.Logger, max int) *Log {
	return &Log{store: s, log: log, max: max}
}

// Record appends an entry. Failures are logged, never returned: auditing
// must not block the action it describes.
func (l *Log) Record(e Entry) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	err := l.store.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(store.BucketAudit)
		id, err := b.NextSequence()
		if err != nil {
			return err
		}
		e.ID = id
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if err := b.Put(store.SequenceKey(id), data); err != nil {
			return err
		}
		// Drop the oldest entries beyond the cap.
		if excess := b.Stats().KeyN + 1 - l.max; excess > 0 {
			c := b.Cursor()
			for k, _ := c.First(); k != nil && excess > 0; k, _ = c.Next() {
				if err := c.Delete(); err != nil {
					return err
				}
				excess--
			}
		}
		return nil
	})
	attrs := []any{"action", e.Action, "user", e.Username, "target", e.Target, "ip", e.IP, "success", e.Success}
	if err != nil {
		l.log.Error("audit write failed", append(attrs, "error", err)...)
		return
	}
	l.log.Info("audit", attrs...)
}

// List returns up to limit entries older than beforeID (0 for the newest),
// newest first.
func (l *Log) List(beforeID uint64, limit int) ([]Entry, error) {
	entries := []Entry{}
	err := l.store.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(store.BucketAudit).Cursor()
		var k, v []byte
		if beforeID == 0 {
			k, v = c.Last()
		} else {
			k, v = c.Seek(store.SequenceKey(beforeID))
			if k == nil {
				k, v = c.Last()
			}
			for k != nil && binary.BigEndian.Uint64(k) >= beforeID {
				k, v = c.Prev()
			}
		}
		for ; k != nil && len(entries) < limit; k, v = c.Prev() {
			var e Entry
			if err := json.Unmarshal(v, &e); err != nil {
				continue
			}
			entries = append(entries, e)
		}
		return nil
	})
	return entries, err
}
