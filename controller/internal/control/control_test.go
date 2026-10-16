package control

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func controlSettings() Settings {
	s := DefaultSettings()
	s.Mode = ModeControl
	return s
}

func grid(powerW float64, at time.Time) *Meter {
	return &Meter{ID: "grid", Online: true, Updated: at, PowerW: powerW, VoltageV: 230, HasVoltage: true}
}

func inverter(id string, powerW, ratedW float64, at time.Time) Inverter {
	return Inverter{ID: id, Online: true, Updated: at, PowerW: powerW, RatedW: ratedW, Controllable: true}
}

func hasAlarm(d Decision, code string) bool {
	for _, a := range d.Alarms {
		if a.Code == code {
			return true
		}
	}
	return false
}

func TestGridWithoutVoltageReadingCountsAsPresent(t *testing.T) {
	s := controlSettings()
	var st State
	g := grid(1000, t0)
	g.VoltageV, g.HasVoltage = 0, false
	d := Step(s, Measurements{Now: t0, Grid: g, Inverters: []Inverter{inverter("inv", 0, 10000, t0)}}, &st)
	if !d.GridPresent {
		t.Fatal("a meter without voltage points should not report a grid outage")
	}
}

func TestDefaultSettingsAreValid(t *testing.T) {
	if err := DefaultSettings().Validate(); err != nil {
		t.Fatal(err)
	}
	s := DefaultSettings()
	s.ExportLimitW = -1
	if s.Validate() == nil {
		t.Fatal("negative export limit accepted")
	}
}

func TestZeroExportRampsUpFromCurrentOutput(t *testing.T) {
	s := controlSettings()
	var st State
	// Importing 2 kW while producing 5 kW of 10 kW: 1.8 kW of headroom.
	m := Measurements{Now: t0, Grid: grid(2000, t0), Inverters: []Inverter{inverter("inv", 5000, 10000, t0)}}
	d := Step(s, m, &st)
	if d.Status != StatusCurtailing || d.Binding != "grid" || d.HeadroomW != 1800 {
		t.Fatalf("decision = %+v", d)
	}
	if d.LimitPct != 50 || !d.Send {
		t.Fatalf("first cycle should hold current output: %+v", d)
	}

	m.Now = t0.Add(time.Second)
	m.Grid.Updated, m.Inverters[0].Updated = m.Now, m.Now
	d = Step(s, m, &st)
	if d.LimitPct != 55 { // +5 %/s of 10 kW
		t.Fatalf("ramp = %v %%, want 55", d.LimitPct)
	}
}

func TestExportBreachCutsImmediately(t *testing.T) {
	s := controlSettings()
	var st State
	m := Measurements{Now: t0, Grid: grid(0, t0), Inverters: []Inverter{inverter("inv", 8000, 10000, t0)}}
	if d := Step(s, m, &st); d.LimitW != 7800 {
		t.Fatalf("first limit = %v, want 7800", d.LimitW)
	}

	// The inverter followed its command, then the load drops and the site
	// exports 1.5 kW: cut by export + margin at once.
	m.Now = t0.Add(time.Second)
	m.Grid = grid(-1500, m.Now)
	m.Inverters[0] = inverter("inv", 7800, 10000, m.Now)
	d := Step(s, m, &st)
	if d.LimitW != 6100 || !d.Send {
		t.Fatalf("decision = %+v", d)
	}
}

func TestBreachWithLaggingInverterPushesHarder(t *testing.T) {
	s := controlSettings()
	var st State
	m := Measurements{Now: t0, Grid: grid(0, t0), Inverters: []Inverter{inverter("inv", 8000, 10000, t0)}}
	Step(s, m, &st) // commands 7800 W

	// The inverter is still at 8000 W, above its 7800 W command, while
	// the site exports: lower the command from 7800 W with double gain.
	m.Now = t0.Add(time.Second)
	m.Grid = grid(-1500, m.Now)
	m.Inverters[0].Updated = m.Now
	d := Step(s, m, &st)
	if d.LimitW != 7800-2*1700 {
		t.Fatalf("limit = %v, want 4400", d.LimitW)
	}
}

