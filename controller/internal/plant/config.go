// Package plant runs a site: it polls the configured devices, feeds the
// control loop, writes inverter limits and publishes live snapshots.
package plant

import (
	"errors"
	"fmt"
	"regexp"
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
}

// Site is the full configuration of one installation.
type Site struct {
	Name     string           `json:"name"`
	Timezone string           `json:"timezone"`
	Buses    []Bus            `json:"buses"`
	Devices  []DeviceConfig   `json:"devices"`
	Control  control.Settings `json:"control"`
}

// DefaultSite is an empty site in monitor mode.
func DefaultSite() Site {
	return Site{Name: "My site", Timezone: "UTC", Buses: []Bus{}, Devices: []DeviceConfig{}, Control: control.DefaultSettings()}
}

var configID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// Validate checks the site against the device library.
func (s *Site) Validate(lib *devices.Library) error {
	var errs []error
	if s.Name == "" || len(s.Name) > 80 {
		errs = append(errs, errors.New("site name must be 1 to 80 characters"))
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		errs = append(errs, fmt.Errorf("unknown time zone %q", s.Timezone))
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
		if b.TimeoutMS < 100 || b.TimeoutMS > 10000 {
			errs = append(errs, fmt.Errorf("bus %q: timeout must be 100 to 10000 ms", b.ID))
		}
		switch b.Transport {
		case "tcp", "rtuovertcp":
			if b.Host == "" || b.Port < 1 || b.Port > 65535 {
				errs = append(errs, fmt.Errorf("bus %q: host and port are required", b.ID))
			}
		case "rtu":
			if b.Device == "" || b.BaudRate <= 0 {
				errs = append(errs, fmt.Errorf("bus %q: serial device and baud rate are required", b.ID))
			}
			if b.Parity != "N" && b.Parity != "E" && b.Parity != "O" {
				errs = append(errs, fmt.Errorf("bus %q: parity must be N, E or O", b.ID))
			}
			if b.StopBits != 1 && b.StopBits != 2 {
				errs = append(errs, fmt.Errorf("bus %q: stop bits must be 1 or 2", b.ID))
			}
		default:
			errs = append(errs, fmt.Errorf("bus %q: transport must be tcp, rtuovertcp or rtu", b.ID))
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
		case RoleGridMeter, RoleGensetMeter:
			if profile.Kind != devices.KindMeter {
				errs = append(errs, fmt.Errorf("device %q: role %s needs a meter profile", d.ID, d.Role))
			}
			if d.Role == RoleGridMeter && d.Enabled {
				gridMeters++
			}
			if d.Role == RoleGensetMeter && d.RatedW <= 0 {
				errs = append(errs, fmt.Errorf("device %q: genset rating is required", d.ID))
			}
		case RolePVInverter:
			if profile.Kind != devices.KindInverter {
				errs = append(errs, fmt.Errorf("device %q: role %s needs an inverter profile", d.ID, d.Role))
			}
		default:
			errs = append(errs, fmt.Errorf("device %q: unknown role %q", d.ID, d.Role))
		}
	}
	if gridMeters > 1 {
		errs = append(errs, errors.New("only one grid meter can be enabled"))
	}
	return errors.Join(errs...)
}
