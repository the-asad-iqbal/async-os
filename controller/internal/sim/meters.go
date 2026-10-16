package sim

import (
	"math"
	"net"
	"strconv"

	"github.com/the-asad-iqbal/async-os/controller/internal/modbus"
)

// sdm630 is an Eastron SDM630 register image (float32 input registers).
type sdm630 struct {
	bank      *modbus.RegisterBank
	unitID    byte
	importKWh float64
}

func newSDM630(bank *modbus.RegisterBank, unitID byte) *sdm630 {
	m := &sdm630{bank: bank, unitID: unitID}
	for address := uint16(0); address < 80; address++ {
		bank.SetInput(unitID, address, 0)
	}
	m.put(70, 50) // frequency
	return m
}

func (m *sdm630) put(address uint16, v float64) {
	bits := math.Float32bits(float32(v))
	m.bank.SetInput(m.unitID, address, uint16(bits>>16), uint16(bits))
}

func (m *sdm630) set(powerW, voltage float64) {
	for i := range uint16(3) {
		m.put(0+2*i, voltage)
		current := 0.0
		if voltage > 0 {
			current = math.Abs(powerW) / 3 / voltage
		}
		m.put(6+2*i, current)
		m.put(12+2*i, powerW/3)
	}
	m.put(52, powerW)
	m.put(62, 0.98)
}

func (m *sdm630) addEnergy(kwh float64) {
	m.importKWh += kwh
	m.put(72, m.importKWh)
}

func splitHostPort(address string) (string, int) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return address, 502
	}
	port, _ := strconv.Atoi(portText)
	return host, port
}
