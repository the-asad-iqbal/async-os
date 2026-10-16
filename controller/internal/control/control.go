// Package control computes PV power limits that keep grid export within the
// agreed limit and keep running gensets above their minimum load.
//
// The algorithm is a feedback loop on measured power. Each cycle it works
// out how much PV the site can absorb (headroom) under every active
// constraint, takes the tightest one, and commands the inverters to the
// result. Cutting power is immediate; raising it is ramped so the loop does
// not overshoot while inverters and meters catch up.
package control

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// Mode selects whether the controller writes to inverters.
type Mode string

// Modes.
const (
	// ModeMonitor reads and records, but never writes to inverters.
	ModeMonitor Mode = "monitor"
	// ModeControl actively limits PV power.
	ModeControl Mode = "control"
)

// Settings tune the controller. Installers set them during commissioning.
type Settings struct {
	Mode Mode `json:"mode"`

	// ExportLimitW is the most power the site may export to the grid.
	// Zero means zero export.
	ExportLimitW float64 `json:"export_limit_w"`
	// GridMarginW is kept in reserve below the export limit so short
	// load drops do not breach it.
	GridMarginW float64 `json:"grid_margin_w"`
	// GridPresentMinVoltage is the voltage above which the grid meter
	// counts as "grid available". Zero assumes the grid is present
	// whenever its meter answers.
	GridPresentMinVoltage float64 `json:"grid_present_min_voltage"`

	// GensetMinLoadPct is the minimum load per running genset, in percent
	// of its rating. Keeps gensets out of reverse power and wet stacking.
	GensetMinLoadPct float64 `json:"genset_min_load_pct"`
	// GensetMarginW is kept in reserve above the genset minimum load.
	GensetMarginW float64 `json:"genset_margin_w"`
	// GensetRunningMinW: a genset delivering more than this counts as
	// running when its meter has no explicit running signal.
	GensetRunningMinW float64 `json:"genset_running_min_w"`

	// RampUpPctPerSec limits how fast the total PV limit may rise, in
	// percent of controllable rated power per second.
	RampUpPctPerSec float64 `json:"ramp_up_pct_per_sec"`
	// FailsafeAction is what happens when a required measurement is
	// missing: "limit" curtails to FailsafeLimitPct, "hold" keeps the last
	// limit sent.
	FailsafeAction string `json:"failsafe_action"`
	// FailsafeLimitPct is applied by the "limit" fail-safe action.
	FailsafeLimitPct float64 `json:"failsafe_limit_pct"`
	// MinLimitPct and MaxLimitPct bound every limit sent in normal
	// operation, e.g. to never fully stop or never exceed a contract cap.
	MinLimitPct float64 `json:"min_limit_pct"`
	MaxLimitPct float64 `json:"max_limit_pct"`
	// SourceDebounce: a change in a genset's running state must hold this
	// long before the controller acts on it, so an ATS transfer does not
	// whipsaw the inverters.
	SourceDebounce Duration `json:"source_debounce"`
	// MeasurementTimeout marks a meter reading as stale.
	MeasurementTimeout Duration `json:"measurement_timeout"`

	// MinStepPct: increases smaller than this are not sent, to spare
	// inverters from constant writes. Decreases are always sent.
	MinStepPct float64 `json:"min_step_pct"`
	// KeepaliveInterval re-sends the current limit even when unchanged.
	KeepaliveInterval Duration `json:"keepalive_interval"`
	// ExportAlarmDelay is how long export may exceed the limit before an
	// alarm is raised.
	ExportAlarmDelay Duration `json:"export_alarm_delay"`
}

// DefaultSettings are conservative values for a new site: zero export,
// monitor-only until an installer switches control on.
func DefaultSettings() Settings {
	return Settings{
		Mode:                  ModeMonitor,
		ExportLimitW:          0,
		GridMarginW:           200,
		GridPresentMinVoltage: 150,
		GensetMinLoadPct:      30,
		GensetMarginW:         500,
		GensetRunningMinW:     500,
		RampUpPctPerSec:       5,
		FailsafeAction:        FailsafeLimit,
		FailsafeLimitPct:      0,
		MinLimitPct:           0,
		MaxLimitPct:           100,
		SourceDebounce:        Duration(2 * time.Second),
		MeasurementTimeout:    Duration(5 * time.Second),
		MinStepPct:            1,
		KeepaliveInterval:     Duration(60 * time.Second),
		ExportAlarmDelay:      Duration(10 * time.Second),
	}
}

