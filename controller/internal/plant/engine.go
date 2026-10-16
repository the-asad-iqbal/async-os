package plant

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/the-asad-iqbal/async-os/controller/internal/control"
	"github.com/the-asad-iqbal/async-os/controller/internal/devices"
	"github.com/the-asad-iqbal/async-os/controller/internal/modbus"
)

// DeviceStatus is the live state of one device.
type DeviceStatus struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Role         Role            `json:"role"`
	Profile      string          `json:"profile"`
	Manufacturer string          `json:"manufacturer"`
	Model        string          `json:"model"`
	Verified     bool            `json:"verified"`
	Enabled      bool            `json:"enabled"`
	Online       bool            `json:"online"`
	Updated      *time.Time      `json:"updated,omitempty"`
	Error        string          `json:"error,omitempty"`
	Reading      devices.Reading `json:"reading"`
	RatedW       float64         `json:"rated_w,omitempty"`
	Controllable bool            `json:"controllable"`
	LimitPct     *float64        `json:"limit_pct,omitempty"`
	WriteError   string          `json:"write_error,omitempty"`
}

// Snapshot is the live state of the site, published every control cycle.
type Snapshot struct {
	Time     time.Time        `json:"time"`
	SiteName string           `json:"site_name"`
	Mode     control.Mode     `json:"mode"`
	Decision control.Decision `json:"decision"`
	// HeadroomW is nil when no constraint is active.
	HeadroomW *float64 `json:"headroom_w"`

	// Power flows in W. GridW is positive when importing. Nil when the
	// site has no such source or it is not measured right now.
	GridW   *float64 `json:"grid_w"`
	PVW     float64  `json:"pv_w"`
	GensetW *float64 `json:"genset_w"`
	LoadW   *float64 `json:"load_w"`

	Devices []DeviceStatus `json:"devices"`
}

// Engine runs a site.
type Engine struct {
	lib  *devices.Library
	log  *slog.Logger
	now  func() time.Time
	tick time.Duration

	// cycleMu serializes control cycles with reconfiguration, so clients
	// are never closed under an in-flight write. Taken before mu.
	cycleMu     sync.Mutex
	mu          sync.Mutex
	site        Site
	runners     []*runner
	clients     map[string]*modbus.Client
	stopPollers context.CancelFunc
	pollers     sync.WaitGroup
	state       control.State
	wasOnline   map[string]bool

	snapMu sync.RWMutex
	snap   Snapshot
	subs   map[chan Snapshot]struct{}
	// OnSnapshot, if set, sees every snapshot (statistics, logging).
	OnSnapshot func(Snapshot)
}

type runner struct {
	cfg    DeviceConfig
	bus    Bus
	device *devices.Device

	mu         sync.Mutex
	reading    devices.Reading
	updated    time.Time
	online     bool
	err        string
	limitPct   *float64
	writeError string
}

// NewEngine returns an engine for site. Call Run to start it.
func NewEngine(lib *devices.Library, site Site, log *slog.Logger) (*Engine, error) {
	if err := site.Validate(lib); err != nil {
		return nil, err
	}
	return &Engine{
		lib:       lib,
		log:       log,
		now:       time.Now,
		tick:      time.Second,
		site:      site,
		clients:   map[string]*modbus.Client{},
		wasOnline: map[string]bool{},
		subs:      map[chan Snapshot]struct{}{},
	}, nil
}

// SetCycleInterval changes how often the control loop runs (default 1 s).
// Call it before Run.
func (e *Engine) SetCycleInterval(d time.Duration) { e.tick = d }

// Site returns the running configuration.
func (e *Engine) Site() Site {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.site
}

// Run polls devices and controls the site until ctx ends. On exit it
// leaves inverter limits as they are: a curtailed site stays curtailed.
func (e *Engine) Run(ctx context.Context) {
	e.mu.Lock()
	e.startLocked()
	e.mu.Unlock()
	defer func() {
		e.cycleMu.Lock()
		e.mu.Lock()
		e.stopLocked()
		e.mu.Unlock()
		e.cycleMu.Unlock()
	}()

	ticker := time.NewTicker(e.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.cycle(ctx)
		}
	}
}

// Apply validates and switches to a new configuration without stopping
// the control loop for longer than a restart of the pollers.
func (e *Engine) Apply(site Site) error {
	if err := site.Validate(e.lib); err != nil {
		return err
	}
	e.cycleMu.Lock()
	defer e.cycleMu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	running := e.stopPollers != nil
	if running {
		e.stopLocked()
	}
	e.site = site
	e.wasOnline = map[string]bool{}
	e.state.ForceResend()
	if running {
		e.startLocked()
	}
	return nil
}

