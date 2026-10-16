// Package sim runs a virtual site on a local Modbus TCP server: a grid
// meter, PV inverters and a genset meter, with simple physics. It backs the
// demo mode and end-to-end tests.
package sim

import (
	"context"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/the-asad-iqbal/async-os/controller/internal/control"
	"github.com/the-asad-iqbal/async-os/controller/internal/devices"
	"github.com/the-asad-iqbal/async-os/controller/internal/modbus"
	"github.com/the-asad-iqbal/async-os/controller/internal/plant"
)

// Unit IDs of the simulated devices.
const (
	UnitInverter1   = 1
	UnitInverter2   = 2
	UnitGensetMeter = 50
	UnitGridMeter   = 240
)

// Scenario selects what the simulated site does.
type Scenario string

// Scenarios.
const (
	// ScenarioGrid is a grid-tied site with zero export.
	ScenarioGrid Scenario = "grid"
	// ScenarioHybrid alternates four minutes on grid with two minutes of
	// grid outage, when a genset carries the site.
	ScenarioHybrid Scenario = "hybrid"
	// ScenarioATS has one meter behind an ATS that sees the grid or the
	// genset, with a digital input for "genset running". Like the meters
	// on real sites, its kWh counters freeze while the genset runs.
	ScenarioATS Scenario = "ats"
)

// Unit and register of the ATS meter's running signal (digital input 1).
const (
	UnitATSMeter       = 10
	ATSRunningRegister = 500
)

// Site is a running simulation.
type Site struct {
	Address  string
	scenario Scenario
	server   *modbus.Server
	bank     *modbus.RegisterBank

	inverters []*simInverter
	grid      *devices.SunSpecMeterSim
	genset    *sdm630
	ats       *sdm630
	atsKWh    [2]float64 // import, export
	gensetW   float64

	// GridOn and GridOff set the hybrid scenario's grid cycle (default
	// four minutes on, two minutes off). Change them with SetGridCycle.
	GridOn, GridOff time.Duration

	mu      sync.Mutex
	rng     *rand.Rand
	started time.Time
	load    float64
	pv      float64
	gridW   float64
	gridKWh [2]float64 // import, export
}

type simInverter struct {
	*devices.SunSpecInverterSim
	share  float64 // fraction of site irradiance
	output float64
}

// GensetRatedW is the simulated genset rating.
const GensetRatedW = 60000

// Start serves a simulated site on address ("127.0.0.1:0" for any port).
func Start(address string, scenario Scenario) (*Site, error) {
	bank := modbus.NewRegisterBank()
	s := &Site{
		scenario: scenario,
		bank:     bank,
		rng:      rand.New(rand.NewPCG(1, 2)),
		started:  time.Now(),
		load:     30000,
		GridOn:   4 * time.Minute,
		GridOff:  2 * time.Minute,
	}
	s.inverters = []*simInverter{
		{SunSpecInverterSim: devices.SimulateSunSpecInverter(bank, UnitInverter1, 40000, 30000), share: 0.6},
		{SunSpecInverterSim: devices.SimulateSunSpecInverter(bank, UnitInverter2, 40000, 20000), share: 0.4},
	}
	s.grid = devices.SimulateSunSpecMeter(bank, UnitGridMeter, 40000)
	s.genset = newSDM630(bank, UnitGensetMeter)
	s.ats = newSDM630(bank, UnitATSMeter)
	bank.SetHolding(UnitATSMeter, ATSRunningRegister, 0)

	s.server = modbus.NewServer(bank)
	bound, err := s.server.Listen(address)
	if err != nil {
		return nil, err
	}
	s.Address = bound
	return s, nil
}

// Run advances the physics until ctx ends.
func (s *Site) Run(ctx context.Context) {
	const step = 250 * time.Millisecond
	ticker := time.NewTicker(step)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Step(step)
		}
	}
}

// Close stops the Modbus server.
func (s *Site) Close() error { return s.server.Close() }

// SetGridCycle changes the hybrid scenario's grid on and off periods.
func (s *Site) SetGridCycle(on, off time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.GridOn, s.GridOff = on, off
}

// GridAvailable reports whether the simulated grid is up right now.
func (s *Site) GridAvailable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gridAvailableLocked()
}

func (s *Site) gridAvailableLocked() bool {
	if s.scenario != ScenarioHybrid && s.scenario != ScenarioATS {
		return true
	}
	cycle := s.GridOn + s.GridOff
	return time.Since(s.started)%cycle < s.GridOn
}