// Validate rejects settings that would make the controller unsafe.
func (s Settings) Validate() error {
	switch {
	case s.Mode != ModeMonitor && s.Mode != ModeControl:
		return fmt.Errorf("mode must be %q or %q", ModeMonitor, ModeControl)
	case s.ExportLimitW < 0:
		return fmt.Errorf("export limit cannot be negative")
	case s.GridMarginW < 0 || s.GensetMarginW < 0:
		return fmt.Errorf("margins cannot be negative")
	case s.GensetMinLoadPct < 0 || s.GensetMinLoadPct > 90:
		return fmt.Errorf("genset minimum load must be between 0 and 90 %%")
	case s.RampUpPctPerSec <= 0 || s.RampUpPctPerSec > 100:
		return fmt.Errorf("ramp-up rate must be above 0 and at most 100 %%/s")
	case s.FailsafeAction != FailsafeLimit && s.FailsafeAction != FailsafeHold:
		return fmt.Errorf("fail-safe action must be %q or %q", FailsafeLimit, FailsafeHold)
	case s.FailsafeLimitPct < 0 || s.FailsafeLimitPct > 100:
		return fmt.Errorf("fail-safe limit must be between 0 and 100 %%")
	case s.MinLimitPct < 0 || s.MaxLimitPct > 100 || s.MinLimitPct >= s.MaxLimitPct:
		return fmt.Errorf("inverter limit bounds must satisfy 0 <= minimum < maximum <= 100 %%")
	case s.FailsafeLimitPct > s.MaxLimitPct:
		return fmt.Errorf("fail-safe limit cannot exceed the maximum limit")
	case s.SourceDebounce.D() < 0 || s.SourceDebounce.D() > 30*time.Second:
		return fmt.Errorf("source debounce must be between 0 and 30 s")
	case s.MeasurementTimeout.D() < time.Second || s.MeasurementTimeout.D() > time.Minute:
		return fmt.Errorf("measurement timeout must be between 1 s and 60 s")
	case s.MinStepPct < 0 || s.MinStepPct > 10:
		return fmt.Errorf("minimum step must be between 0 and 10 %%")
	case s.KeepaliveInterval.D() < 5*time.Second:
		return fmt.Errorf("keepalive interval must be at least 5 s")
	}
	return nil
}

// Fail-safe actions.
const (
	FailsafeLimit = "limit"
	FailsafeHold  = "hold"
)

// UnmarshalJSON starts from DefaultSettings, so settings saved by an
// older version gain safe values for fields added since.
func (s *Settings) UnmarshalJSON(data []byte) error {
	type plain Settings
	v := plain(DefaultSettings())
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return err
	}
	*s = Settings(v)
	return nil
}

// Meter is a grid or genset meter as seen in one cycle.
type Meter struct {
	ID     string
	Online bool
	// Updated is when the reading was taken.
	Updated time.Time
	// PowerW: grid meters positive when importing from the grid; genset
	// meters positive when the genset supplies the site.
	PowerW float64
	// VoltageV is the highest phase voltage; only meaningful when
	// HasVoltage is set.
	VoltageV   float64
	HasVoltage bool
}

// Genset is a genset with its output meter.
type Genset struct {
	Meter
	RatedW float64
	// MinLoadW, when above zero, is this genset's minimum load in watts
	// and replaces the percentage setting.
	MinLoadW float64
	// Running is the genset's own running signal (e.g. a digital input),
	// or nil to infer it from output power.
	Running *bool
}

// Inverter is one PV inverter as seen in one cycle.
type Inverter struct {
	ID      string
	Online  bool
	Updated time.Time
	PowerW  float64
	RatedW  float64
	// Controllable is false for inverters without a power limit
	// control; their output is measured but cannot be curtailed.
	Controllable bool
}

// Measurements are the inputs to one control cycle.
type Measurements struct {
	Now       time.Time
	Grid      *Meter // nil when the site has no grid connection
	Gensets   []Genset
	Inverters []Inverter
}

// Status summarizes what the controller is doing.
type Status string