func (e *Engine) startLocked() {
	ctx, cancel := context.WithCancel(context.Background())
	e.stopPollers = cancel

	buses := map[string]Bus{}
	for _, b := range e.site.Buses {
		buses[b.ID] = b
	}
	e.runners = nil
	for _, cfg := range e.site.Devices {
		profile, _ := e.lib.Get(cfg.Profile)
		bus := buses[cfg.BusID]
		r := &runner{cfg: cfg, bus: bus}
		if cfg.Enabled {
			r.device = devices.NewDevice(profile, e.clientLocked(bus), byte(cfg.UnitID))
			e.pollers.Add(1)
			go func() {
				defer e.pollers.Done()
				e.poll(ctx, r)
			}()
		}
		e.runners = append(e.runners, r)
	}
}

func (e *Engine) stopLocked() {
	if e.stopPollers != nil {
		e.stopPollers()
		e.pollers.Wait()
		e.stopPollers = nil
	}
	for id, c := range e.clients {
		_ = c.Close()
		delete(e.clients, id)
	}
}

func (e *Engine) clientLocked(b Bus) *modbus.Client {
	if c, ok := e.clients[b.ID]; ok {
		return c
	}
	c := modbus.NewClient(transportFor(b))
	e.clients[b.ID] = c
	return c
}

func transportFor(b Bus) modbus.Transport {
	timeout := time.Duration(b.TimeoutMS) * time.Millisecond
	address := fmt.Sprintf("%s:%d", b.Host, b.Port)
	switch b.Transport {
	case "rtuovertcp":
		return modbus.NewRTUOverTCPTransport(address, timeout)
	case "rtu":
		return modbus.NewSerialRTUTransport(modbus.SerialConfig{
			Device: b.Device, BaudRate: b.BaudRate, DataBits: 8, Parity: b.Parity, StopBits: b.StopBits,
		}, timeout)
	default:
		return modbus.NewTCPTransport(address, timeout)
	}
}

func (e *Engine) poll(ctx context.Context, r *runner) {
	interval := time.Duration(r.cfg.PollIntervalMS) * time.Millisecond
	if interval == 0 {
		interval = time.Second
	}
	for {
		started := time.Now()
		readCtx, cancel := context.WithTimeout(ctx, 2*time.Duration(r.bus.TimeoutMS)*time.Millisecond+interval)
		reading, err := r.device.Read(readCtx)
		cancel()
		if ctx.Err() != nil {
			return
		}

		r.mu.Lock()
		if err != nil {
			if r.online || r.err == "" {
				e.log.Warn("device not responding", "device", r.cfg.ID, "error", err)
			}
			r.online, r.err = false, err.Error()
		} else {
			if !r.online && r.err != "" {
				e.log.Info("device responding again", "device", r.cfg.ID)
			}
			if r.cfg.InvertPower {
				invert(reading)
			}
			r.reading, r.updated, r.online, r.err = reading, e.now(), true, ""
		}
		r.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-time.After(max(0, interval-time.Since(started))):
		}
	}
}

func invert(r devices.Reading) {
	for _, p := range []string{devices.PointPower, devices.PointPowerL1, devices.PointPowerL2, devices.PointPowerL3} {
		if v, ok := r[p]; ok {
			r[p] = -v
		}
	}
	imp, hasImp := r[devices.PointEnergyImport]
	exp, hasExp := r[devices.PointEnergyExport]
	delete(r, devices.PointEnergyImport)
	delete(r, devices.PointEnergyExport)
	if hasImp {
		r[devices.PointEnergyExport] = imp
	}
	if hasExp {
		r[devices.PointEnergyImport] = exp
	}
}

