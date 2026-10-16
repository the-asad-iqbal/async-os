// Package devices describes inverters and energy meters (profiles), and
// reads and controls them over Modbus.
package devices

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Kind of device a profile describes.
type Kind string

// Device kinds.
const (
	KindMeter    Kind = "meter"
	KindInverter Kind = "inverter"
)

// Canonical point names. Every profile maps its registers onto these, so the
// rest of the controller never deals with vendor specifics.
const (
	PointPower        = "power"         // W, see Profile docs for the sign
	PointPowerL1      = "power_l1"      // W
	PointPowerL2      = "power_l2"      // W
	PointPowerL3      = "power_l3"      // W
	PointVoltageL1    = "voltage_l1"    // V, phase to neutral
	PointVoltageL2    = "voltage_l2"    // V
	PointVoltageL3    = "voltage_l3"    // V
	PointCurrentL1    = "current_l1"    // A
	PointCurrentL2    = "current_l2"    // A
	PointCurrentL3    = "current_l3"    // A
	PointFrequency    = "frequency"     // Hz
	PointPowerFactor  = "power_factor"  // -1..1
	PointEnergyImport = "energy_import" // kWh, meters
	PointEnergyExport = "energy_export" // kWh, meters
	PointEnergyTotal  = "energy_total"  // kWh, inverter lifetime yield
	PointRatedPower   = "rated_power"   // W, inverter nameplate
	PointDCPower      = "dc_power"      // W
	PointPowerLimit   = "power_limit"   // %, active limit read back
)

// ControlPowerLimit sets active power as a percentage of rated power.
const ControlPowerLimit = "power_limit"

var knownPoints = map[string]string{
	PointPower: "W", PointPowerL1: "W", PointPowerL2: "W", PointPowerL3: "W",
	PointVoltageL1: "V", PointVoltageL2: "V", PointVoltageL3: "V",
	PointCurrentL1: "A", PointCurrentL2: "A", PointCurrentL3: "A",
	PointFrequency: "Hz", PointPowerFactor: "", PointEnergyImport: "kWh",
	PointEnergyExport: "kWh", PointEnergyTotal: "kWh", PointRatedPower: "W",
	PointDCPower: "W", PointPowerLimit: "%",
}

// Table selects the Modbus register table.
type Table string

// Register tables.
const (
	TableHolding Table = "holding"
	TableInput   Table = "input"
)

// DataType is how one value is laid out in registers.
type DataType string

// Supported data types.
const (
	TypeUint16  DataType = "uint16"
	TypeInt16   DataType = "int16"
	TypeUint32  DataType = "uint32"
	TypeInt32   DataType = "int32"
	TypeFloat32 DataType = "float32"
	TypeUint64  DataType = "uint64"
	TypeInt64   DataType = "int64"
)

// Registers returns how many 16-bit registers the type occupies.
func (t DataType) Registers() int {
	switch t {
	case TypeUint16, TypeInt16:
		return 1
	case TypeUint32, TypeInt32, TypeFloat32:
		return 2
	case TypeUint64, TypeInt64:
		return 4
	default:
		return 0
	}
}

// Register locates and decodes one value.
//
// Address is the zero-based protocol address, as sent on the wire. Manuals
// often print one-based "Modicon" numbers instead: input register 30053
// is Address 52 in the input table, holding register 40001 is Address 0.
type Register struct {
	Table   Table    `json:"table"`
	Address uint16   `json:"address"`
	Type    DataType `json:"type"`
	// WordOrder of multi-register values: "high_first" (default, Modbus
	// convention) or "low_first".
	WordOrder string `json:"word_order,omitempty"`
	// Scale multiplies the raw value (default 1).
	Scale float64 `json:"scale,omitempty"`
	// ScaleFactor, if set, is an int16 register holding a power of ten
	// exponent applied on top of Scale (SunSpec style).
	ScaleFactor *uint16 `json:"scale_factor,omitempty"`
	// Unavailable marks raw values that mean "not implemented", e.g.
	// 0x8000 for SunSpec int16.
	Unavailable []float64 `json:"unavailable,omitempty"`
}

// Control is a writable setpoint.
type Control struct {
	Register
	// Min and Max bound the value in engineering units after scaling.
	Min float64 `json:"min"`
	Max float64 `json:"max"`
	// Enable, if set, is written before the setpoint (e.g. a "limit
	// enabled" flag).
	Enable *Write `json:"enable,omitempty"`
	// Extra writes made with every setpoint, e.g. a revert timeout.
	Extra []Write `json:"extra,omitempty"`
	// UseFunc16 forces "write multiple registers" even for one register.
	UseFunc16 bool `json:"use_func16,omitempty"`
}

// Write is a raw register write.
type Write struct {
	Table   Table    `json:"table,omitempty"`
	Address uint16   `json:"address"`
	Values  []uint16 `json:"values"`
}

// Connection holds defaults for how a device is reached.
type Connection struct {
	Transport string `json:"transport,omitempty"` // "tcp", "rtu" or "rtuovertcp"
	Port      int    `json:"port,omitempty"`
	UnitID    int    `json:"unit_id,omitempty"`
	BaudRate  int    `json:"baud_rate,omitempty"`
	Parity    string `json:"parity,omitempty"`
	StopBits  int    `json:"stop_bits,omitempty"`
}

// SunSpec enables runtime discovery of SunSpec models instead of fixed
// registers.
type SunSpec struct {
	// BaseAddresses to probe for the "SunS" marker, in order.
	BaseAddresses []uint16 `json:"base_addresses"`
	// Control enables the model 123 power limit.
	Control bool `json:"control"`
}

