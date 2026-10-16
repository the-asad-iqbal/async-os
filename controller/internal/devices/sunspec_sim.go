package devices

import (
	"math"

	"github.com/the-asad-iqbal/async-os/controller/internal/modbus"
)

// SunSpec register images for the simulator and tests. They follow the
// integer + scale factor models (1, 103, 120, 123 and 203).

const (
	simModel1Length   = 66
	simModel103Length = 50
	simModel120Length = 26
	simModel123Length = 24
	simModel203Length = 105
)

// SunSpecInverterSim is a simulated three-phase SunSpec inverter.
type SunSpecInverterSim struct {
	bank      *modbus.RegisterBank
	unitID    byte
	model103  uint16
	model123  uint16
	ratedWatt float64
}

// SimulateSunSpecInverter writes a SunSpec inverter image at base.
func SimulateSunSpecInverter(bank *modbus.RegisterBank, unitID byte, base uint16, ratedWatt float64) *SunSpecInverterSim {
	address := writeSunSpecHeader(bank, unitID, base)

	sim := &SunSpecInverterSim{bank: bank, unitID: unitID, model103: address + 2, ratedWatt: ratedWatt}
	m103 := make([]uint16, simModel103Length)
	for i := range m103 {
		m103[i] = 0xFFFF // unimplemented until set
	}
	m103[4] = uint16(0xFFFE)            // A_SF = -2
	m103[11] = uint16(0xFFFF)           // V_SF = -1
	m103[12] = 0                        // W
	m103[13] = 1                        // W_SF = 1 (tens of watts)
	m103[14] = 5000                     // Hz
	m103[15] = uint16(0xFFFE)           // Hz_SF = -2
	m103[20] = 100                      // PF in percent
	m103[21] = 0                        // PF_SF
	m103[22], m103[23] = 0x00BC, 0x614E // WH = 12345678 Wh
	m103[24] = 0                        // WH_SF
	m103[29] = 0                        // DCW
	m103[30] = 1                        // DCW_SF
	for i, v := range []uint16{2300, 2300, 2300} {
		m103[8+i] = v // 230.0 V
	}
	bank.SetHolding(unitID, address, append([]uint16{103, simModel103Length}, m103...)...)
	address += 2 + simModel103Length

	m120 := make([]uint16, simModel120Length)
	m120[0] = 4 // DERTyp: PV
	m120[1] = uint16(math.Round(ratedWatt / 10))
	m120[2] = 1 // WRtg_SF
	bank.SetHolding(unitID, address, append([]uint16{120, simModel120Length}, m120...)...)
	address += 2 + simModel120Length

	sim.model123 = address + 2
	m123 := make([]uint16, simModel123Length)
	m123[3] = 1000            // WMaxLimPct = 100.0 %
	m123[21] = uint16(0xFFFF) // WMaxLimPct_SF = -1
	bank.SetHolding(unitID, address, append([]uint16{123, simModel123Length}, m123...)...)
	address += 2 + simModel123Length

	bank.SetHolding(unitID, address, 0xFFFF, 0)
	return sim
}

// SetPower sets the reported AC power in watts.
func (s *SunSpecInverterSim) SetPower(watt float64) {
	raw := int16(math.Max(math.MinInt16+1, math.Min(math.MaxInt16, math.Round(watt/10))))
	s.bank.SetHolding(s.unitID, s.model103+12, uint16(raw))
	s.bank.SetHolding(s.unitID, s.model103+29, uint16(raw))
	perPhase := watt / 3 / 230
	for i := range 3 {
		s.bank.SetHolding(s.unitID, s.model103+1+uint16(i), uint16(math.Max(0, math.Round(perPhase*100))))
	}
}

// AddEnergy adds produced energy in Wh to the lifetime counter.
func (s *SunSpecInverterSim) AddEnergy(wh float64) {
	hi := s.bank.Holding(s.unitID, s.model103+22)
	lo := s.bank.Holding(s.unitID, s.model103+23)
	total := uint32(hi)<<16 | uint32(lo)
	total += uint32(math.Max(0, math.Round(wh)))
	s.bank.SetHolding(s.unitID, s.model103+22, uint16(total>>16), uint16(total))
}

