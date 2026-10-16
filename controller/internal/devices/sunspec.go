package devices

import (
	"context"
	"errors"
	"fmt"
)

// SunSpec model IDs used by the controller.
const (
	sunspecMarkerHigh = 0x5375 // "Su"
	sunspecMarkerLow  = 0x6e53 // "nS"
	sunspecEnd        = 0xFFFF
)

type sunspecModel struct {
	id     uint16
	start  uint16 // first data register, after ID and length
	length uint16
}

// discoverSunSpec walks the SunSpec model chain and maps the models the
// controller understands onto canonical points and controls.
func discoverSunSpec(ctx context.Context, client RegisterClient, unitID byte, profile *Profile) (map[string]Register, map[string]Control, error) {
	models, err := sunspecModels(ctx, client, unitID, profile.SunSpec.BaseAddresses)
	if err != nil {
		return nil, nil, err
	}
	points := map[string]Register{}
	controls := map[string]Control{}

	switch profile.Kind {
	case KindInverter:
		if m, ok := firstModel(models, 103, 102, 101); ok {
			addInverterIntModel(points, m)
		} else if m, ok := firstModel(models, 113, 112, 111); ok {
			addInverterFloatModel(points, m)
		} else {
			return nil, nil, errors.New("sunspec: no inverter model (101-103, 111-113) found")
		}
		if m, ok := firstModel(models, 120); ok && m.length >= 3 {
			sf := m.start + 2
			points[PointRatedPower] = Register{Table: TableHolding, Address: m.start + 1, Type: TypeUint16, ScaleFactor: &sf, Unavailable: []float64{0xFFFF}}
		}
		if m, ok := firstModel(models, 123); ok && m.length >= 22 && profile.SunSpec.Control {
			sf := m.start + 21
			limit := Register{Table: TableHolding, Address: m.start + 3, Type: TypeUint16, ScaleFactor: &sf}
			points[PointPowerLimit] = limit
			controls[ControlPowerLimit] = Control{
				Register: limit,
				Min:      0,
				Max:      100,
				// Revert timeout 0: the limit stays until changed, so a
				// controller outage keeps the site curtailed, not open.
				Extra:  []Write{{Table: TableHolding, Address: m.start + 5, Values: []uint16{0}}},
				Enable: &Write{Table: TableHolding, Address: m.start + 7, Values: []uint16{1}},
			}
		}

	case KindMeter:
		if m, ok := firstModel(models, 203, 202, 201); ok {
			addMeterIntModel(points, m)
		} else if m, ok := firstModel(models, 213, 212, 211); ok {
			addMeterFloatModel(points, m)
		} else {
			return nil, nil, errors.New("sunspec: no meter model (201-203, 211-213) found")
		}
	}
	return points, controls, nil
}

func sunspecModels(ctx context.Context, client RegisterClient, unitID byte, bases []uint16) ([]sunspecModel, error) {
	var lastErr error
	for _, base := range bases {
		marker, err := client.ReadHoldingRegisters(ctx, unitID, base, 2)
		if err != nil {
			lastErr = err
			continue
		}
		if marker[0] != sunspecMarkerHigh || marker[1] != sunspecMarkerLow {
			continue
		}
		var models []sunspecModel
		address := base + 2
		for range 64 {
			header, err := client.ReadHoldingRegisters(ctx, unitID, address, 2)
			if err != nil {
				return nil, fmt.Errorf("sunspec: read model header at %d: %w", address, err)
			}
			if header[0] == sunspecEnd || header[0] == 0 {
				return models, nil
			}
			models = append(models, sunspecModel{id: header[0], start: address + 2, length: header[1]})
			next := int(address) + 2 + int(header[1])
			if next > 0xFFFF {
				return nil, errors.New("sunspec: model chain runs past the address space")
			}
			address = uint16(next)
		}
		return models, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("sunspec: %w", lastErr)
	}
	return nil, errors.New(`sunspec: no "SunS" marker at the configured base addresses`)
}

func firstModel(models []sunspecModel, ids ...uint16) (sunspecModel, bool) {
	for _, id := range ids {
		for _, m := range models {
			if m.id == id {
				return m, true
			}
		}
	}
	return sunspecModel{}, false
}

// SunSpec marks unimplemented values with these raw values.
var (
	naInt16  = []float64{-32768}
	naUint16 = []float64{0xFFFF}
	naAcc32  = []float64{0}
)

func sfAt(m sunspecModel, offset uint16) *uint16 {
	a := m.start + offset
	return &a
}