// cycle runs one control step.
func (e *Engine) cycle(ctx context.Context) {
	e.cycleMu.Lock()
	defer e.cycleMu.Unlock()
	e.mu.Lock()
	site := e.site
	runners := e.runners
	now := e.now()

	m := control.Measurements{Now: now}
	type inverterRef struct {
		r      *runner
		ratedW float64
	}
	var inverters []inverterRef
	statuses := make([]DeviceStatus, 0, len(runners))

	for _, r := range runners {
		profile, _ := e.lib.Get(r.cfg.Profile)
		r.mu.Lock()
		reading := r.reading
		power, hasPower := reading[devices.PointPower]
		online := r.online && hasPower
		status := DeviceStatus{
			ID: r.cfg.ID, Name: r.cfg.Name, Role: r.cfg.Role, Profile: r.cfg.Profile,
			Manufacturer: profile.Manufacturer, Model: profile.Model, Verified: profile.Verified,
			Enabled: r.cfg.Enabled, Online: online, Error: r.err, Reading: reading,
			LimitPct: r.limitPct, WriteError: r.writeError,
		}
		if !r.updated.IsZero() {
			updated := r.updated
			status.Updated = &updated
		}
		updated := r.updated
		r.mu.Unlock()
		if status.Reading == nil {
			status.Reading = devices.Reading{}
		}
		if !r.cfg.Enabled {
			statuses = append(statuses, status)
			continue
		}

		switch r.cfg.Role {
		case RoleGridMeter:
			meter := meterFor(r.cfg.ID, online, updated, power, reading)
			m.Grid = &meter
		case RoleGensetMeter:
			status.RatedW = r.cfg.RatedW
			m.Gensets = append(m.Gensets, control.Genset{
				Meter:  meterFor(r.cfg.ID, online, updated, power, reading),
				RatedW: r.cfg.RatedW,
			})
		case RolePVInverter:
			rated := reading[devices.PointRatedPower]
			if rated <= 0 {
				rated = r.cfg.RatedW
			}
			status.RatedW = rated
			status.Controllable = rated > 0 && r.device.HasControl(devices.ControlPowerLimit)
			m.Inverters = append(m.Inverters, control.Inverter{
				ID: r.cfg.ID, Online: online, Updated: updated, PowerW: power, RatedW: rated, Controllable: status.Controllable,
			})
			// An inverter that comes back may have lost its setpoint.
			if online && !e.wasOnline[r.cfg.ID] {
				e.state.ForceResend()
			}
			e.wasOnline[r.cfg.ID] = online
			if online && status.Controllable {
				inverters = append(inverters, inverterRef{r, rated})
			}
		}
		statuses = append(statuses, status)
	}

	decision := control.Step(site.Control, m, &e.state)
	e.mu.Unlock()

	if decision.Send && len(inverters) > 0 {
		var wg sync.WaitGroup
		failed := false
		var failedMu sync.Mutex
		for _, inv := range inverters {
			wg.Add(1)
			go func() {
				defer wg.Done()
				writeCtx, cancel := context.WithTimeout(ctx, 2*time.Duration(inv.r.bus.TimeoutMS)*time.Millisecond)
				defer cancel()
				err := inv.r.device.SetPowerLimit(writeCtx, decision.LimitPct)
				inv.r.mu.Lock()
				if err != nil {
					inv.r.writeError = err.Error()
				} else {
					pct := decision.LimitPct
					inv.r.limitPct, inv.r.writeError = &pct, ""
				}
				inv.r.mu.Unlock()
				if err != nil {
					e.log.Warn("power limit write failed", "device", inv.r.cfg.ID, "limit_pct", decision.LimitPct, "error", err)
					failedMu.Lock()
					failed = true
					failedMu.Unlock()
				}
			}()
		}
		wg.Wait()
		if failed {
			e.mu.Lock()
			e.state.ForceResend()
			e.mu.Unlock()
		}
	}

	e.publish(e.snapshot(now, site, m, decision, statuses))
}

// maxVoltage returns the highest phase voltage, and whether the meter
// measures voltage at all.
func maxVoltage(r devices.Reading) (float64, bool) {
	v, ok := 0.0, false
	for _, p := range []string{devices.PointVoltageL1, devices.PointVoltageL2, devices.PointVoltageL3} {
		if pv, has := r[p]; has {
			v, ok = math.Max(v, pv), true
		}
	}
	return v, ok
}

func meterFor(id string, online bool, updated time.Time, power float64, r devices.Reading) control.Meter {
	v, ok := maxVoltage(r)
	return control.Meter{ID: id, Online: online, Updated: updated, PowerW: power, VoltageV: v, HasVoltage: ok}
}

