package energy

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)

func f(v float64) *float64 { return &v }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// meter simulates a dual-source meter polled every second.
type meter struct {
	s       *Splitter
	now     time.Time
	imp     float64
	exp     float64
	freeze  bool // counters freeze while the genset runs
	running bool
}

func (m *meter) run(seconds int, powerW float64) {
	for range seconds {
		m.now = m.now.Add(time.Second)
		if !(m.freeze && m.running) {
			if powerW > 0 {
				m.imp += powerW / 3600 / 1000
			} else {
				m.exp += -powerW / 3600 / 1000
			}
		}
		m.s.Observe(Sample{Time: m.now, GensetRunning: m.running, PowerW: f(powerW), ImportKWh: f(m.imp), ExportKWh: f(m.exp)})
	}
}

func TestGridEnergyFromCounters(t *testing.T) {
	m := &meter{s: NewSplitter(Totals{}), now: t0, imp: 1000, exp: 50}
	m.run(1, 0)
	m.run(3600, 36000)  // 36 kW import for an hour
	m.run(1800, -10000) // export 10 kW for half an hour
	got := m.s.Totals()
	if !near(got.GridImportKWh, 36) || !near(got.GridExportKWh, 5) {
		t.Fatalf("totals = %+v", got)
	}
	if got.GensetImportKWh != 0 || got.GensetExportKWh != 0 {
		t.Fatalf("genset got grid energy: %+v", got)
	}
}

func TestImportAndExportAreIndependent(t *testing.T) {
	// Regression for the bug where the export tracker saw the import
	// counter: a night of import must never show up as export.
	m := &meter{s: NewSplitter(Totals{}), now: t0, imp: 5000, exp: 200}
	m.run(1, 0)
	m.run(7200, 20000)
	if got := m.s.Totals(); got.GridExportKWh != 0 || !near(got.GridImportKWh, 40) {
		t.Fatalf("totals = %+v", got)
	}
}

func TestGensetEnergyRecoveredWhenCountersFreeze(t *testing.T) {
	m := &meter{s: NewSplitter(Totals{}), now: t0, imp: 1000, freeze: true}
	m.run(1, 0)
	m.run(600, 30000) // grid: 5 kWh
	m.running = true
	m.run(3600, 45000) // genset for an hour at 45 kW; counters frozen
	m.running = false
	m.run(600, 30000) // grid again: 5 kWh
	got := m.s.Totals()
	if !near(got.GridImportKWh, 10) {
		t.Fatalf("grid import = %v, want 10", got.GridImportKWh)
	}
	// The first power sample of the window starts the integration.
	if math.Abs(got.GensetImportKWh-45) > 0.02 {
		t.Fatalf("genset import = %v, want about 45", got.GensetImportKWh)
	}
}

func TestNoDoubleCountWhenCountersKeepRunning(t *testing.T) {
	m := &meter{s: NewSplitter(Totals{}), now: t0, imp: 1000, freeze: false}
	m.run(1, 0)
	m.running = true
	m.run(3600, 20000)
	m.running = false
	m.run(1, 0)
	got := m.s.Totals()
	// Counters captured 20 kWh during the window, integration ~20 kWh: the
	// top-up must be close to nothing.
	if math.Abs(got.GensetImportKWh-20) > 0.02 {
		t.Fatalf("genset import = %v, want about 20", got.GensetImportKWh)
	}
}

func TestGensetReversePowerCountsAsGensetExport(t *testing.T) {
	m := &meter{s: NewSplitter(Totals{}), now: t0, freeze: true}
	m.run(1, 0)
	m.running = true
	m.run(360, -5000) // PV pushes 5 kW back into the genset for 6 minutes
	m.running = false
	m.run(1, 0)
	if got := m.s.Totals(); math.Abs(got.GensetExportKWh-0.5) > 0.01 || got.GensetImportKWh != 0 {
		t.Fatalf("totals = %+v", got)
	}
}

func TestCounterResetIsRebased(t *testing.T) {
	s := NewSplitter(Totals{})
	s.Observe(Sample{Time: t0, ImportKWh: f(9000)})
	s.Observe(Sample{Time: t0.Add(time.Minute), ImportKWh: f(12)})              // meter replaced
	s.Observe(Sample{Time: t0.Add(2 * time.Minute), ImportKWh: f(12.5)})        // +0.5 kWh
	s.Observe(Sample{Time: t0.Add(3 * time.Minute), ImportKWh: f(9_000_000)})   // garbage read
	s.Observe(Sample{Time: t0.Add(4 * time.Minute), ImportKWh: f(9_000_000.4)}) // continues from new base
	if got := s.Totals(); !near(got.GridImportKWh, 0.9) {
		t.Fatalf("grid import = %v, want 0.9", got.GridImportKWh)
	}
}

func TestLongGapWithPlausibleRateIsCounted(t *testing.T) {
	s := NewSplitter(Totals{})
	s.Observe(Sample{Time: t0, ImportKWh: f(100)})
	// Two hours without polls, 60 kWh: 30 kW average is plausible.
	s.Observe(Sample{Time: t0.Add(2 * time.Hour), ImportKWh: f(160)})
	if got := s.Totals(); !near(got.GridImportKWh, 60) {
		t.Fatalf("grid import = %v", got.GridImportKWh)
	}
}

func TestPowerGapCannotInventEnergy(t *testing.T) {
	s := NewSplitter(Totals{})
	s.Observe(Sample{Time: t0, GensetRunning: true, PowerW: f(40000)})
	s.Observe(Sample{Time: t0.Add(time.Second), GensetRunning: true, PowerW: f(40000)})
	s.Observe(Sample{Time: t0.Add(time.Hour), GensetRunning: true, PowerW: f(40000)}) // gap
	s.Observe(Sample{Time: t0.Add(time.Hour + time.Second), GensetRunning: false})
	if got := s.Totals(); got.GensetImportKWh > 0.03 {
		t.Fatalf("genset import = %v, the gap was integrated", got.GensetImportKWh)
	}
}

func TestSmallIncrementsAddUp(t *testing.T) {
	s := NewSplitter(Totals{})
	v := 10.0
	s.Observe(Sample{Time: t0, ImportKWh: f(v)})
	for i := 1; i <= 100; i++ {
		v += 0.00005 // below the noise threshold each time
		s.Observe(Sample{Time: t0.Add(time.Duration(i) * time.Second), ImportKWh: f(v)})
	}
	if got := s.Totals(); math.Abs(got.GridImportKWh-0.005) > 0.0002 {
		t.Fatalf("grid import = %v, want about 0.005", got.GridImportKWh)
	}
}

func TestRestoredTotalsContinue(t *testing.T) {
	s := NewSplitter(Totals{GridImportKWh: 100, GensetImportKWh: 7})
	s.Observe(Sample{Time: t0, ImportKWh: f(500)})
	s.Observe(Sample{Time: t0.Add(time.Minute), ImportKWh: f(500.5)})
	if got := s.Totals(); !near(got.GridImportKWh, 100.5) || got.GensetImportKWh != 7 {
		t.Fatalf("totals = %+v", got)
	}
}