func TestExportLimitAboveZero(t *testing.T) {
	s := controlSettings()
	s.ExportLimitW = 3000
	var st State
	m := Measurements{Now: t0, Grid: grid(-2000, t0), Inverters: []Inverter{inverter("inv", 9000, 20000, t0)}}
	d := Step(s, m, &st)
	if d.HeadroomW != 800 {
		t.Fatalf("headroom = %v, want 800", d.HeadroomW)
	}
}

func TestExportAlarmAfterDelay(t *testing.T) {
	s := controlSettings()
	var st State
	for i := range 12 {
		now := t0.Add(time.Duration(i) * time.Second)
		m := Measurements{Now: now, Grid: grid(-500, now), Inverters: []Inverter{inverter("inv", 0, 10000, now)}}
		d := Step(s, m, &st)
		if alarmed := hasAlarm(d, AlarmExportExceeded); alarmed != (i >= 10) {
			t.Fatalf("second %d: export alarm = %v", i, alarmed)
		}
	}
}

func TestGensetMinimumLoad(t *testing.T) {
	s := controlSettings()
	var st State
	// 50 kW genset carrying 18 kW, minimum 30 % = 15 kW, margin 500 W.
	m := Measurements{
		Now:       t0,
		Gensets:   []Genset{{Meter: Meter{ID: "dg1", Online: true, Updated: t0, PowerW: 18000}, RatedW: 50000}},
		Inverters: []Inverter{inverter("inv", 10000, 30000, t0)},
	}
	d := Step(s, m, &st)
	if d.Binding != "genset" || d.HeadroomW != 2500 || d.GensetMinLoadW != 15000 || d.GensetsRunning != 1 {
		t.Fatalf("decision = %+v", d)
	}
}

func TestGensetReversePowerIsCritical(t *testing.T) {
	s := controlSettings()
	var st State
	m := Measurements{
		Now:       t0,
		Gensets:   []Genset{{Meter: Meter{ID: "dg1", Online: true, Updated: t0, PowerW: -800}, RatedW: 50000}},
		Inverters: []Inverter{inverter("inv", 20000, 30000, t0)},
	}
	d := Step(s, m, &st)
	if !hasAlarm(d, AlarmGensetReversePower) {
		t.Fatal("no reverse power alarm")
	}
	// -800 - 15000 - 500 = -16300 W of headroom: shed hard.
	if d.LimitW != 3700 {
		t.Fatalf("limit = %v, want 3700", d.LimitW)
	}
}

func TestTightestConstraintWins(t *testing.T) {
	s := controlSettings()
	var st State
	m := Measurements{
		Now:       t0,
		Grid:      grid(5000, t0), // grid allows 4800 W more
		Gensets:   []Genset{{Meter: Meter{ID: "dg1", Online: true, Updated: t0, PowerW: 16000}, RatedW: 50000}},
		Inverters: []Inverter{inverter("inv", 10000, 30000, t0)},
	}
	d := Step(s, m, &st)
	if d.Binding != "genset" || d.HeadroomW != 500 {
		t.Fatalf("decision = %+v", d)
	}
}

func TestStaleGridMeterTriggersFailsafe(t *testing.T) {
	s := controlSettings()
	s.FailsafeLimitPct = 10
	var st State
	stale := t0.Add(-10 * time.Second)
	m := Measurements{Now: t0, Grid: grid(0, stale), Inverters: []Inverter{inverter("inv", 9000, 10000, t0)}}
	d := Step(s, m, &st)
	if d.Status != StatusFailsafe || d.LimitPct != 10 || !d.Send || !hasAlarm(d, AlarmGridMeterOffline) {
		t.Fatalf("decision = %+v", d)
	}

	// Recovery ramps up from the fail-safe level instead of jumping.
	m.Now = t0.Add(time.Second)
	m.Grid = grid(5000, m.Now)
	m.Inverters[0] = inverter("inv", 1000, 10000, m.Now)
	d = Step(s, m, &st)
	if d.Status != StatusCurtailing || d.LimitPct != 15 {
		t.Fatalf("recovery = %+v", d)
	}
}

