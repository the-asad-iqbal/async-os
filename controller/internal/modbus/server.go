package modbus

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
)

// Handler answers requests for a Server. Returning an *ExceptionError sends
// that exception; any other error sends "server device failure".
type Handler interface {
	ReadRegisters(unitID, function byte, address, quantity uint16) ([]uint16, error)
	WriteRegisters(unitID byte, address uint16, values []uint16) error
}

// Server is a minimal Modbus TCP server for simulators and tests.
type Server struct {
	handler Handler

	mu       sync.Mutex
	listener net.Listener
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
}

// NewServer returns a server for handler.
func NewServer(handler Handler) *Server {
	return &Server{handler: handler, conns: map[net.Conn]struct{}{}}
}

// Listen starts serving on address ("127.0.0.1:0" picks a free port) and
// returns the bound address.
func (s *Server) Listen(address string) (string, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.conns[conn] = struct{}{}
			s.mu.Unlock()
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				s.serve(conn)
				s.mu.Lock()
				delete(s.conns, conn)
				s.mu.Unlock()
			}()
		}
	}()
	return listener.Addr().String(), nil
}

// Close stops the server and waits for its connections to end.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.listener != nil {
		_ = s.listener.Close()
	}
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	return nil
}

func (s *Server) serve(conn net.Conn) {
	defer conn.Close()
	header := make([]byte, mbapHeaderLength)
	for {
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		length := int(binary.BigEndian.Uint16(header[4:]))
		if length < 2 || length > maxPDULength+1 {
			return
		}
		request := make([]byte, length-1)
		if _, err := io.ReadFull(conn, request); err != nil {
			return
		}
		response := s.handle(header[6], request)
		frame := make([]byte, mbapHeaderLength+len(response))
		copy(frame, header[:4])
		binary.BigEndian.PutUint16(frame[4:], uint16(len(response)+1))
		frame[6] = header[6]
		copy(frame[mbapHeaderLength:], response)
		if _, err := conn.Write(frame); err != nil {
			return
		}
	}
}

func (s *Server) handle(unitID byte, request []byte) []byte {
	exception := func(code byte) []byte { return []byte{request[0] | 0x80, code} }
	fromError := func(err error) []byte {
		var ex *ExceptionError
		if errors.As(err, &ex) {
			return exception(ex.Code)
		}
		return exception(4)
	}

	switch request[0] {
	case FuncReadHoldingRegisters, FuncReadInputRegisters:
		if len(request) != 5 {
			return exception(3)
		}
		address := binary.BigEndian.Uint16(request[1:])
		quantity := binary.BigEndian.Uint16(request[3:])
		if quantity == 0 || quantity > MaxReadQuantity {
			return exception(3)
		}
		values, err := s.handler.ReadRegisters(unitID, request[0], address, quantity)
		if err != nil {
			return fromError(err)
		}
		response := make([]byte, 2+2*len(values))
		response[0] = request[0]
		response[1] = byte(2 * len(values))
		for i, v := range values {
			binary.BigEndian.PutUint16(response[2+2*i:], v)
		}
		return response

	case FuncWriteSingleRegister:
		if len(request) != 5 {
			return exception(3)
		}
		address := binary.BigEndian.Uint16(request[1:])
		if err := s.handler.WriteRegisters(unitID, address, []uint16{binary.BigEndian.Uint16(request[3:])}); err != nil {
			return fromError(err)
		}
		return request

	case FuncWriteMultipleRegisters:
		if len(request) < 6 {
			return exception(3)
		}
		address := binary.BigEndian.Uint16(request[1:])
		quantity := int(binary.BigEndian.Uint16(request[3:]))
		if quantity == 0 || quantity > MaxWriteQuantity || int(request[5]) != 2*quantity || len(request) != 6+2*quantity {
			return exception(3)
		}
		values := make([]uint16, quantity)
		for i := range values {
			values[i] = binary.BigEndian.Uint16(request[6+2*i:])
		}
		if err := s.handler.WriteRegisters(unitID, address, values); err != nil {
			return fromError(err)
		}
		return request[:5]

	default:
		return exception(1)
	}
}

// RegisterBank is a simple Handler backed by register maps, keyed by unit.
type RegisterBank struct {
	mu      sync.Mutex
	holding map[byte]map[uint16]uint16
	input   map[byte]map[uint16]uint16
	// OnWrite, if set, observes writes after they are stored.
	OnWrite func(unitID byte, address uint16, values []uint16)
}

// NewRegisterBank returns an empty bank.
func NewRegisterBank() *RegisterBank {
	return &RegisterBank{holding: map[byte]map[uint16]uint16{}, input: map[byte]map[uint16]uint16{}}
}

// SetHolding stores holding registers starting at address.
func (b *RegisterBank) SetHolding(unitID byte, address uint16, values ...uint16) {
	b.set(b.holding, unitID, address, values)
}

// SetInput stores input registers starting at address.
func (b *RegisterBank) SetInput(unitID byte, address uint16, values ...uint16) {
	b.set(b.input, unitID, address, values)
}

// Holding returns one holding register.
func (b *RegisterBank) Holding(unitID byte, address uint16) uint16 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.holding[unitID][address]
}

func (b *RegisterBank) set(bank map[byte]map[uint16]uint16, unitID byte, address uint16, values []uint16) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if bank[unitID] == nil {
		bank[unitID] = map[uint16]uint16{}
	}
	for i, v := range values {
		bank[unitID][address+uint16(i)] = v
	}
}

// ReadRegisters implements Handler. Unknown units do not answer, like a
// missing device on a gateway; unknown registers are an illegal address.
func (b *RegisterBank) ReadRegisters(unitID, function byte, address, quantity uint16) ([]uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	bank := b.holding
	if function == FuncReadInputRegisters {
		bank = b.input
	}
	registers, ok := bank[unitID]
	if !ok {
		return nil, &ExceptionError{Function: function, Code: 11}
	}
	values := make([]uint16, quantity)
	for i := range values {
		v, ok := registers[address+uint16(i)]
		if !ok {
			return nil, &ExceptionError{Function: function, Code: 2}
		}
		values[i] = v
	}
	return values, nil
}

// WriteRegisters implements Handler. Only existing holding registers are
// writable.
func (b *RegisterBank) WriteRegisters(unitID byte, address uint16, values []uint16) error {
	b.mu.Lock()
	registers, ok := b.holding[unitID]
	if !ok {
		b.mu.Unlock()
		return &ExceptionError{Function: FuncWriteMultipleRegisters, Code: 11}
	}
	for i := range values {
		if _, ok := registers[address+uint16(i)]; !ok {
			b.mu.Unlock()
			return &ExceptionError{Function: FuncWriteMultipleRegisters, Code: 2}
		}
	}
	for i, v := range values {
		registers[address+uint16(i)] = v
	}
	onWrite := b.OnWrite
	b.mu.Unlock()
	if onWrite != nil {
		onWrite(unitID, address, values)
	}
	return nil
}

var _ Handler = (*RegisterBank)(nil)
