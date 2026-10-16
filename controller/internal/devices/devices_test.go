package devices

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/the-asad-iqbal/async-os/controller/internal/modbus"
)

func TestDecode(t *testing.T) {
	sf := uint16(0)
	cases := []struct {
		name string
		reg  Register
		raw  []uint16
		sf   int16
		want float64
		ok   bool
	}{
		{"uint16", Register{Type: TypeUint16}, []uint16{65535}, 0, 65535, true},
		{"int16", Register{Type: TypeInt16}, []uint16{0xFFFE}, 0, -2, true},
		{"int32 high first", Register{Type: TypeInt32}, []uint16{0xFFFF, 0xFF38}, 0, -200, true},
		{"uint32 low first", Register{Type: TypeUint32, WordOrder: "low_first"}, []uint16{0x0002, 0x0001}, 0, 65538, true},
		{"float32", Register{Type: TypeFloat32}, []uint16{0x4048, 0xF5C3}, 0, 3.14, true},
		{"float32 NaN unavailable", Register{Type: TypeFloat32}, []uint16{0x7FC0, 0x0000}, 0, 0, false},
		{"scale", Register{Type: TypeUint16, Scale: 0.1}, []uint16{2305}, 0, 230.5, true},
		{"sunspec sf", Register{Type: TypeInt16, ScaleFactor: &sf}, []uint16{1234}, -1, 123.4, true},
		{"unavailable marker", Register{Type: TypeInt16, Unavailable: []float64{-32768}}, []uint16{0x8000}, 0, 0, false},
		{"int64", Register{Type: TypeInt64, Scale: 0.001}, []uint16{0, 0, 0x0001, 0x86A0}, 0, 100, true},
		{"bit set", Register{Type: TypeUint16, Bit: ptr8(3)}, []uint16{0b1000}, 0, 1, true},
		{"bit clear", Register{Type: TypeUint16, Bit: ptr8(2)}, []uint16{0b1000}, 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := decode(tc.reg, tc.raw, tc.sf)
			if err != nil {
				t.Fatal(err)
			}
			if ok != tc.ok || (ok && math.Abs(got-tc.want) > 1e-4) {
				t.Fatalf("decode = %v, %v; want %v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	regs := []Register{
		{Type: TypeUint16, Scale: 0.1},
		{Type: TypeInt16},
		{Type: TypeInt32, WordOrder: "low_first"},
		{Type: TypeFloat32},
	}
	for _, r := range regs {
		raw, err := encode(r, 42.5, 0)
		if err != nil {
			t.Fatal(err)
		}
		got, _, _ := decode(r, raw, 0)
		want := 42.5
		if r.Scale == 0 && r.Type != TypeFloat32 {
			want = 43 // integers round half away from zero
		}
		if math.Abs(got-want) > 1e-6 {
			t.Fatalf("%s: round trip = %v, want %v", r.Type, got, want)
		}
	}
	if _, err := encode(Register{Type: TypeUint16}, -1, 0); err == nil {
		t.Fatal("negative uint16 accepted")
	}
}

func TestBuiltinLibraryIsValid(t *testing.T) {
	lib, err := LoadLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	all := lib.All()
	if len(all) < 10 {
		t.Fatalf("only %d profiles loaded", len(all))
	}
	for _, p := range all {
		if p.Kind == KindInverter && p.SunSpec == nil {
			if _, ok := p.Controls[ControlPowerLimit]; !ok {
				t.Errorf("%s: inverter without a power limit control", p.ID)
			}
		}
	}
}

func TestPlanBlocksMergesNearbyRegisters(t *testing.T) {
	points := map[string]Register{
		"a": {Table: TableInput, Address: 0, Type: TypeFloat32},
		"b": {Table: TableInput, Address: 6, Type: TypeFloat32},
		"c": {Table: TableInput, Address: 70, Type: TypeFloat32},
		"d": {Table: TableHolding, Address: 10, Type: TypeUint16},
	}
	blocks := planBlocks(points)
	if len(blocks) != 3 {
		t.Fatalf("got %d blocks: %+v", len(blocks), blocks)
	}
	if blocks[1].table != TableInput || blocks[1].address != 0 || blocks[1].quantity != 8 {
		t.Fatalf("first input block = %+v", blocks[1])
	}
}

func newTestDevice(t *testing.T, bank *modbus.RegisterBank, profile *Profile, unit byte) *Device {
	t.Helper()
	server := modbus.NewServer(bank)
	address, err := server.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	client := modbus.NewClient(modbus.NewTCPTransport(address, time.Second))
	t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	return NewDevice(profile, client, unit)
}

func float32Regs(v float32) []uint16 {
	bits := math.Float32bits(v)
	return []uint16{uint16(bits >> 16), uint16(bits)}
}

func TestReadEastronProfile(t *testing.T) {
	lib, _ := LoadLibrary("")
	profile, _ := lib.Get("eastron-sdm630")
	bank := modbus.NewRegisterBank()
	// Fill every input register the profile touches, then set real values.
	for addr := uint16(0); addr < 80; addr++ {
		bank.SetInput(1, addr, 0)
	}
	bank.SetInput(1, 0, float32Regs(231.5)...)
	bank.SetInput(1, 52, float32Regs(-1500)...)
	bank.SetInput(1, 72, float32Regs(1234.5)...)

	reading, err := newTestDevice(t, bank, profile, 1).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reading[PointVoltageL1] != 231.5 || reading[PointPower] != -1500 || reading[PointEnergyImport] != 1234.5 {
		t.Fatalf("reading = %v", reading)
	}
}

func TestReadFallsBackToPointwise(t *testing.T) {
	profile := &Profile{ID: "gappy", Kind: KindMeter, Points: map[string]Register{
		PointPower:     {Table: TableInput, Address: 0, Type: TypeInt16},
		PointFrequency: {Table: TableInput, Address: 4, Type: TypeUint16, Scale: 0.01},
	}}
	bank := modbus.NewRegisterBank()
	// Registers 1..3 do not exist, so a block read 0..4 fails.
	bank.SetInput(1, 0, 0xFF9C)
	bank.SetInput(1, 4, 5000)
	device := newTestDevice(t, bank, profile, 1)
	for range 2 {
		reading, err := device.Read(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if reading[PointPower] != -100 || reading[PointFrequency] != 50 {
			t.Fatalf("reading = %v", reading)
		}
	}
}

func TestReadUnreachableDevice(t *testing.T) {
	profile := &Profile{ID: "x", Kind: KindMeter, Points: map[string]Register{
		PointPower: {Table: TableInput, Address: 0, Type: TypeInt16},
	}}
	client := modbus.NewClient(modbus.NewTCPTransport("127.0.0.1:1", 200*time.Millisecond))
	defer client.Close()
	if _, err := NewDevice(profile, client, 1).Read(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestSunSpecInverterDiscoveryAndLimit(t *testing.T) {
	bank := modbus.NewRegisterBank()
	inverter := SimulateSunSpecInverter(bank, 1, 40000, 10000)
	inverter.SetPower(6543)

	lib, _ := LoadLibrary("")
	profile, _ := lib.Get("sunspec-inverter")
	device := newTestDevice(t, bank, profile, 1)
	reading, err := device.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reading[PointPower] != 6540 { // W_SF = 1 rounds to tens of watts
		t.Fatalf("power = %v", reading[PointPower])
	}
	if reading[PointRatedPower] != 10000 {
		t.Fatalf("rated power = %v", reading[PointRatedPower])
	}
	if reading[PointEnergyTotal] != 12345.678 {
		t.Fatalf("energy = %v", reading[PointEnergyTotal])
	}
	if !device.HasControl(ControlPowerLimit) {
		t.Fatal("power limit control not discovered")
	}
	if err := device.SetPowerLimit(context.Background(), 42.5); err != nil {
		t.Fatal(err)
	}
	if got := inverter.LimitPercent(); got != 42.5 {
		t.Fatalf("limit = %v, want 42.5", got)
	}
	if !inverter.LimitEnabled() || inverter.RevertSeconds() != 0 {
		t.Fatal("limit not enabled or revert timeout not cleared")
	}
	// Out-of-range setpoints are clamped, never rejected mid-control.
	if err := device.SetPowerLimit(context.Background(), 140); err != nil {
		t.Fatal(err)
	}
	if got := inverter.LimitPercent(); got != 100 {
		t.Fatalf("clamped limit = %v", got)
	}
}

func TestSunSpecMeterDiscovery(t *testing.T) {
	bank := modbus.NewRegisterBank()
	meter := SimulateSunSpecMeter(bank, 240, 40000)
	meter.SetPower(-2500)
	meter.SetEnergy(100, 50)

	lib, _ := LoadLibrary("")
	profile, _ := lib.Get("sunspec-meter")
	reading, err := newTestDevice(t, bank, profile, 240).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reading[PointPower] != -2500 || reading[PointEnergyImport] != 100 || reading[PointEnergyExport] != 50 {
		t.Fatalf("reading = %v", reading)
	}
}

func ptr8(v uint8) *uint8 { return &v }

func TestExtraPointsAreRead(t *testing.T) {
	lib, _ := LoadLibrary("")
	profile, _ := lib.Get("eastron-sdm630")
	bank := modbus.NewRegisterBank()
	for addr := uint16(0); addr < 80; addr++ {
		bank.SetInput(1, addr, 0)
	}
	bank.SetHolding(1, 500, 0b0100) // digital inputs, DI3 on
	device := newTestDevice(t, bank, profile, 1)
	device.AddPoints(map[string]Register{PointRunning: {Table: TableHolding, Address: 500, Type: TypeUint16, Bit: ptr8(2)}})
	reading, err := device.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reading[PointRunning] != 1 {
		t.Fatalf("running = %v", reading[PointRunning])
	}
}
