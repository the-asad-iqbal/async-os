package plant_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/the-asad-iqbal/async-os/controller/internal/control"
	"github.com/the-asad-iqbal/async-os/controller/internal/devices"
	"github.com/the-asad-iqbal/async-os/controller/internal/plant"
	"github.com/the-asad-iqbal/async-os/controller/internal/sim"
)

func startSite(t *testing.T, scenario sim.Scenario) (*sim.Site, *plant.Engine, context.CancelFunc) {
	t.Helper()
	lib, err := devices.LoadLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	site, err := sim.Start("127.0.0.1:0", scenario)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := plant.NewEngine(lib, site.Config(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	engine.SetCycleInterval(200 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	go site.Run(ctx)
	go engine.Run(ctx)
	t.Cleanup(func() {
		cancel()
		site.Close()
	})
	return site, engine, cancel
}

func waitFor(t *testing.T, timeout time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestEngineHoldsZeroExportOnSimulatedSite(t *testing.T) {
	_, engine, _ := startSite(t, sim.ScenarioGrid)

	waitFor(t, 5*time.Second, "all devices online", func() bool {
		s := engine.Snapshot()
		if len(s.Devices) != 3 {
			return false
		}
		for _, d := range s.Devices {
			if !d.Online {
				return false
			}
		}
		return true
	})

	s := engine.Snapshot()
	if s.Mode != control.ModeControl || s.GridW == nil || s.LoadW == nil {
		t.Fatalf("snapshot = %+v", s)
	}
	for _, d := range s.Devices {
		if d.Role == plant.RolePVInverter && (!d.Controllable || d.RatedW == 0) {
			t.Fatalf("inverter %s not controllable: %+v", d.ID, d)
		}
	}

	// Let the loop settle, then export must stay within limit + margin
	// for the rest of the observation window.
	time.Sleep(3 * time.Second)
	end := time.Now().Add(3 * time.Second)
	for time.Now().Before(end) {
		s := engine.Snapshot()
		if s.GridW != nil && *s.GridW < -1500 {
			t.Fatalf("exporting %.0f W with a zero export limit", -*s.GridW)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestEngineApplyRestartsDevices(t *testing.T) {
	site, engine, _ := startSite(t, sim.ScenarioGrid)
	waitFor(t, 5*time.Second, "first snapshot", func() bool { return len(engine.Snapshot().Devices) == 3 })

	cfg := site.Config()
	cfg.Devices = cfg.Devices[:2]
	cfg.Control.Mode = control.ModeMonitor
	if err := engine.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "reconfigured snapshot", func() bool {
		s := engine.Snapshot()
		return len(s.Devices) == 2 && s.Mode == control.ModeMonitor && s.Decision.Status == control.StatusMonitoring
	})

	bad := site.Config()
	bad.Devices[0].Profile = "does-not-exist"
	if err := engine.Apply(bad); err == nil {
		t.Fatal("invalid configuration accepted")
	}
}

func TestEngineProtectsGensetDuringOutage(t *testing.T) {
	site, engine, _ := startSite(t, sim.ScenarioHybrid)
	site.SetGridCycle(time.Second, time.Hour)

	waitFor(t, 10*time.Second, "genset carrying the site", func() bool {
		s := engine.Snapshot()
		return s.Decision.GensetsRunning == 1 && !s.Decision.GridPresent
	})
	time.Sleep(3 * time.Second)

	minLoad := 0.30 * sim.GensetRatedW
	end := time.Now().Add(3 * time.Second)
	for time.Now().Before(end) {
		s := engine.Snapshot()
		if s.GensetW == nil {
			t.Fatal("genset power missing")
		}
		// Minimum load minus margin, with slack for load swings.
		if *s.GensetW < minLoad-3000 {
			t.Fatalf("genset at %.0f W, below its %.0f W minimum", *s.GensetW, minLoad)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestEngineSplitsATSMeterEnergy(t *testing.T) {
	site, engine, _ := startSite(t, sim.ScenarioATS)
	site.SetGridCycle(4*time.Second, 6*time.Second)

	var meter plant.DeviceStatus
	find := func() bool {
		for _, d := range engine.Snapshot().Devices {
			if d.ID == "meter1" {
				meter = d
				return true
			}
		}
		return false
	}
	// On genset the meter becomes the genset: the floor binds and the
	// genset never drops far below 25 kW.
	waitFor(t, 15*time.Second, "genset window", func() bool { return find() && meter.Source == "genset" })
	time.Sleep(3 * time.Second)
	s := engine.Snapshot()
	if s.GensetW == nil || *s.GensetW < 25000-3000 || s.GridW != nil {
		t.Fatalf("genset mode: genset=%v grid=%v decision=%+v", s.GensetW, s.GridW, s.Decision)
	}

	// Back on grid, the genset window closes and its energy, invisible
	// to the frozen counters, is recovered from live power.
	waitFor(t, 15*time.Second, "grid again", func() bool { return find() && meter.Source == "grid" })
	waitFor(t, 5*time.Second, "split totals", func() bool {
		find()
		return meter.SourceTotals != nil && meter.SourceTotals.GensetImportKWh > 0.01 && meter.SourceTotals.GridImportKWh > 0
	})
	if got := engine.Totals()["meter1"]; got.GensetImportKWh <= 0 {
		t.Fatalf("totals = %+v", got)
	}
}