func TestMonitorModeNeverWritesUntilReleased(t *testing.T) {
	s := controlSettings()
	var st State
	m := Measurements{Now: t0, Grid: grid(-3000, t0), Inverters: []Inverter{inverter("inv", 9000, 10000, t0)}}
	if d := Step(s, m, &st); !d.Send || d.LimitPct >= 100 {
		t.Fatalf("control cycle = %+v", d)
	}

	s.Mode = ModeMonitor
	d := Step(s, m, &st)
	if d.Status != StatusMonitoring || !d.Send || d.LimitPct != 100 {
		t.Fatalf("switching to monitor should release once: %+v", d)
	}
	if d = Step(s, m, &st); d.Send {
		t.Fatal("monitor mode wrote again")
	}

	var fresh State
	if d = Step(s, m, &fresh); d.Send {
		t.Fatal("monitor mode wrote without a previous limit")
	}
}

func TestWritePolicy(t *testing.T) {
	s := controlSettings()
	var st State
	at := func(sec int, gridW, pvW float64) Decision {
		now := t0.Add(time.Duration(sec) * time.Second)
		return Step(s, Measurements{Now: now, Grid: grid(gridW, now), Inverters: []Inverter{inverter("inv", pvW, 10000, now)}}, &st)
	}
	if d := at(0, -1000, 5000); !d.Send {
		t.Fatal("first limit not sent")
	}
	// Headroom 30 W: an increase of 0.3 % is below the 1 % step.
	if d := at(1, 230, 3800); d.Send {
		t.Fatalf("tiny increase sent: %+v", d)
	}
	// A breach is always sent, however small.
	if d := at(2, -50, 3800); !d.Send {
		t.Fatalf("breach not sent: %+v", d)
	}
	// Keepalive after 60 s with no change.
	if d := at(63, 200, 3550); !d.Send {
		t.Fatalf("keepalive not sent: %+v", d)
	}
}

func TestUncontrollableInverterIsExcluded(t *testing.T) {
	s := controlSettings()
	var st State
	ro := inverter("ro", 4000, 5000, t0)
	ro.Controllable = false
	m := Measurements{Now: t0, Grid: grid(1000, t0), Inverters: []Inverter{inverter("inv", 5000, 10000, t0), ro}}
	d := Step(s, m, &st)
	if d.ControllableW != 10000 || d.PVPowerW != 9000 || !hasAlarm(d, AlarmInverterUncontrolled) {
		t.Fatalf("decision = %+v", d)
	}
}

func TestNoConstraintRunsUnlimited(t *testing.T) {
	s := controlSettings()
	var st State
	d := Step(s, Measurements{Now: t0, Inverters: []Inverter{inverter("inv", 5000, 10000, t0)}}, &st)
	if d.Status != StatusUnlimited || d.LimitPct != 100 || !math.IsNaN(d.HeadroomW) {
		t.Fatalf("decision = %+v", d)
	}
}

func TestGridOutageWithoutGensetRaisesNoSource(t *testing.T) {
	s := controlSettings()
	var st State
	g := grid(0, t0)
	g.VoltageV = 0 // measured: the grid is down
	d := Step(s, Measurements{Now: t0, Grid: g, Inverters: []Inverter{inverter("inv", 0, 10000, t0)}}, &st)
	if d.GridPresent || !hasAlarm(d, AlarmNoSource) {
		t.Fatalf("decision = %+v", d)
	}
}

