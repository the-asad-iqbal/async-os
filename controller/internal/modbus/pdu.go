// Package modbus implements a Modbus client for TCP, RTU (serial) and RTU
// framed over TCP, which is what RS-485 to Ethernet gateways speak.
package modbus

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Function codes used by the controller.
const (
	FuncReadHoldingRegisters   byte = 0x03
	FuncReadInputRegisters     byte = 0x04
	FuncWriteSingleRegister    byte = 0x06
	FuncWriteMultipleRegisters byte = 0x10
)

// Protocol limits from the Modbus application protocol specification.
const (
	MaxReadQuantity  = 125
	MaxWriteQuantity = 123
	maxPDULength     = 253
)

// ExceptionError is an exception response returned by a device.
type ExceptionError struct {
	Function byte
	Code     byte
}

func (e *ExceptionError) Error() string {
	return fmt.Sprintf("modbus exception %d (%s) for function 0x%02x", e.Code, exceptionText(e.Code), e.Function)
}

func exceptionText(code byte) string {
	switch code {
	case 1:
		return "illegal function"
	case 2:
		return "illegal data address"
	case 3:
		return "illegal data value"
	case 4:
		return "server device failure"
	case 5:
		return "acknowledge"
	case 6:
		return "server device busy"
	case 10:
		return "gateway path unavailable"
	case 11:
		return "gateway target device failed to respond"
	default:
		return "unknown"
	}
}

// ErrInvalidResponse reports a response that does not match its request.
var ErrInvalidResponse = errors.New("modbus: invalid response")

func readRequest(function byte, address, quantity uint16) ([]byte, error) {
	if quantity == 0 || quantity > MaxReadQuantity {
		return nil, fmt.Errorf("modbus: read quantity %d out of range 1..%d", quantity, MaxReadQuantity)
	}
	pdu := make([]byte, 5)
	pdu[0] = function
	binary.BigEndian.PutUint16(pdu[1:], address)
	binary.BigEndian.PutUint16(pdu[3:], quantity)
	return pdu, nil
}

func writeSingleRequest(address, value uint16) []byte {
	pdu := make([]byte, 5)
	pdu[0] = FuncWriteSingleRegister
	binary.BigEndian.PutUint16(pdu[1:], address)
	binary.BigEndian.PutUint16(pdu[3:], value)
	return pdu
}

func writeMultipleRequest(address uint16, values []uint16) ([]byte, error) {
	if len(values) == 0 || len(values) > MaxWriteQuantity {
		return nil, fmt.Errorf("modbus: write quantity %d out of range 1..%d", len(values), MaxWriteQuantity)
	}
	pdu := make([]byte, 6+2*len(values))
	pdu[0] = FuncWriteMultipleRegisters
	binary.BigEndian.PutUint16(pdu[1:], address)
	binary.BigEndian.PutUint16(pdu[3:], uint16(len(values)))
	pdu[5] = byte(2 * len(values))
	for i, v := range values {
		binary.BigEndian.PutUint16(pdu[6+2*i:], v)
	}
	return pdu, nil
}

// checkException returns the device exception carried by a response, if any.
func checkException(request, response []byte) error {
	if len(response) == 0 {
		return ErrInvalidResponse
	}
	if response[0] == request[0]|0x80 {
		if len(response) < 2 {
			return ErrInvalidResponse
		}
		return &ExceptionError{Function: request[0], Code: response[1]}
	}
	if response[0] != request[0] {
		return fmt.Errorf("%w: function 0x%02x, expected 0x%02x", ErrInvalidResponse, response[0], request[0])
	}
	return nil
}

func parseReadResponse(request, response []byte) ([]uint16, error) {
	if err := checkException(request, response); err != nil {
		return nil, err
	}
	quantity := int(binary.BigEndian.Uint16(request[3:]))
	if len(response) < 2 || int(response[1]) != 2*quantity || len(response) != 2+2*quantity {
		return nil, fmt.Errorf("%w: expected %d registers", ErrInvalidResponse, quantity)
	}
	registers := make([]uint16, quantity)
	for i := range registers {
		registers[i] = binary.BigEndian.Uint16(response[2+2*i:])
	}
	return registers, nil
}

func parseWriteResponse(request, response []byte) error {
	if err := checkException(request, response); err != nil {
		return err
	}
	// Both write functions echo the address and the value or quantity.
	if len(response) != 5 || string(response[1:5]) != string(request[1:5]) {
		return fmt.Errorf("%w: write echo mismatch", ErrInvalidResponse)
	}
	return nil
}

// expectedResponseLength returns the PDU length of a successful response, so
// RTU framing knows how many bytes to wait for.
func expectedResponseLength(request []byte) int {
	switch request[0] {
	case FuncReadHoldingRegisters, FuncReadInputRegisters:
		return 2 + 2*int(binary.BigEndian.Uint16(request[3:]))
	case FuncWriteSingleRegister, FuncWriteMultipleRegisters:
		return 5
	default:
		return -1
	}
}
