package modbus

import (
	"context"
	"sync"
)

// Transport carries one request PDU to a unit and returns its response PDU.
// Implementations serialize access: Modbus devices answer one request at a
// time, and an RS-485 bus is shared by every device on it.
type Transport interface {
	Send(ctx context.Context, unitID byte, pdu []byte) ([]byte, error)
	Close() error
}

// Client issues Modbus requests over a Transport.
type Client struct {
	transport Transport
	mu        sync.Mutex
}

// NewClient wraps a transport.
func NewClient(t Transport) *Client {
	return &Client{transport: t}
}

func (c *Client) send(ctx context.Context, unitID byte, pdu []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.transport.Send(ctx, unitID, pdu)
}

// ReadHoldingRegisters reads quantity holding registers (function 3).
func (c *Client) ReadHoldingRegisters(ctx context.Context, unitID byte, address, quantity uint16) ([]uint16, error) {
	return c.read(ctx, unitID, FuncReadHoldingRegisters, address, quantity)
}

// ReadInputRegisters reads quantity input registers (function 4).
func (c *Client) ReadInputRegisters(ctx context.Context, unitID byte, address, quantity uint16) ([]uint16, error) {
	return c.read(ctx, unitID, FuncReadInputRegisters, address, quantity)
}

func (c *Client) read(ctx context.Context, unitID, function byte, address, quantity uint16) ([]uint16, error) {
	request, err := readRequest(function, address, quantity)
	if err != nil {
		return nil, err
	}
	response, err := c.send(ctx, unitID, request)
	if err != nil {
		return nil, err
	}
	return parseReadResponse(request, response)
}

// WriteRegisters writes one register with function 6, or several with
// function 16.
func (c *Client) WriteRegisters(ctx context.Context, unitID byte, address uint16, values []uint16) error {
	var request []byte
	if len(values) == 1 {
		request = writeSingleRequest(address, values[0])
	} else {
		var err error
		if request, err = writeMultipleRequest(address, values); err != nil {
			return err
		}
	}
	response, err := c.send(ctx, unitID, request)
	if err != nil {
		return err
	}
	return parseWriteResponse(request, response)
}

// WriteMultipleRegisters always uses function 16, which some devices require
// even for a single register.
func (c *Client) WriteMultipleRegisters(ctx context.Context, unitID byte, address uint16, values []uint16) error {
	request, err := writeMultipleRequest(address, values)
	if err != nil {
		return err
	}
	response, err := c.send(ctx, unitID, request)
	if err != nil {
		return err
	}
	return parseWriteResponse(request, response)
}

// Close releases the transport.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.transport.Close()
}
