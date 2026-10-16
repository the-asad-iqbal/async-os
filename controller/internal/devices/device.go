package devices

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/the-asad-iqbal/async-os/controller/internal/modbus"
)

// RegisterClient is the subset of *modbus.Client a device needs.
type RegisterClient interface {
	ReadHoldingRegisters(ctx context.Context, unitID byte, address, quantity uint16) ([]uint16, error)
	ReadInputRegisters(ctx context.Context, unitID byte, address, quantity uint16) ([]uint16, error)
	WriteRegisters(ctx context.Context, unitID byte, address uint16, values []uint16) error
	WriteMultipleRegisters(ctx context.Context, unitID byte, address uint16, values []uint16) error
}

// Reads may span unused registers between points, up to this many, to save
// round trips.
const maxReadGap = 8

// Device is one configured inverter or meter.
type Device struct {
	profile *Profile
	client  RegisterClient
	unitID  byte

	mu       sync.Mutex
	resolved bool
	points   map[string]Register
	controls map[string]Control
	extra    map[string]Register
	blocks   []block
	// pointwise is set once a device rejects block reads, so it is read
	// one value at a time from then on.
	pointwise bool
}

// NewDevice binds a profile to a Modbus client and unit ID.
func NewDevice(profile *Profile, client RegisterClient, unitID byte) *Device {
	return &Device{profile: profile, client: client, unitID: unitID}
}

// AddPoints adds installer-defined points, such as a genset running
// signal on a digital input. Call it before the first Read.
func (d *Device) AddPoints(points map[string]Register) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.extra = points
	d.resolved = false
}

// Profile returns the device's profile.
func (d *Device) Profile() *Profile { return d.profile }

// Reading is one poll of a device.
type Reading map[string]float64

// Read polls every point. Points the device reports as unavailable are left
// out. An error means nothing could be read.
func (d *Device) Read(ctx context.Context) (Reading, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.resolve(ctx); err != nil {
		return nil, err
	}
	values, err := d.readRegisters(ctx)
	if err != nil {
		return nil, err
	}

	reading := Reading{}
	for name, r := range d.points {
		raw, ok := values.get(r.Table, r.Address, r.Type.Registers())
		if !ok {
			continue
		}
		sf, ok := d.scaleFactor(values, r)
		if !ok {
			continue
		}
		v, ok, err := decode(r, raw, sf)
		if err != nil || !ok {
			continue
		}
		reading[name] = v
	}
	if len(reading) == 0 {
		return nil, errors.New("device returned no usable values")
	}
	return reading, nil
}

func (d *Device) scaleFactor(values registerValues, r Register) (int16, bool) {
	if r.ScaleFactor == nil {
		return 0, true
	}
	raw, ok := values.get(r.Table, *r.ScaleFactor, 1)
	if !ok || raw[0] == 0x8000 {
		return 0, false
	}
	sf := int16(raw[0])
	if sf < -10 || sf > 10 {
		return 0, false
	}
	return sf, true
}

// resolve fixes the point set: from the profile, or by SunSpec discovery.
func (d *Device) resolve(ctx context.Context) error {
	if d.resolved {
		return nil
	}
	if d.profile.SunSpec != nil {
		points, controls, err := discoverSunSpec(ctx, d.client, d.unitID, d.profile)
		if err != nil {
			return err
		}
		d.points, d.controls = points, controls
	} else {
		d.points, d.controls = map[string]Register{}, d.profile.Controls
		for name, r := range d.profile.Points {
			d.points[name] = r
		}
	}
	for name, r := range d.extra {
		d.points[name] = r
	}
	d.blocks = planBlocks(d.points)
	d.resolved = true
	return nil
}

// HasControl reports whether the device supports a control. It is only
// meaningful after a successful Read.
func (d *Device) HasControl(name string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.controls[name]
	return ok
}

// SetPowerLimit limits active power to percent of rated power.
func (d *Device) SetPowerLimit(ctx context.Context, percent float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.resolve(ctx); err != nil {
		return err
	}
	c, ok := d.controls[ControlPowerLimit]
	if !ok {
		return fmt.Errorf("%s %s has no power limit control", d.profile.Manufacturer, d.profile.Model)
	}
	if math.IsNaN(percent) {
		return errors.New("power limit is not a number")
	}
	percent = math.Max(c.Min, math.Min(c.Max, percent))

	var sf int16
	if c.ScaleFactor != nil {
		raw, err := d.client.ReadHoldingRegisters(ctx, d.unitID, *c.ScaleFactor, 1)
		if err != nil {
			return fmt.Errorf("read power limit scale factor: %w", err)
		}
		sf = int16(raw[0])
	}
	value, err := encode(c.Register, percent, sf)
	if err != nil {
		return err
	}

	// Order: extra settings, then the setpoint, then the enable flag, so
	// the limit is in place before it takes effect.
	for _, w := range c.Extra {
		if err := d.write(ctx, w.Address, w.Values, c.UseFunc16); err != nil {
			return fmt.Errorf("write register %d: %w", w.Address, err)
		}
	}
	if err := d.write(ctx, c.Address, value, c.UseFunc16); err != nil {
		return fmt.Errorf("write power limit: %w", err)
	}
	if c.Enable != nil {
		if err := d.write(ctx, c.Enable.Address, c.Enable.Values, c.UseFunc16); err != nil {
			return fmt.Errorf("enable power limit: %w", err)
		}
	}
	return nil
}