// Profile describes one device model.
//
// Sign conventions: for meters, power is positive when energy flows in the
// meter's import direction (installers can invert it per device). For
// inverters, power is AC output and positive when producing.
type Profile struct {
	ID           string              `json:"id"`
	Manufacturer string              `json:"manufacturer"`
	Model        string              `json:"model"`
	Kind         Kind                `json:"kind"`
	Connection   Connection          `json:"connection"`
	Points       map[string]Register `json:"points,omitempty"`
	Controls     map[string]Control  `json:"controls,omitempty"`
	SunSpec      *SunSpec            `json:"sunspec,omitempty"`
	// Verified is true only once the map was checked against real hardware.
	Verified bool `json:"verified"`
	// Source names the document the map was taken from.
	Source string `json:"source"`
	Notes  string `json:"notes,omitempty"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Validate checks a profile for mistakes that would only surface on site.
func (p *Profile) Validate() error {
	var errs []error
	if !idPattern.MatchString(p.ID) {
		errs = append(errs, fmt.Errorf("id %q must be lowercase words joined by dashes", p.ID))
	}
	if p.Manufacturer == "" || p.Model == "" {
		errs = append(errs, errors.New("manufacturer and model are required"))
	}
	if p.Kind != KindMeter && p.Kind != KindInverter {
		errs = append(errs, fmt.Errorf("kind %q must be meter or inverter", p.Kind))
	}
	switch p.Connection.Transport {
	case "tcp", "rtu", "rtuovertcp":
	default:
		errs = append(errs, fmt.Errorf("connection transport %q must be tcp, rtu or rtuovertcp", p.Connection.Transport))
	}
	if p.Source == "" {
		errs = append(errs, errors.New("source is required"))
	}
	if p.SunSpec == nil {
		if _, ok := p.Points[PointPower]; !ok {
			errs = append(errs, errors.New(`point "power" is required`))
		}
	} else if len(p.SunSpec.BaseAddresses) == 0 {
		errs = append(errs, errors.New("sunspec.base_addresses is required"))
	}
	for name, r := range p.Points {
		if _, ok := knownPoints[name]; !ok {
			errs = append(errs, fmt.Errorf("point %q is not a known point", name))
		}
		if err := r.validate(); err != nil {
			errs = append(errs, fmt.Errorf("point %q: %w", name, err))
		}
	}
	for name, c := range p.Controls {
		if name != ControlPowerLimit {
			errs = append(errs, fmt.Errorf("control %q is not a known control", name))
		}
		if p.Kind != KindInverter {
			errs = append(errs, fmt.Errorf("control %q is only valid for inverters", name))
		}
		if err := c.Register.validate(); err != nil {
			errs = append(errs, fmt.Errorf("control %q: %w", name, err))
		}
		if c.Table != TableHolding {
			errs = append(errs, fmt.Errorf("control %q must be a holding register", name))
		}
		if c.Max <= c.Min {
			errs = append(errs, fmt.Errorf("control %q: max must be greater than min", name))
		}
	}
	return errors.Join(errs...)
}

func (r Register) validate() error {
	if r.Table != TableHolding && r.Table != TableInput {
		return fmt.Errorf("table %q must be holding or input", r.Table)
	}
	if r.Type.Registers() == 0 {
		return fmt.Errorf("unknown type %q", r.Type)
	}
	if r.WordOrder != "" && r.WordOrder != "high_first" && r.WordOrder != "low_first" {
		return fmt.Errorf("word_order %q must be high_first or low_first", r.WordOrder)
	}
	if int(r.Address)+r.Type.Registers() > 0x10000 {
		return errors.New("address range overflows")
	}
	return nil
}

//go:embed library/*.json
var builtin embed.FS

// Library holds every known profile.
type Library struct {
	profiles map[string]*Profile
}

// LoadLibrary loads the built-in profiles, then the JSON files in
// overrideDir (if it exists), which may replace built-in ones by ID.
func LoadLibrary(overrideDir string) (*Library, error) {
	lib := &Library{profiles: map[string]*Profile{}}
	if err := lib.loadFS(builtin, "library", "built-in"); err != nil {
		return nil, err
	}
	if overrideDir != "" {
		if _, err := os.Stat(overrideDir); err == nil {
			if err := lib.loadFS(os.DirFS(overrideDir), ".", overrideDir); err != nil {
				return nil, err
			}
		}
	}
	return lib, nil
}

func (l *Library) loadFS(fsys fs.FS, dir, origin string) error {
	paths, err := fs.Glob(fsys, filepath.ToSlash(filepath.Join(dir, "*.json")))
	if err != nil {
		return err
	}
	var errs []error
	for _, path := range paths {
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var p Profile
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&p); err != nil {
			errs = append(errs, fmt.Errorf("%s/%s: %w", origin, path, err))
			continue
		}
		if err := p.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("%s/%s: %w", origin, path, err))
			continue
		}
		if want := strings.TrimSuffix(filepath.Base(path), ".json"); want != p.ID {
			errs = append(errs, fmt.Errorf("%s/%s: id %q does not match the file name", origin, path, p.ID))
			continue
		}
		l.profiles[p.ID] = &p
	}
	return errors.Join(errs...)
}

// Get returns a profile by ID.
func (l *Library) Get(id string) (*Profile, bool) {
	p, ok := l.profiles[id]
	return p, ok
}

// All returns every profile, sorted by manufacturer and model.
func (l *Library) All() []*Profile {
	all := make([]*Profile, 0, len(l.profiles))
	for _, p := range l.profiles {
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Manufacturer != all[j].Manufacturer {
			return all[i].Manufacturer < all[j].Manufacturer
		}
		return all[i].Model < all[j].Model
	})
	return all
}