// Statuses.
const (
	StatusMonitoring Status = "monitoring"
	StatusUnlimited  Status = "unlimited"  // no constraint is binding
	StatusCurtailing Status = "curtailing" // PV is held back
	StatusFailsafe   Status = "failsafe"   // a required input is missing
	StatusNoPV       Status = "no_pv"      // nothing to control
)

// Alarm codes.
const (
	AlarmGridMeterOffline     = "grid_meter_offline"
	AlarmGensetMeterOffline   = "genset_meter_offline"
	AlarmInverterOffline      = "inverter_offline"
	AlarmInverterUncontrolled = "inverter_uncontrolled"
	AlarmExportExceeded       = "export_limit_exceeded"
	AlarmGensetReversePower   = "genset_reverse_power"
	AlarmGensetUnderMinLoad   = "genset_under_min_load"
	AlarmNoSource             = "no_source"
)

// Alarm is a condition that needs attention.
type Alarm struct {
	Code   string `json:"code"`
	Device string `json:"device,omitempty"`
	// Critical alarms mean the site may be outside its limits.
	Critical bool   `json:"critical"`
	Message  string `json:"message"`
}

// Decision is the result of one control cycle.
type Decision struct {
	Status Status `json:"status"`
	Reason string `json:"reason"`

	// LimitW is the total power limit across controllable inverters.
	LimitW float64 `json:"limit_w"`
	// LimitPct is the limit as a percentage of controllable rated power,
	// sent to every controllable inverter.
	LimitPct float64 `json:"limit_pct"`
	// Send is true when the limit should be written this cycle.
	Send bool `json:"-"`

	// HeadroomW is how much more PV the site could absorb (negative:
	// how much must be shed). NaN when no constraint is active.
	HeadroomW float64 `json:"-"`
	// Binding names the constraint that set the limit: "grid", "genset"
	// or "" when none did.
	Binding string `json:"binding"`

	PVPowerW       float64 `json:"pv_power_w"`
	ControllableW  float64 `json:"controllable_rated_w"`
	GridPresent    bool    `json:"grid_present"`
	GensetsRunning int     `json:"gensets_running"`
	GensetMinLoadW float64 `json:"genset_min_load_w"`
	Alarms         []Alarm `json:"alarms"`
}

// State carries memory between cycles.
type State struct {
	initialized   bool
	lastTime      time.Time
	limitW        float64
	sentPct       float64
	sentAt        time.Time
	exportSince   time.Time
	running       map[string]*debounced
	controllableW float64
}