// Step advances the simulation by dt.
func (s *Site) Step(dt time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := time.Since(s.started).Seconds()
	sec := dt.Seconds()

	// Load: a slow swing, random walk and occasional big machines.
	s.load += (s.rng.Float64() - 0.5) * 600 * sec
	target := 32000 + 12000*math.Sin(t/90)
	s.load += (target - s.load) * 0.05 * sec
	if s.rng.Float64() < 0.004*sec {
		s.load += (s.rng.Float64() - 0.4) * 15000
	}
	s.load = math.Max(5000, math.Min(70000, s.load))

	// Sun: mostly bright with passing clouds.
	cloud := 0.25 * math.Max(0, math.Sin(t/37)) * math.Max(0, math.Sin(t/13))
	irradiance := math.Max(0.05, 0.85+0.1*math.Sin(t/120)-cloud)

	gridUp := s.gridAvailableLocked()
	s.pv = 0
	for _, inv := range s.inverters {
		available := irradiance * inv.share * 50000
		limit := inv.RatedWatt()
		if inv.LimitEnabled() {
			limit = inv.LimitPercent() / 100 * inv.RatedWatt()
		}
		inv.output += (math.Min(available, limit) - inv.output) * math.Min(1, 2.5*sec)
		inv.SetPower(inv.output)
		inv.AddEnergy(inv.output * sec / 3600)
		s.pv += inv.output
	}

	if gridUp {
		s.gensetW = 0
		s.gridW = s.load - s.pv
		if s.gridW >= 0 {
			s.gridKWh[0] += s.gridW * sec / 3600 / 1000
		} else {
			s.gridKWh[1] += -s.gridW * sec / 3600 / 1000
		}
		s.grid.SetPower(s.gridW)
		s.grid.SetEnergy(s.gridKWh[0], s.gridKWh[1])
		s.genset.set(0, 0)
	} else {
		// The genset covers whatever PV does not, and is pushed into
		// reverse power when PV exceeds the load.
		s.gridW = 0
		s.gensetW = s.load - s.pv
		s.grid.SetPower(0)
		s.genset.set(s.gensetW, 230)
		s.genset.addEnergy(math.Max(0, s.gensetW) * sec / 3600 / 1000)
	}
	s.grid.SetVoltage(map[bool]float64{true: 230, false: 0}[gridUp])

	// The ATS meter sees whichever source feeds the site.
	atsW := s.load - s.pv
	s.ats.set(atsW, 230)
	running := uint16(0)
	if !gridUp {
		running = 1
	}
	s.bank.SetHolding(UnitATSMeter, ATSRunningRegister, running)
	if gridUp { // counters freeze on genset
		if atsW >= 0 {
			s.atsKWh[0] += atsW * sec / 3600 / 1000
		} else {
			s.atsKWh[1] += -atsW * sec / 3600 / 1000
		}
		s.ats.put(72, s.atsKWh[0])
		s.ats.put(74, s.atsKWh[1])
	}
}

// Config returns a site configuration that points at the simulation.
func (s *Site) Config() plant.Site {
	host, port := splitHostPort(s.Address)
	site := plant.DefaultSite()
	site.Name = "Demo site"
	site.Control.Mode = control.ModeControl
	site.Buses = []plant.Bus{{ID: "sim", Name: "Simulator (Modbus TCP)", Transport: "tcp", Host: host, Port: port, TimeoutMS: 500}}
	site.Devices = []plant.DeviceConfig{
		{ID: "grid", Name: "Grid meter", Profile: "sunspec-meter", Role: plant.RoleGridMeter, BusID: "sim", UnitID: UnitGridMeter, Enabled: true},
		{ID: "inv-1", Name: "Inverter 1 (30 kW)", Profile: "sunspec-inverter", Role: plant.RolePVInverter, BusID: "sim", UnitID: UnitInverter1, Enabled: true},
		{ID: "inv-2", Name: "Inverter 2 (20 kW)", Profile: "sunspec-inverter", Role: plant.RolePVInverter, BusID: "sim", UnitID: UnitInverter2, Enabled: true},
	}
	if s.scenario == ScenarioATS {
		bit := uint8(0)
		site.Devices[0] = plant.DeviceConfig{
			ID: "meter1", Name: "Main meter (grid / genset)", Profile: "eastron-sdm630", Role: plant.RoleSourceMeter,
			BusID: "sim", UnitID: UnitATSMeter, Enabled: true, RatedW: 100000, MinLoadW: 25000,
			RunningSignal: &devices.Register{Table: devices.TableHolding, Address: ATSRunningRegister, Type: devices.TypeUint16, Bit: &bit},
		}
	}
	if s.scenario == ScenarioHybrid {
		site.Devices = append(site.Devices, plant.DeviceConfig{
			ID: "dg-1", Name: "Genset (60 kW)", Profile: "eastron-sdm630", Role: plant.RoleGensetMeter,
			BusID: "sim", UnitID: UnitGensetMeter, Enabled: true, RatedW: GensetRatedW,
		})
	}
	return site
}
