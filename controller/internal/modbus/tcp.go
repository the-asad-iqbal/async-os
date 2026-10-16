package modbus

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const mbapHeaderLength = 7

// TCPTransport speaks Modbus TCP (MBAP framing). The connection is opened on
// first use and re-opened after any I/O error.
type TCPTransport struct {
	address string
	timeout time.Duration

	mu            sync.Mutex
	conn          net.Conn
	transactionID uint16
}

// NewTCPTransport returns a transport for host:port. timeout bounds each
// request, including the connect.
func NewTCPTransport(address string, timeout time.Duration) *TCPTransport {
	return &TCPTransport{address: address, timeout: timeout}
}

// Send implements Transport.
func (t *TCPTransport) Send(ctx context.Context, unitID byte, pdu []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	conn, err := t.connect(ctx)
	if err != nil {
		return nil, err
	}
	response, err := t.exchange(ctx, conn, unitID, pdu)
	if err != nil {
		t.closeLocked()
		return nil, err
	}
	return response, nil
}

func (t *TCPTransport) connect(ctx context.Context) (net.Conn, error) {
	if t.conn != nil {
		return t.conn, nil
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", t.address)
	if err != nil {
		return nil, fmt.Errorf("modbus: connect %s: %w", t.address, err)
	}
	t.conn = conn
	return conn, nil
}

func (t *TCPTransport) exchange(ctx context.Context, conn net.Conn, unitID byte, pdu []byte) ([]byte, error) {
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}
	// Unblock reads and writes as soon as the caller gives up.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	t.transactionID++
	frame := make([]byte, mbapHeaderLength+len(pdu))
	binary.BigEndian.PutUint16(frame[0:], t.transactionID)
	binary.BigEndian.PutUint16(frame[2:], 0) // protocol identifier
	binary.BigEndian.PutUint16(frame[4:], uint16(len(pdu)+1))
	frame[6] = unitID
	copy(frame[mbapHeaderLength:], pdu)
	if _, err := conn.Write(frame); err != nil {
		return nil, wrapIOError(ctx, err)
	}

	header := make([]byte, mbapHeaderLength)
	for {
		if _, err := io.ReadFull(conn, header); err != nil {
			return nil, wrapIOError(ctx, err)
		}
		length := int(binary.BigEndian.Uint16(header[4:]))
		if length < 2 || length > maxPDULength+1 || binary.BigEndian.Uint16(header[2:]) != 0 {
			return nil, fmt.Errorf("%w: bad MBAP header", ErrInvalidResponse)
		}
		body := make([]byte, length-1)
		if _, err := io.ReadFull(conn, body); err != nil {
			return nil, wrapIOError(ctx, err)
		}
		// A late answer to an earlier, timed-out request: skip it.
		if binary.BigEndian.Uint16(header[0:]) != t.transactionID {
			continue
		}
		return body, nil
	}
}

func (t *TCPTransport) closeLocked() {
	if t.conn != nil {
		_ = t.conn.Close()
		t.conn = nil
	}
}

// Close implements Transport.
func (t *TCPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closeLocked()
	return nil
}

// wrapIOError reports the context's reason when the deadline fired because
// of it, which reads better than "i/o timeout".
func wrapIOError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("modbus: %w", ctxErr)
	}
	return fmt.Errorf("modbus: %w", err)
}
