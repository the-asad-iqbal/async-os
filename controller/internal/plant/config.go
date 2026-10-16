// Package plant runs a site: it polls the configured devices, feeds the
// control loop, writes inverter limits and publishes live snapshots.
package plant

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/the-asad-iqbal/async-os/controller/internal/control"
	"github.com/the-asad-iqbal/async-os/controller/internal/devices"
)

// Role is what a device does on the site.
type Role string

// Device roles.
const (
	RoleGridMeter   Role = "grid_meter"
	RoleGensetMeter Role = "genset_meter"
	// RoleSourceMeter is one meter fed by the grid or, through an ATS, a
	// genset. Its running signal says which; its energy is split into
	// grid and genset totals.
	RoleSourceMeter Role = "source_meter"
	RolePVInverter  Role = "pv_inverter"
)

// Bus is a communication channel shared by one or more devices.
type Bus struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Transport is "tcp", "rtuovertcp" or "rtu".
	Transport string `json:"transport"`
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
	Device    string `json:"device,omitempty"` // serial device, e.g. /dev/ttyUSB0
	BaudRate  int    `json:"baud_rate,omitempty"`
	Parity    string `json:"parity,omitempty"`
	StopBits  int    `json:"stop_bits,omitempty"`
	TimeoutMS int    `json:"timeout_ms"`
}

// DeviceConfig places one device on the site.
type DeviceConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Profile string `json:"profile"`
	Role    Role   `json:"role"`
	BusID   string `json:"bus_id"`
	UnitID  int    `json:"unit_id"`
	Enabled bool   `json:"enabled"`
	// InvertPower flips the meter's power sign, for meters wired or
	// configured the other way round.
	InvertPower bool `json:"invert_power,omitempty"`
	// RatedW is required for gensets, and for inverters whose profile
	// cannot read the nameplate rating.
	RatedW float64 `json:"rated_w,omitempty"`
	// PollIntervalMS defaults to 1000.
	PollIntervalMS int `json:"poll_interval_ms,omitempty"`

	// Genset only. MinLoadW, when set, is the minimum load in watts and
	// replaces the site-wide percentage.
	MinLoadW float64 `json:"min_load_w,omitempty"`
	// RunningSignal reads the genset's running state from a register of
	// this device, e.g. a digital input bit wired to the genset or ATS.
	RunningSignal *devices.Register `json:"running_signal,omitempty"`
}

// Site is the full configuration of one installation.
type Site struct {
	Name     string           `json:"name"`
	Timezone string           `json:"timezone"`
	Buses    []Bus            `json:"buses"`
	Devices  []DeviceConfig   `json:"devices"`
	Control  control.Settings `json:"control"`
	// OperatorExportLimitMaxW bounds the export limit operators may set.
	OperatorExportLimitMaxW float64 `json:"operator_export_limit_max_w"`
}

// DefaultSite is an empty site in monitor mode.
func DefaultSite() Site {
	return Site{Name: "My site", Timezone: "UTC", Buses: []Bus{}, Devices: []DeviceConfig{}, Control: control.DefaultSettings()}
}

var (
	configID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	// Only real serial ports may be opened as a bus.
	serialDevice = regexp.MustCompile(`^/dev/(tty[A-Za-z0-9]+|serial/by-(id|path)/[A-Za-z0-9._:+-]+)$`)
)

// Validate checks one bus's connection settings.
func (b Bus) Validate() error {
	if b.TimeoutMS < 100 || b.TimeoutMS > 10000 {
		return errors.New("timeout must be 100 to 10000 ms")
	}
	switch b.Transport {
	case "tcp", "rtuovertcp":
		if b.Host == "" || len(b.Host) > 253 || strings.ContainsAny(b.Host, "/ ") || b.Port < 1 || b.Port > 65535 {
			return errors.New("a valid host and port are required")
		}
	case "rtu":
		if !serialDevice.MatchString(b.Device) {
			return fmt.Errorf("serial device %q must be /dev/tty* or /dev/serial/by-id/*", b.Device)
		}
		if b.BaudRate < 300 || b.BaudRate > 921600 {
			return errors.New("baud rate must be 300 to 921600")
		}
		if b.Parity != "N" && b.Parity != "E" && b.Parity != "O" {
			return errors.New("parity must be N, E or O")
		}
		if b.StopBits != 1 && b.StopBits != 2 {
			return errors.New("stop bits must be 1 or 2")
		}
	default:
		return errors.New("transport must be tcp, rtuovertcp or rtu")
	}
	return nil
}