// Step runs one control cycle.
func Step(s Settings, m Measurements, state *State) Decision {
	d := Decision{HeadroomW: math.NaN()}

	fresh := func(online bool, updated time.Time) bool {
		return online && m.Now.Sub(updated) <= s.MeasurementTimeout.D()
	}

	// Inverters: what is producing, and what we can curtail.
	var controllableRated, controllablePower float64
	for _, inv := range m.Inverters {
		online := fresh(inv.Online, inv.Updated)
		if online {
			d.PVPowerW += inv.PowerW
		} else {
			d.Alarms = append(d.Alarms, Alarm{Code: AlarmInverterOffline, Device: inv.ID, Message: "Inverter is not responding"})
			continue
		}
		if !inv.Controllable {
			d.Alarms = append(d.Alarms, Alarm{Code: AlarmInverterUncontrolled, Device: inv.ID, Message: "Inverter has no power limit control; its output cannot be curtailed"})
			continue
		}
		controllableRated += inv.RatedW
		controllablePower += inv.PowerW
	}
	d.ControllableW = controllableRated

	// Constraints. Each yields headroom: how much controllable PV may
	// change before the constraint is hit.
	failsafe := ""
	headroom := math.Inf(1)

	if m.Grid != nil {
		if !fresh(m.Grid.Online, m.Grid.Updated) {
			failsafe = "Grid meter is not responding"
			d.Alarms = append(d.Alarms, Alarm{Code: AlarmGridMeterOffline, Device: m.Grid.ID, Critical: true, Message: failsafe})
		} else {
			d.GridPresent = s.GridPresentMinVoltage == 0 || !m.Grid.HasVoltage || m.Grid.VoltageV >= s.GridPresentMinVoltage
			if d.GridPresent {
				// Import + allowed export - margin.
				h := m.Grid.PowerW + s.ExportLimitW - s.GridMarginW
				if h < headroom {
					headroom, d.Binding = h, "grid"
				}
				exportW := -m.Grid.PowerW
				if exportW > s.ExportLimitW {
					if state.exportSince.IsZero() {
						state.exportSince = m.Now
					}
					if m.Now.Sub(state.exportSince) >= s.ExportAlarmDelay.D() {
						d.Alarms = append(d.Alarms, Alarm{Code: AlarmExportExceeded, Device: m.Grid.ID, Critical: true,
							Message: fmt.Sprintf("Export %.0f W is above the %.0f W limit", exportW, s.ExportLimitW)})
					}
				} else {
					state.exportSince = time.Time{}
				}
			}
		}
	}

	var gensetPower float64
	for _, g := range m.Gensets {
		if !fresh(g.Online, g.Updated) {
			if failsafe == "" {
				failsafe = "Genset meter is not responding"
			}
			d.Alarms = append(d.Alarms, Alarm{Code: AlarmGensetMeterOffline, Device: g.ID, Critical: true, Message: "Genset meter is not responding"})
			continue
		}
		raw := g.PowerW >= s.GensetRunningMinW || g.PowerW < 0
		if g.Running != nil {
			raw = *g.Running
		}
		if !state.debounce(g.ID, raw, m.Now, s.SourceDebounce.D()) {
			continue // stopped
		}
		d.GensetsRunning++
		gensetPower += g.PowerW
		minLoad := s.GensetMinLoadPct / 100 * g.RatedW
		if g.MinLoadW > 0 {
			minLoad = g.MinLoadW
		}
		d.GensetMinLoadW += minLoad
		if g.PowerW < 0 {
			d.Alarms = append(d.Alarms, Alarm{Code: AlarmGensetReversePower, Device: g.ID, Critical: true,
				Message: fmt.Sprintf("Genset is absorbing %.0f W (reverse power)", -g.PowerW)})
		} else if g.PowerW < minLoad {
			d.Alarms = append(d.Alarms, Alarm{Code: AlarmGensetUnderMinLoad, Device: g.ID,
				Message: fmt.Sprintf("Genset load %.0f W is below its %.0f W minimum", g.PowerW, minLoad)})
		}
	}
	if d.GensetsRunning > 0 {
		h := gensetPower - d.GensetMinLoadW - s.GensetMarginW
		if h < headroom {
			headroom, d.Binding = h, "genset"
		}
	}

	if m.Grid != nil && failsafe == "" && !d.GridPresent && d.GensetsRunning == 0 {
		d.Alarms = append(d.Alarms, Alarm{Code: AlarmNoSource, Message: "Neither the grid nor a genset is supplying the site"})
	}

	// Rated power changes (an inverter dropped out) rescale the limit.
	if state.initialized && state.controllableW > 0 && controllableRated != state.controllableW {
		state.limitW *= controllableRated / state.controllableW
	}
	state.controllableW = controllableRated

	dt := 0.0
	if state.initialized {
		// A stalled loop must not turn into one big jump up.
		dt = math.Min(maxRampStep.Seconds(), math.Max(0, m.Now.Sub(state.lastTime).Seconds()))
	}
	state.lastTime = m.Now

	switch {
	case s.Mode == ModeMonitor:
		d.Status, d.Reason = StatusMonitoring, "Monitoring only, inverters are not limited"
		d.HeadroomW = finite(headroom)
		d.LimitW, d.LimitPct = controllableRated, 100
		state.initialized = false
		if !state.sentAt.IsZero() && state.sentPct < 100 {
			// Control was switched off: hand full output back once, or
			// the inverters would keep the last limit indefinitely.
			d.Send = true
			state.sentPct, state.sentAt = 100, m.Now
		}
		return d
	case controllableRated <= 0:
		d.Status, d.Reason = StatusNoPV, "No controllable inverter is online"
		d.HeadroomW = finite(headroom)
		state.initialized = false
		return d
	}

	var target float64
	switch {
	case failsafe != "" && s.FailsafeAction == FailsafeHold:
		d.Status, d.Reason = StatusFailsafe, failsafe+"; holding the last limit"
		if !state.initialized {
			d.Reason = failsafe + "; no limit sent yet, waiting for measurements"
			d.LimitW, d.LimitPct = controllableRated, 100
			return d
		}
		target = state.limitW
	case failsafe != "":
		d.Status, d.Reason = StatusFailsafe, failsafe
		target = s.FailsafeLimitPct / 100 * controllableRated
		state.limitW = target // fail-safe is applied at once, no ramp
	case math.IsInf(headroom, 1):
		// No grid or genset constraint: nothing to protect.
		d.Status, d.Reason = StatusUnlimited, "No export or genset constraint is active"
		target = controllableRated
	default:
		d.HeadroomW = headroom
		target = controllablePower + headroom
		if headroom < 0 && state.initialized && state.limitW < controllablePower {
			// Inverters are still above the last command while the site
			// is over a limit. Lower the command from where it was, not
			// from the lagging output, and with extra gain, so a falling
			// load is tracked instead of chased. Over-curtailing briefly
			// only costs energy; the ramp-up recovers it.
			target = state.limitW + breachGain*headroom
		}
	}
	target = math.Max(0, math.Min(controllableRated, target))
	target = math.Min(target, s.MaxLimitPct/100*controllableRated)
	if failsafe == "" {
		target = math.Max(target, s.MinLimitPct/100*controllableRated)
	}

	limit := target
	if failsafe == "" && state.initialized && target > state.limitW {
		// Ramp up gradually; never below what is already allowed.
		maxRise := s.RampUpPctPerSec / 100 * controllableRated * dt
		limit = math.Min(target, state.limitW+maxRise)
	}
	if !state.initialized && failsafe == "" && d.Binding != "" {
		// First cycle: start from what the inverters produce now rather
		// than jumping to full power.
		limit = math.Min(target, math.Max(controllablePower, 0))
	}
	state.limitW = limit
	state.initialized = true

	d.LimitW = limit
	d.LimitPct = math.Round(limit/controllableRated*1000) / 10
	if d.Status == "" {
		if d.LimitPct < 100 {
			d.Status, d.Reason = StatusCurtailing, curtailReason(d.Binding)
		} else {
			d.Status, d.Reason = StatusUnlimited, "Constraints allow full PV output"
		}
	}

	// Write policy: decreases always, increases in steps, plus keepalive.
	switch {
	case state.sentAt.IsZero():
		d.Send = true
	case d.LimitPct < state.sentPct && (state.sentPct-d.LimitPct >= s.MinStepPct/2 || headroom < 0):
		// Cut at once when a constraint is breached; otherwise skip
		// tiny decreases caused by measurement noise.
		d.Send = true
	case d.LimitPct-state.sentPct >= s.MinStepPct:
		d.Send = true
	case d.LimitPct >= 100 && state.sentPct < 100:
		d.Send = true
	case m.Now.Sub(state.sentAt) >= s.KeepaliveInterval.D():
		d.Send = true
	}
	if d.Send {
		state.sentPct, state.sentAt = d.LimitPct, m.Now
	}
	return d
}