func (e *Engine) snapshot(now time.Time, site Site, m control.Measurements, d control.Decision, statuses []DeviceStatus) Snapshot {
	s := Snapshot{Time: now, SiteName: site.Name, Mode: site.Control.Mode, Decision: d, PVW: d.PVPowerW, Devices: statuses}
	if !math.IsNaN(d.HeadroomW) {
		h := d.HeadroomW
		s.HeadroomW = &h
	}
	fresh := func(online bool, updated time.Time) bool {
		return online && now.Sub(updated) <= site.Control.MeasurementTimeout.D()
	}

	load := s.PVW
	complete := true
	if m.Grid != nil {
		if fresh(m.Grid.Online, m.Grid.Updated) {
			g := m.Grid.PowerW
			s.GridW = &g
			load += g
		} else {
			complete = false
		}
	}
	if len(m.Gensets) > 0 {
		total := 0.0
		for _, g := range m.Gensets {
			if fresh(g.Online, g.Updated) {
				total += g.PowerW
			} else {
				complete = false
			}
		}
		s.GensetW = &total
		load += total
	}
	for _, inv := range m.Inverters {
		if !fresh(inv.Online, inv.Updated) {
			complete = false
		}
	}
	if complete {
		load = math.Max(0, load)
		s.LoadW = &load
	}
	sort.SliceStable(s.Devices, func(i, j int) bool { return roleOrder(s.Devices[i].Role) < roleOrder(s.Devices[j].Role) })
	return s
}

func roleOrder(r Role) int {
	switch r {
	case RoleGridMeter:
		return 0
	case RoleGensetMeter:
		return 1
	default:
		return 2
	}
}

func (e *Engine) publish(s Snapshot) {
	e.snapMu.Lock()
	e.snap = s
	subs := make([]chan Snapshot, 0, len(e.subs))
	for ch := range e.subs {
		subs = append(subs, ch)
	}
	e.snapMu.Unlock()

	for _, ch := range subs {
		// Keep only the newest snapshot for slow subscribers.
		select {
		case ch <- s:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- s:
			default:
			}
		}
	}
	if e.OnSnapshot != nil {
		e.OnSnapshot(s)
	}
}

// Snapshot returns the latest published snapshot.
func (e *Engine) Snapshot() Snapshot {
	e.snapMu.RLock()
	defer e.snapMu.RUnlock()
	return e.snap
}

// Subscribe returns a channel of snapshots and a function to stop.
func (e *Engine) Subscribe() (<-chan Snapshot, func()) {
	ch := make(chan Snapshot, 1)
	e.snapMu.Lock()
	e.subs[ch] = struct{}{}
	e.snapMu.Unlock()
	return ch, func() {
		e.snapMu.Lock()
		delete(e.subs, ch)
		e.snapMu.Unlock()
	}
}

// withClient runs fn with a client for bus: the running one when the bus
// is in use with identical settings (a serial port cannot be opened
// twice), otherwise a temporary one.
func (e *Engine) withClient(bus Bus, fn func(*modbus.Client) error) error {
	if err := bus.Validate(); err != nil {
		return err
	}
	e.mu.Lock()
	var shared *modbus.Client
	for _, b := range e.site.Buses {
		if b == bus {
			shared = e.clients[b.ID]
		}
	}
	e.mu.Unlock()
	if shared != nil {
		return fn(shared)
	}
	temp := modbus.NewClient(transportFor(bus))
	defer temp.Close()
	return fn(temp)
}

// Probe reads a device once with the given settings, for commissioning.
func (e *Engine) Probe(ctx context.Context, bus Bus, cfg DeviceConfig) (devices.Reading, bool, error) {
	profile, ok := e.lib.Get(cfg.Profile)
	if !ok {
		return nil, false, fmt.Errorf("unknown profile %q", cfg.Profile)
	}
	if cfg.UnitID < 1 || cfg.UnitID > 247 {
		return nil, false, fmt.Errorf("unit ID must be 1 to 247")
	}
	var reading devices.Reading
	controllable := false
	err := e.withClient(bus, func(c *modbus.Client) error {
		d := devices.NewDevice(profile, c, byte(cfg.UnitID))
		var err error
		reading, err = d.Read(ctx)
		controllable = err == nil && d.HasControl(devices.ControlPowerLimit)
		return err
	})
	return reading, controllable, err
}

// ReadRaw reads registers directly, for commissioning.
func (e *Engine) ReadRaw(ctx context.Context, bus Bus, unitID int, table devices.Table, address, quantity uint16) ([]uint16, error) {
	if unitID < 1 || unitID > 247 {
		return nil, fmt.Errorf("unit ID must be 1 to 247")
	}
	var values []uint16
	err := e.withClient(bus, func(c *modbus.Client) error {
		var err error
		if table == devices.TableInput {
			values, err = c.ReadInputRegisters(ctx, byte(unitID), address, quantity)
		} else {
			values, err = c.ReadHoldingRegisters(ctx, byte(unitID), address, quantity)
		}
		return err
	})
	return values, err
}