// Validate checks the site against the device library.
func (s *Site) Validate(lib *devices.Library) error {
	var errs []error
	if s.Name == "" || len(s.Name) > 80 {
		errs = append(errs, errors.New("site name must be 1 to 80 characters"))
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		errs = append(errs, fmt.Errorf("unknown time zone %q", s.Timezone))
	}
	if s.OperatorExportLimitMaxW < 0 {
		errs = append(errs, errors.New("operator export limit bound cannot be negative"))
	}
	if err := s.Control.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("control: %w", err))
	}

	buses := map[string]Bus{}
	for _, b := range s.Buses {
		if !configID.MatchString(b.ID) {
			errs = append(errs, fmt.Errorf("bus id %q must be lowercase letters, digits, - or _", b.ID))
		}
		if _, dup := buses[b.ID]; dup {
			errs = append(errs, fmt.Errorf("bus id %q is used twice", b.ID))
		}
		buses[b.ID] = b
		if err := b.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("bus %q: %w", b.ID, err))
		}
	}

	ids := map[string]bool{}
	gridMeters := 0
	for _, d := range s.Devices {
		if !configID.MatchString(d.ID) {
			errs = append(errs, fmt.Errorf("device id %q must be lowercase letters, digits, - or _", d.ID))
		}
		if ids[d.ID] {
			errs = append(errs, fmt.Errorf("device id %q is used twice", d.ID))
		}
		ids[d.ID] = true
		if d.Name == "" {
			errs = append(errs, fmt.Errorf("device %q: name is required", d.ID))
		}
		profile, ok := lib.Get(d.Profile)
		if !ok {
			errs = append(errs, fmt.Errorf("device %q: unknown profile %q", d.ID, d.Profile))
			continue
		}
		if _, ok := buses[d.BusID]; !ok {
			errs = append(errs, fmt.Errorf("device %q: unknown bus %q", d.ID, d.BusID))
		}
		if d.UnitID < 1 || d.UnitID > 247 {
			errs = append(errs, fmt.Errorf("device %q: unit ID must be 1 to 247", d.ID))
		}
		if d.PollIntervalMS != 0 && (d.PollIntervalMS < 200 || d.PollIntervalMS > 60000) {
			errs = append(errs, fmt.Errorf("device %q: poll interval must be 200 to 60000 ms", d.ID))
		}
		if d.RatedW < 0 {
			errs = append(errs, fmt.Errorf("device %q: rating cannot be negative", d.ID))
		}
		switch d.Role {
		case RoleGridMeter, RoleGensetMeter, RoleSourceMeter:
			if profile.Kind != devices.KindMeter {
				errs = append(errs, fmt.Errorf("device %q: role %s needs a meter profile", d.ID, d.Role))
			}
			if (d.Role == RoleGridMeter || d.Role == RoleSourceMeter) && d.Enabled {
				gridMeters++
			}
			isGenset := d.Role == RoleGensetMeter || d.Role == RoleSourceMeter
			if isGenset && d.RatedW <= 0 {
				errs = append(errs, fmt.Errorf("device %q: genset rating is required", d.ID))
			}
			if d.Role == RoleSourceMeter && d.RunningSignal == nil {
				errs = append(errs, fmt.Errorf("device %q: a grid/genset meter needs the genset running signal to tell its sources apart", d.ID))
			}
			if isGenset && (d.MinLoadW < 0 || (d.RatedW > 0 && d.MinLoadW >= d.RatedW)) {
				errs = append(errs, fmt.Errorf("device %q: minimum load must be below the genset rating", d.ID))
			}
		case RolePVInverter:
			if d.MinLoadW != 0 || d.RunningSignal != nil {
				errs = append(errs, fmt.Errorf("device %q: minimum load and running signal apply to gensets only", d.ID))
			}
			if profile.Kind != devices.KindInverter {
				errs = append(errs, fmt.Errorf("device %q: role %s needs an inverter profile", d.ID, d.Role))
			}
		default:
			errs = append(errs, fmt.Errorf("device %q: unknown role %q", d.ID, d.Role))
		}
	}
	for _, d := range s.Devices {
		if d.RunningSignal != nil {
			check := devices.Profile{ID: "check", Manufacturer: "-", Model: "-", Kind: devices.KindMeter, Source: "-",
				Connection: devices.Connection{Transport: "tcp"},
				Points:     map[string]devices.Register{devices.PointPower: *d.RunningSignal}}
			if err := check.Validate(); err != nil {
				errs = append(errs, fmt.Errorf("device %q: running signal: %w", d.ID, err))
			}
			if d.Role == RoleGridMeter {
				errs = append(errs, fmt.Errorf("device %q: running signal applies to gensets only", d.ID))
			}
		}
	}
	if gridMeters > 1 {
		errs = append(errs, errors.New("only one grid meter or grid/genset meter can be enabled"))
	}
	return errors.Join(errs...)
}