// breachGain scales the correction while a limit is breached and the
// inverters lag behind their command.
const breachGain = 2

// maxRampStep caps the time step used for ramping.
const maxRampStep = 5 * time.Second

// debounced is a boolean that only changes after holding its new value.
type debounced struct {
	value   bool
	pending bool
	since   time.Time
}

// debounce returns the committed value for id. The first observation is
// taken as is; later changes must hold for delay.
func (st *State) debounce(id string, raw bool, now time.Time, delay time.Duration) bool {
	if st.running == nil {
		st.running = map[string]*debounced{}
	}
	d, ok := st.running[id]
	if !ok {
		st.running[id] = &debounced{value: raw, pending: raw}
		return raw
	}
	switch {
	case raw == d.value:
		d.pending = raw
	case raw != d.pending:
		d.pending, d.since = raw, now
	case now.Sub(d.since) >= delay:
		d.value = raw
	}
	return d.value
}

// ForceResend makes the next cycle write the limit, e.g. after an inverter
// reconnects and may have lost its setpoint.
func (st *State) ForceResend() { st.sentAt = time.Time{} }

func curtailReason(binding string) string {
	switch binding {
	case "grid":
		return "Holding export at the grid limit"
	case "genset":
		return "Keeping gensets above minimum load"
	default:
		return "Ramping up"
	}
}

func finite(v float64) float64 {
	if math.IsInf(v, 0) {
		return math.NaN()
	}
	return v
}