func (d *Device) write(ctx context.Context, address uint16, values []uint16, func16 bool) error {
	if func16 {
		return d.client.WriteMultipleRegisters(ctx, d.unitID, address, values)
	}
	return d.client.WriteRegisters(ctx, d.unitID, address, values)
}

// block is one contiguous read.
type block struct {
	table    Table
	address  uint16
	quantity uint16
	// spans lists the address ranges that hold wanted values, for
	// point-by-point fallback reads.
	spans []span
}

type span struct {
	address  uint16
	quantity uint16
}

func planBlocks(points map[string]Register) []block {
	byTable := map[Table][]span{}
	add := func(t Table, address uint16, quantity int) {
		byTable[t] = append(byTable[t], span{address, uint16(quantity)})
	}
	for _, r := range points {
		add(r.Table, r.Address, r.Type.Registers())
		if r.ScaleFactor != nil {
			add(r.Table, *r.ScaleFactor, 1)
		}
	}

	var blocks []block
	for _, table := range []Table{TableHolding, TableInput} {
		spans := byTable[table]
		sort.Slice(spans, func(i, j int) bool { return spans[i].address < spans[j].address })
		var current *block
		for _, s := range spans {
			end := int(s.address) + int(s.quantity)
			if current != nil {
				currentEnd := int(current.address) + int(current.quantity)
				if int(s.address) <= currentEnd+maxReadGap && end-int(current.address) <= modbus.MaxReadQuantity {
					if end > currentEnd {
						current.quantity = uint16(end - int(current.address))
					}
					current.spans = append(current.spans, s)
					continue
				}
			}
			blocks = append(blocks, block{table: table, address: s.address, quantity: s.quantity, spans: []span{s}})
			current = &blocks[len(blocks)-1]
		}
	}
	return blocks
}

// registerValues holds the registers read in one poll.
type registerValues map[Table]map[uint16]uint16

func (v registerValues) put(t Table, address uint16, values []uint16) {
	if v[t] == nil {
		v[t] = map[uint16]uint16{}
	}
	for i, value := range values {
		v[t][address+uint16(i)] = value
	}
}

func (v registerValues) get(t Table, address uint16, quantity int) ([]uint16, bool) {
	out := make([]uint16, quantity)
	for i := range out {
		value, ok := v[t][address+uint16(i)]
		if !ok {
			return nil, false
		}
		out[i] = value
	}
	return out, true
}

func (d *Device) readRegisters(ctx context.Context) (registerValues, error) {
	values := registerValues{}
	var firstErr error
	read := func(t Table, address, quantity uint16) error {
		var raw []uint16
		var err error
		if t == TableInput {
			raw, err = d.client.ReadInputRegisters(ctx, d.unitID, address, quantity)
		} else {
			raw, err = d.client.ReadHoldingRegisters(ctx, d.unitID, address, quantity)
		}
		if err != nil {
			return err
		}
		values.put(t, address, raw)
		return nil
	}

	for _, b := range d.blocks {
		if !d.pointwise || len(b.spans) == 1 {
			err := read(b.table, b.address, b.quantity)
			if err == nil {
				continue
			}
			var ex *modbus.ExceptionError
			if !errors.As(err, &ex) {
				// The device does not answer: stop instead of waiting
				// for a timeout on every remaining block.
				return values, nonEmpty(values, err)
			}
			if ex.Code != 2 || len(b.spans) == 1 {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			// The block crossed unmapped registers. Read value by value,
			// now and on every later poll.
			d.pointwise = true
		}
		for _, s := range b.spans {
			err := read(b.table, s.address, s.quantity)
			if err == nil {
				continue
			}
			var ex *modbus.ExceptionError
			if !errors.As(err, &ex) {
				return values, nonEmpty(values, err)
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return values, nonEmpty(values, firstErr)
}

func nonEmpty(values registerValues, err error) error {
	if len(values) == 0 {
		if err == nil {
			err = errors.New("no registers read")
		}
		return err
	}
	return nil
}