// LimitPercent returns the power limit last written.
func (s *SunSpecInverterSim) LimitPercent() float64 {
	return float64(s.bank.Holding(s.unitID, s.model123+3)) / 10
}

// LimitEnabled reports WMaxLim_Ena.
func (s *SunSpecInverterSim) LimitEnabled() bool {
	return s.bank.Holding(s.unitID, s.model123+7) == 1
}

// RevertSeconds returns WMaxLimPct_RvrtTms.
func (s *SunSpecInverterSim) RevertSeconds() uint16 {
	return s.bank.Holding(s.unitID, s.model123+5)
}

// RatedWatt returns the nameplate rating.
func (s *SunSpecInverterSim) RatedWatt() float64 { return s.ratedWatt }

// SunSpecMeterSim is a simulated three-phase SunSpec meter (model 203).
type SunSpecMeterSim struct {
	bank     *modbus.RegisterBank
	unitID   byte
	model203 uint16
}

// SimulateSunSpecMeter writes a SunSpec meter image at base.
func SimulateSunSpecMeter(bank *modbus.RegisterBank, unitID byte, base uint16) *SunSpecMeterSim {
	address := writeSunSpecHeader(bank, unitID, base)
	sim := &SunSpecMeterSim{bank: bank, unitID: unitID, model203: address + 2}

	m := make([]uint16, simModel203Length)
	for i := range m {
		m[i] = 0x8000 // int16 "not implemented"
	}
	m[4] = uint16(0xFFFE)  // A_SF = -2
	m[13] = uint16(0xFFFF) // V_SF = -1
	m[14] = 5000           // Hz
	m[15] = uint16(0xFFFE) // Hz_SF = -2
	m[16] = 0              // W
	m[20] = 1              // W_SF = 1
	for i := range 3 {
		m[6+i] = 2300
	}
	for i := 36; i < 52; i++ {
		m[i] = 0 // energy accumulators
	}
	m[52] = 0 // TotWh_SF
	bank.SetHolding(unitID, address, append([]uint16{203, simModel203Length}, m...)...)
	address += 2 + simModel203Length
	bank.SetHolding(unitID, address, 0xFFFF, 0)
	return sim
}

// SetPower sets total real power in watts, positive when importing.
func (s *SunSpecMeterSim) SetPower(watt float64) {
	raw := func(w float64) uint16 {
		return uint16(int16(math.Max(math.MinInt16+1, math.Min(math.MaxInt16, math.Round(w/10)))))
	}
	s.bank.SetHolding(s.unitID, s.model203+16, raw(watt), raw(watt/3), raw(watt/3), raw(watt/3))
}

// SetEnergy sets the import and export counters in kWh.
func (s *SunSpecMeterSim) SetEnergy(importKWh, exportKWh float64) {
	put := func(offset uint16, kwh float64) {
		wh := uint32(math.Max(0, math.Round(kwh*1000)))
		s.bank.SetHolding(s.unitID, s.model203+offset, uint16(wh>>16), uint16(wh))
	}
	put(36, exportKWh)
	put(44, importKWh)
}

func writeSunSpecHeader(bank *modbus.RegisterBank, unitID byte, base uint16) uint16 {
	bank.SetHolding(unitID, base, sunspecMarkerHigh, sunspecMarkerLow)
	common := make([]uint16, simModel1Length)
	copy(common, asciiRegisters("AsyncOS", 16))
	copy(common[16:], asciiRegisters("Simulator", 16))
	bank.SetHolding(unitID, base+2, append([]uint16{1, simModel1Length}, common...)...)
	return base + 2 + 2 + simModel1Length
}

func asciiRegisters(s string, registers int) []uint16 {
	out := make([]uint16, registers)
	for i := 0; i < len(s) && i/2 < registers; i++ {
		if i%2 == 0 {
			out[i/2] |= uint16(s[i]) << 8
		} else {
			out[i/2] |= uint16(s[i])
		}
	}
	return out
}