// TestClosedLoop runs the controller against a simple site model for ten
// minutes of swinging load, moving sun and sudden 20+ kW load drops, with
// slow, medium and fast inverters.
func TestClosedLoop(t *testing.T) {
	for _, response := range []float64{0.3, 0.6, 0.9} {
		s := controlSettings()
		s.ExportLimitW = 1000
		const rated = 50000.0
		var st State

		pv, limit := 0.0, rated
		var exports []float64
		overshootSeconds := 0
		for sec := range 600 {
			now := t0.Add(time.Duration(sec) * time.Second)
			phase := sec % 120
			load := 30000 + 15000*math.Sin(float64(sec)/40)
			if phase > 100 {
				load = 8000
			}
			sun := rated * (0.6 + 0.4*math.Sin(float64(sec)/25))

			// The inverter moves a fraction of the way to its target
			// each second.
			pv += response * (math.Min(sun, limit) - pv)
			gridW := load - pv
			exports = append(exports, -gridW)

			// Right after a sudden drop the inverter lag is physics, not
			// the controller: skip those seconds.
			afterStep := (phase > 100 && phase < 107) || phase < 3
			if !afterStep && -gridW > s.ExportLimitW+100 {
				overshootSeconds++
			}

			d := Step(s, Measurements{Now: now, Grid: grid(gridW, now), Inverters: []Inverter{inverter("inv", pv, rated, now)}}, &st)
			if d.Send {
				limit = d.LimitW
			}
		}

		if overshootSeconds > 15 {
			t.Errorf("response %.1f: export above limit + 100 W for %d s", response, overshootSeconds)
		}
		for i := 60; i <= len(exports); i++ {
			mean := 0.0
			for _, e := range exports[i-60 : i] {
				mean += e / 60
			}
			if mean > s.ExportLimitW {
				t.Errorf("response %.1f: 60 s mean export %.0f W ending at %d s is above the limit", response, mean, i)
				break
			}
		}
	}
}

func genset(id string, powerW, ratedW float64, at time.Time) Genset {
	return Genset{Meter: Meter{ID: id, Online: true, Updated: at, PowerW: powerW}, RatedW: ratedW}
}

func TestGensetRunningSignalIsDebounced(t *testing.T) {
	s := controlSettings()
	var st State
	off, on := false, true
	step := func(sec float64, running *bool) Decision {
		now := t0.Add(time.Duration(sec * float64(time.Second)))
		g := genset("dg1", 20000, 50000, now)
		g.Running = running
		return Step(s, Measurements{Now: now, Gensets: []Genset{g}, Inverters: []Inverter{inverter("inv", 5000, 30000, now)}}, &st)
	}
	if d := step(0, &off); d.GensetsRunning != 0 {
		t.Fatal("explicit 'not running' signal ignored in favour of power")
	}
	// An ATS transfer: the signal flickers on for one second.
	if d := step(1, &on); d.GensetsRunning != 0 {
		t.Fatal("reacted to a flicker")
	}
	if d := step(1.5, &off); d.GensetsRunning != 0 {
		t.Fatal("reacted to a flicker")
	}
	step(2, &on)
	step(3, &on)
	if d := step(4.1, &on); d.GensetsRunning != 1 || d.Binding != "genset" {
		t.Fatalf("running not committed after the debounce: %+v", d)
	}
}

func TestGensetAbsoluteMinimumLoad(t *testing.T) {
	s := controlSettings()
	var st State
	g := genset("dg1", 30000, 100000, t0)
	g.MinLoadW = 25000 // the site's 25 kW floor, instead of 30 %
	d := Step(s, Measurements{Now: t0, Gensets: []Genset{g}, Inverters: []Inverter{inverter("inv", 5000, 30000, t0)}}, &st)
	if d.GensetMinLoadW != 25000 || d.HeadroomW != 30000-25000-500 {
		t.Fatalf("decision = %+v", d)
	}
}

func TestLimitBounds(t *testing.T) {
	s := controlSettings()
	s.MinLimitPct, s.MaxLimitPct = 10, 80
	var st State
	// Massive export would call for 0 %: held at the 10 % floor.
	d := Step(s, Measurements{Now: t0, Grid: grid(-50000, t0), Inverters: []Inverter{inverter("inv", 9000, 10000, t0)}}, &st)
	if d.LimitPct != 10 {
		t.Fatalf("limit = %v %%, want the 10 %% floor", d.LimitPct)
	}
	var st2 State
	d = Step(s, Measurements{Now: t0, Inverters: []Inverter{inverter("inv", 9000, 10000, t0)}}, &st2)
	if d.LimitPct != 80 {
		t.Fatalf("unconstrained limit = %v %%, want the 80 %% cap", d.LimitPct)
	}
}

