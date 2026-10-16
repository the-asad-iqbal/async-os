package audit

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/the-asad-iqbal/async-os/controller/internal/store"
)

func TestRecordListAndCap(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	l := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), 5)
	for i := range 8 {
		l.Record(Entry{Action: "test", Target: string(rune('a' + i)), Success: true})
	}

	newest, err := l.List(0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(newest) != 3 || newest[0].Target != "h" || newest[2].Target != "f" {
		t.Fatalf("newest = %+v", newest)
	}
	older, _ := l.List(newest[2].ID, 10)
	// Capped at five entries: d..h remain, so only d and e are older.
	if len(older) != 2 || older[0].Target != "e" || older[1].Target != "d" {
		t.Fatalf("older = %+v", older)
	}
}