// addInverterIntModel maps models 101-103 (integer values with scale factors).
func addInverterIntModel(points map[string]Register, m sunspecModel) {
	if m.length < 31 {
		return
	}
	reg := func(offset uint16, t DataType, sfOffset uint16, scale float64, na []float64) Register {
		return Register{Table: TableHolding, Address: m.start + offset, Type: t, ScaleFactor: sfAt(m, sfOffset), Scale: scale, Unavailable: na}
	}
	points[PointCurrentL1] = reg(1, TypeUint16, 4, 1, naUint16)
	points[PointCurrentL2] = reg(2, TypeUint16, 4, 1, naUint16)
	points[PointCurrentL3] = reg(3, TypeUint16, 4, 1, naUint16)
	points[PointVoltageL1] = reg(8, TypeUint16, 11, 1, naUint16)
	points[PointVoltageL2] = reg(9, TypeUint16, 11, 1, naUint16)
	points[PointVoltageL3] = reg(10, TypeUint16, 11, 1, naUint16)
	points[PointPower] = reg(12, TypeInt16, 13, 1, naInt16)
	points[PointFrequency] = reg(14, TypeUint16, 15, 1, naUint16)
	points[PointPowerFactor] = reg(20, TypeInt16, 21, 0.01, naInt16)
	points[PointEnergyTotal] = reg(22, TypeUint32, 24, 0.001, naAcc32)
	points[PointDCPower] = reg(29, TypeInt16, 30, 1, naInt16)
}

// addInverterFloatModel maps models 111-113 (float32 values).
func addInverterFloatModel(points map[string]Register, m sunspecModel) {
	if m.length < 38 {
		return
	}
	reg := func(offset uint16, scale float64) Register {
		return Register{Table: TableHolding, Address: m.start + offset, Type: TypeFloat32, Scale: scale}
	}
	points[PointCurrentL1] = reg(2, 1)
	points[PointCurrentL2] = reg(4, 1)
	points[PointCurrentL3] = reg(6, 1)
	points[PointVoltageL1] = reg(14, 1)
	points[PointVoltageL2] = reg(16, 1)
	points[PointVoltageL3] = reg(18, 1)
	points[PointPower] = reg(20, 1)
	points[PointFrequency] = reg(22, 1)
	points[PointPowerFactor] = reg(28, 0.01)
	points[PointEnergyTotal] = reg(30, 0.001)
	points[PointDCPower] = reg(36, 1)
}

// addMeterIntModel maps models 201-203 (integer values with scale factors).
func addMeterIntModel(points map[string]Register, m sunspecModel) {
	if m.length < 53 {
		return
	}
	reg := func(offset uint16, t DataType, sfOffset uint16, scale float64, na []float64) Register {
		return Register{Table: TableHolding, Address: m.start + offset, Type: t, ScaleFactor: sfAt(m, sfOffset), Scale: scale, Unavailable: na}
	}
	points[PointCurrentL1] = reg(1, TypeInt16, 4, 1, naInt16)
	points[PointCurrentL2] = reg(2, TypeInt16, 4, 1, naInt16)
	points[PointCurrentL3] = reg(3, TypeInt16, 4, 1, naInt16)
	points[PointVoltageL1] = reg(6, TypeInt16, 13, 1, naInt16)
	points[PointVoltageL2] = reg(7, TypeInt16, 13, 1, naInt16)
	points[PointVoltageL3] = reg(8, TypeInt16, 13, 1, naInt16)
	points[PointFrequency] = reg(14, TypeInt16, 15, 1, naInt16)
	points[PointPower] = reg(16, TypeInt16, 20, 1, naInt16)
	points[PointPowerL1] = reg(17, TypeInt16, 20, 1, naInt16)
	points[PointPowerL2] = reg(18, TypeInt16, 20, 1, naInt16)
	points[PointPowerL3] = reg(19, TypeInt16, 20, 1, naInt16)
	points[PointPowerFactor] = reg(31, TypeInt16, 35, 0.01, naInt16)
	points[PointEnergyExport] = reg(36, TypeUint32, 52, 0.001, naAcc32)
	points[PointEnergyImport] = reg(44, TypeUint32, 52, 0.001, naAcc32)
}

// addMeterFloatModel maps models 211-213 (float32 values).
func addMeterFloatModel(points map[string]Register, m sunspecModel) {
	if m.length < 68 {
		return
	}
	reg := func(offset uint16, scale float64) Register {
		return Register{Table: TableHolding, Address: m.start + offset, Type: TypeFloat32, Scale: scale}
	}
	points[PointCurrentL1] = reg(2, 1)
	points[PointCurrentL2] = reg(4, 1)
	points[PointCurrentL3] = reg(6, 1)
	points[PointVoltageL1] = reg(10, 1)
	points[PointVoltageL2] = reg(12, 1)
	points[PointVoltageL3] = reg(14, 1)
	points[PointFrequency] = reg(24, 1)
	points[PointPower] = reg(26, 1)
	points[PointPowerL1] = reg(28, 1)
	points[PointPowerL2] = reg(30, 1)
	points[PointPowerL3] = reg(32, 1)
	points[PointPowerFactor] = reg(50, 0.01)
	points[PointEnergyExport] = reg(58, 0.001)
	points[PointEnergyImport] = reg(66, 0.001)
}