func TestFailsafeHoldKeepsLastLimit(t *testing.T) {
	s := controlSettings()
	s.FailsafeAction = FailsafeHold
	var st State
	m := Measurements{Now: t0, Grid: grid(-1000, t0), Inverters: []Inverter{inverter("inv", 6000, 10000, t0)}}
	first := Step(s, m, &st)

	m.Now = t0.Add(10 * time.Second) // grid reading now stale
	m.Inverters[0].Updated = m.Now
	d := Step(s, m, &st)
	if d.Status != StatusFailsafe || d.LimitPct != first.LimitPct || d.Send {
		t.Fatalf("hold = %+v, first = %v %%", d, first.LimitPct)
	}

	var fresh State
	d = Step(s, Measurements{Now: t0, Grid: grid(0, t0.Add(-time.Minute)), Inverters: []Inverter{inverter("inv", 6000, 10000, t0)}}, &fresh)
	if d.Send {
		t.Fatal("hold sent a limit before ever having one")
	}
}

func TestSettingsJSONFillsNewFieldsWithDefaults(t *testing.T) {
	var s Settings
	old := `{"mode":"control","export_limit_w":5000,"grid_margin_w":200,"grid_present_min_voltage":150,
		"genset_min_load_pct":30,"genset_margin_w":500,"genset_running_min_w":500,"ramp_up_pct_per_sec":5,
		"failsafe_limit_pct":0,"measurement_timeout":"5s","min_step_pct":1,"keepalive_interval":"1m0s","export_alarm_delay":"10s"}`
	if err := json.Unmarshal([]byte(old), &s); err != nil {
		t.Fatal(err)
	}
	if s.ExportLimitW != 5000 || s.MaxLimitPct != 100 || s.FailsafeAction != FailsafeLimit || s.SourceDebounce.D() != 2*time.Second {
		t.Fatalf("settings = %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"mode":"control","typo":1}`), &s); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestSourceMeterFollowsATS(t *testing.T) {
	s := controlSettings()
	s.ExportLimitW = 1000
	var st State
	step := func(sec float64, powerW float64, running bool) Decision {
		now := t0.Add(time.Duration(sec * float64(time.Second)))
		src := &SourceMeter{Meter: Meter{ID: "meter1", Online: true, Updated: now, PowerW: powerW, VoltageV: 230, HasVoltage: true},
			RatedW: 100000, MinLoadW: 25000, GensetRunning: running}
		return Step(s, Measurements{Now: now, Source: src, Inverters: []Inverter{inverter("inv", 30000, 60000, now)}}, &st)
	}

	// On grid: export limit applies. Exporting 500 W, limit 1000, margin 200.
	d := step(0, -500, false)
	if d.Source != "grid" || d.Binding != "grid" || d.HeadroomW != 300 {
		t.Fatalf("grid mode = %+v", d)
	}
	// A one-second flicker of the signal is ignored.
	if d = step(1, -500, true); d.Source != "grid" {
		t.Fatal("followed a flicker")
	}
	step(1.5, -500, false)
	// The ATS transfers; after the debounce the meter is the genset.
	step(2, 40000, true)
	step(3, 40000, true)
	d = step(4.5, 40000, true)
	if d.Source != "genset" || d.Binding != "genset" || d.GensetsRunning != 1 || d.GensetMinLoadW != 25000 {
		t.Fatalf("genset mode = %+v", d)
	}
	// 40 kW - 25 kW floor - 500 W margin.
	if d.HeadroomW != 14500 {
		t.Fatalf("headroom = %v", d.HeadroomW)
	}
}

func TestSourceMeterOfflineTriggersFailsafe(t *testing.T) {
	s := controlSettings()
	var st State
	src := &SourceMeter{Meter: Meter{ID: "meter1", Online: false, Updated: t0}, RatedW: 100000}
	d := Step(s, Measurements{Now: t0, Source: src, Inverters: []Inverter{inverter("inv", 30000, 60000, t0)}}, &st)
	if d.Status != StatusFailsafe || d.Source != "" {
		t.Fatalf("decision = %+v", d)
	}
}
