package stats

import (
	"io"
	"log/slog"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/the-asad-iqbal/async-os/controller/internal/plant"
	"github.com/the-asad-iqbal/async-os/controller/internal/store"
)

func ptr(v float64) *float64 { return &v }

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func feed(r *Recorder, from time.Time, seconds int, pv, grid float64) {
	for i := 0; i <= seconds; i++ {
		load := pv + grid
		r.Observe(plant.Snapshot{Time: from.Add(time.Duration(i) * time.Second), PVW: pv, GridW: ptr(grid), LoadW: ptr(load)})
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestEnergyIntegration(t *testing.T) {
	r := NewRecorder(openStore(t), quiet)
	start := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	// 10 minutes: 10 kW PV, 2 kW import, then 10 minutes exporting 3 kW.
	feed(r, start, 600, 10000, 2000)
	feed(r, start.Add(601*time.Second), 599, 10000, -3000)
	r.Flush()

	totals, err := r.Totals(start, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// 0..1200 s with no gap. The second between the runs straddles import
	// and export and is split by the trapezoid rule.
	if !near(totals[PV], 10000*1200.0/3600) {
		t.Fatalf("pv = %v Wh", totals[PV])
	}
	if !near(totals[GridImport], 2000*600.0/3600+1000.0/3600) {
		t.Fatalf("import = %v Wh", totals[GridImport])
	}
	if !near(totals[GridExport], 3000*599.0/3600+1500.0/3600) {
		t.Fatalf("export = %v Wh", totals[GridExport])
	}

	buckets, err := r.Query(start, start.Add(time.Hour), Period5m, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	// The sample at exactly 10:20:00 opens a fifth bucket holding the
	// last second: an interval belongs to the bucket of its end time.
	if len(buckets) != 5 || buckets[4].Seconds != 1 {
		t.Fatalf("got %d five-minute buckets", len(buckets))
	}
	if p := buckets[0].Power[PowerGrid]; p.Mean != 2000 || p.Min != 2000 || p.Max != 2000 {
		t.Fatalf("grid power stats = %+v", p)
	}
}

func TestQueryIncludesLiveBucket(t *testing.T) {
	r := NewRecorder(openStore(t), quiet)
	start := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	feed(r, start, 60, 3600, 0) // one minute, not flushed
	totals, err := r.Totals(start, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !near(totals[PV], 60) {
		t.Fatalf("live pv = %v Wh, want 60", totals[PV])
	}
}

func TestRestartInsideBucketAddsUp(t *testing.T) {
	st := openStore(t)
	start := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	r := NewRecorder(st, quiet)
	feed(r, start, 60, 3600, 0)
	r.Flush()
	r = NewRecorder(st, quiet)
	feed(r, start.Add(2*time.Minute), 60, 3600, 0)
	r.Flush()
	totals, _ := r.Totals(start, start.Add(time.Hour))
	if !near(totals[PV], 120) {
		t.Fatalf("pv after restart = %v Wh, want 120", totals[PV])
	}
}

func TestDaysFollowSiteTimezone(t *testing.T) {
	r := NewRecorder(openStore(t), quiet)
	karachi, err := time.LoadLocation("Asia/Karachi") // UTC+5
	if err != nil {
		t.Skip("no tzdata")
	}
	// 18:30 and 19:30 UTC fall on different local days in Karachi.
	feed(r, time.Date(2026, 10, 16, 18, 30, 0, 0, time.UTC), 60, 3600, 0)
	r.Flush()
	feed(r, time.Date(2026, 10, 16, 19, 30, 0, 0, time.UTC), 60, 3600, 0)
	r.Flush()
	days, err := r.Query(time.Date(2026, 10, 16, 0, 0, 0, 0, karachi), time.Date(2026, 10, 18, 0, 0, 0, 0, karachi), PeriodDay, karachi)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 || days[0].Start.Day() != 16 || days[1].Start.Day() != 17 {
		t.Fatalf("days = %+v", days)
	}
}

func TestQueryValidation(t *testing.T) {
	r := NewRecorder(openStore(t), quiet)
	now := time.Now()
	if _, err := r.Query(now, now.Add(-time.Hour), PeriodHour, time.UTC); err == nil {
		t.Fatal("reversed range accepted")
	}
	if _, err := r.Query(now, now.Add(time.Hour), "week", time.UTC); err == nil {
		t.Fatal("unknown period accepted")
	}
	if _, err := r.Query(now, now.Add(500*24*time.Hour), PeriodDay, time.UTC); err == nil {
		t.Fatal("huge range accepted")
	}
}
