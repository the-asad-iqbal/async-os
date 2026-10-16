package modbus

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Stream is a byte stream with deadlines: a serial port or a TCP connection.
type Stream interface {
	io.ReadWriteCloser
	SetDeadline(t time.Time) error
}

// flusher is implemented by serial ports, which may hold stale bytes from a
// response that arrived after its request timed out.
type flusher interface {
	FlushInput() error
}

// Opener opens the underlying stream.
type Opener func(ctx context.Context) (Stream, error)

// RTUTransport speaks Modbus RTU framing (unit, PDU, CRC) over a stream.
// Used for RS-485 serial lines and for RS-485 gateways in "RTU over TCP"
// mode.
type RTUTransport struct {
	open     Opener
	timeout  time.Duration
	frameGap time.Duration

	mu        sync.Mutex
	stream    Stream
	lastFrame time.Time
}

// NewRTUTransport builds an RTU transport. frameGap is the silent interval
// required between frames (3.5 characters on serial lines, zero on TCP).
func NewRTUTransport(open Opener, timeout, frameGap time.Duration) *RTUTransport {
	return &RTUTransport{open: open, timeout: timeout, frameGap: frameGap}
}

// NewRTUOverTCPTransport returns an RTU transport over a TCP connection.
func NewRTUOverTCPTransport(address string, timeout time.Duration) *RTUTransport {
	open := func(ctx context.Context) (Stream, error) {
		var dialer net.Dialer
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, fmt.Errorf("modbus: connect %s: %w", address, err)
		}
		return conn.(*net.TCPConn), nil
	}
	return NewRTUTransport(open, timeout, 0)
}

// SerialFrameGap returns the 3.5 character silent interval for a baud rate.
// The specification fixes it at 1.75 ms above 19200 baud.
func SerialFrameGap(baud int) time.Duration {
	if baud > 19200 {
		return 1750 * time.Microsecond
	}
	// 11 bits per character: start, 8 data, parity or second stop, stop.
	return time.Duration(float64(time.Second) * 3.5 * 11 / float64(baud))
}

// Send implements Transport.
func (t *RTUTransport) Send(ctx context.Context, unitID byte, pdu []byte) ([]byte, error) {
	if unitID == 0 {
		return nil, fmt.Errorf("modbus: broadcast is not supported")
	}
	expected := expectedResponseLength(pdu)
	if expected < 0 {
		return nil, fmt.Errorf("modbus: unsupported function 0x%02x over RTU", pdu[0])
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	stream, err := t.connect(ctx)
	if err != nil {
		return nil, err
	}
	response, err := t.exchange(ctx, stream, unitID, pdu, expected)
	t.lastFrame = time.Now()
	if err != nil {
		// Serial ports stay open; a fresh TCP connection drops stale bytes.
		if _, isSerial := stream.(flusher); !isSerial {
			t.closeLocked()
		}
		return nil, err
	}
	return response, nil
}

func (t *RTUTransport) connect(ctx context.Context) (Stream, error) {
	if t.stream != nil {
		return t.stream, nil
	}
	stream, err := t.open(ctx)
	if err != nil {
		return nil, err
	}
	t.stream = stream
	return stream, nil
}

func (t *RTUTransport) exchange(ctx context.Context, stream Stream, unitID byte, pdu []byte, expected int) ([]byte, error) {
	if wait := time.Until(t.lastFrame.Add(t.frameGap)); wait > 0 {
		time.Sleep(wait)
	}
	if f, ok := stream.(flusher); ok {
		if err := f.FlushInput(); err != nil {
			return nil, fmt.Errorf("modbus: flush: %w", err)
		}
	}

	if deadline, ok := ctx.Deadline(); ok {
		if err := stream.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}
	stop := context.AfterFunc(ctx, func() { _ = stream.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	frame := make([]byte, 0, len(pdu)+3)
	frame = append(frame, unitID)
	frame = append(frame, pdu...)
	frame = appendCRC(frame)
	if _, err := stream.Write(frame); err != nil {
		return nil, wrapIOError(ctx, err)
	}

	// Unit and function code tell us how long the rest of the frame is.
	head := make([]byte, 2)
	if _, err := io.ReadFull(stream, head); err != nil {
		return nil, wrapIOError(ctx, err)
	}
	remaining := expected - 1 + 2
	if head[1] == pdu[0]|0x80 {
		remaining = 1 + 2
	}
	rest := make([]byte, remaining)
	if _, err := io.ReadFull(stream, rest); err != nil {
		return nil, wrapIOError(ctx, err)
	}

	response := append(head, rest...)
	if !validCRC(response) {
		return nil, fmt.Errorf("%w: CRC mismatch", ErrInvalidResponse)
	}
	if response[0] != unitID {
		return nil, fmt.Errorf("%w: answer from unit %d, expected %d", ErrInvalidResponse, response[0], unitID)
	}
	return response[1 : len(response)-2], nil
}

func (t *RTUTransport) closeLocked() {
	if t.stream != nil {
		_ = t.stream.Close()
		t.stream = nil
	}
}

// Close implements Transport.
func (t *RTUTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closeLocked()
	return nil
}
