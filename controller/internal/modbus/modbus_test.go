package modbus

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestCRC16(t *testing.T) {
	// Read 2 holding registers at 0 from unit 1: well-known frame 01 03 00 00 00 02 C4 0B.
	frame := appendCRC([]byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x02})
	if frame[6] != 0xC4 || frame[7] != 0x0B {
		t.Fatalf("crc = % X, want C4 0B", frame[6:])
	}
	if !validCRC(frame) {
		t.Fatal("validCRC rejected a correct frame")
	}
	frame[3] ^= 1
	if validCRC(frame) {
		t.Fatal("validCRC accepted a corrupted frame")
	}
}

func startServer(t *testing.T, bank *RegisterBank) *Client {
	t.Helper()
	server := NewServer(bank)
	address, err := server.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(NewTCPTransport(address, time.Second))
	t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	return client
}

func TestTCPReadWrite(t *testing.T) {
	bank := NewRegisterBank()
	bank.SetHolding(1, 100, 10, 20, 30)
	bank.SetInput(1, 0, 0xABCD)
	client := startServer(t, bank)
	ctx := context.Background()

	got, err := client.ReadHoldingRegisters(ctx, 1, 100, 3)
	if err != nil || len(got) != 3 || got[0] != 10 || got[2] != 30 {
		t.Fatalf("ReadHoldingRegisters = %v, %v", got, err)
	}
	in, err := client.ReadInputRegisters(ctx, 1, 0, 1)
	if err != nil || in[0] != 0xABCD {
		t.Fatalf("ReadInputRegisters = %v, %v", in, err)
	}
	if err := client.WriteRegisters(ctx, 1, 101, []uint16{99}); err != nil {
		t.Fatal(err)
	}
	if err := client.WriteMultipleRegisters(ctx, 1, 100, []uint16{1, 2}); err != nil {
		t.Fatal(err)
	}
	if bank.Holding(1, 100) != 1 || bank.Holding(1, 101) != 2 || bank.Holding(1, 102) != 30 {
		t.Fatal("writes not stored")
	}
}

func TestTCPException(t *testing.T) {
	bank := NewRegisterBank()
	bank.SetHolding(1, 0, 1)
	client := startServer(t, bank)

	_, err := client.ReadHoldingRegisters(context.Background(), 1, 500, 1)
	var ex *ExceptionError
	if !errors.As(err, &ex) || ex.Code != 2 {
		t.Fatalf("err = %v, want illegal data address", err)
	}
	// The connection stays usable after an exception.
	if _, err := client.ReadHoldingRegisters(context.Background(), 1, 0, 1); err != nil {
		t.Fatal(err)
	}
}

func TestTCPReconnectsAfterServerRestart(t *testing.T) {
	bank := NewRegisterBank()
	bank.SetHolding(1, 0, 7)
	server := NewServer(bank)
	address, err := server.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(NewTCPTransport(address, 500*time.Millisecond))
	defer client.Close()
	if _, err := client.ReadHoldingRegisters(context.Background(), 1, 0, 1); err != nil {
		t.Fatal(err)
	}
	server.Close()
	if _, err := client.ReadHoldingRegisters(context.Background(), 1, 0, 1); err == nil {
		t.Fatal("expected an error while the server is down")
	}
	server = NewServer(bank)
	if _, err := server.Listen(address); err != nil {
		t.Skipf("port %s not reusable: %v", address, err)
	}
	defer server.Close()
	if _, err := client.ReadHoldingRegisters(context.Background(), 1, 0, 1); err != nil {
		t.Fatalf("no reconnect: %v", err)
	}
}

func TestTCPTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			io.Copy(io.Discard, conn) // never answers
		}
	}()
	client := NewClient(NewTCPTransport(listener.Addr().String(), 100*time.Millisecond))
	defer client.Close()
	start := time.Now()
	if _, err := client.ReadHoldingRegisters(context.Background(), 1, 0, 1); err == nil {
		t.Fatal("expected timeout")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout took %v", elapsed)
	}
}

// scriptedStream answers each written frame with the next scripted reply.
type scriptedStream struct {
	replies  [][]byte
	pending  []byte
	written  [][]byte
	flushes  int
	deadline time.Time
}

func (s *scriptedStream) Write(p []byte) (int, error) {
	s.written = append(s.written, append([]byte(nil), p...))
	if len(s.replies) > 0 {
		s.pending = append(s.pending, s.replies[0]...)
		s.replies = s.replies[1:]
	}
	return len(p), nil
}

func (s *scriptedStream) Read(p []byte) (int, error) {
	if len(s.pending) == 0 {
		return 0, errors.New("i/o timeout")
	}
	n := copy(p, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}

func (s *scriptedStream) Close() error                  { return nil }
func (s *scriptedStream) SetDeadline(t time.Time) error { s.deadline = t; return nil }
func (s *scriptedStream) FlushInput() error             { s.flushes++; s.pending = nil; return nil }

func rtuClient(stream *scriptedStream) *Client {
	open := func(context.Context) (Stream, error) { return stream, nil }
	return NewClient(NewRTUTransport(open, time.Second, 0))
}

func TestRTURead(t *testing.T) {
	stream := &scriptedStream{replies: [][]byte{appendCRC([]byte{0x05, 0x04, 0x04, 0x12, 0x34, 0x56, 0x78})}}
	got, err := rtuClient(stream).ReadInputRegisters(context.Background(), 5, 0x10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != 0x1234 || got[1] != 0x5678 {
		t.Fatalf("got % X", got)
	}
	want := appendCRC([]byte{0x05, 0x04, 0x00, 0x10, 0x00, 0x02})
	if string(stream.written[0]) != string(want) {
		t.Fatalf("request % X, want % X", stream.written[0], want)
	}
	if stream.flushes != 1 {
		t.Fatal("serial input not flushed before the request")
	}
}

func TestRTUException(t *testing.T) {
	stream := &scriptedStream{replies: [][]byte{appendCRC([]byte{0x01, 0x83, 0x02})}}
	_, err := rtuClient(stream).ReadHoldingRegisters(context.Background(), 1, 0, 1)
	var ex *ExceptionError
	if !errors.As(err, &ex) || ex.Code != 2 {
		t.Fatalf("err = %v", err)
	}
}

func TestRTURejectsBadCRCAndWrongUnit(t *testing.T) {
	bad := appendCRC([]byte{0x01, 0x03, 0x02, 0x00, 0x01})
	bad[len(bad)-1] ^= 0xFF
	stream := &scriptedStream{replies: [][]byte{bad}}
	if _, err := rtuClient(stream).ReadHoldingRegisters(context.Background(), 1, 0, 1); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("bad CRC: err = %v", err)
	}
	stream = &scriptedStream{replies: [][]byte{appendCRC([]byte{0x02, 0x03, 0x02, 0x00, 0x01})}}
	if _, err := rtuClient(stream).ReadHoldingRegisters(context.Background(), 1, 0, 1); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("wrong unit: err = %v", err)
	}
}

func TestRTUWriteEcho(t *testing.T) {
	stream := &scriptedStream{replies: [][]byte{appendCRC([]byte{0x01, 0x10, 0x00, 0x05, 0x00, 0x02})}}
	if err := rtuClient(stream).WriteRegisters(context.Background(), 1, 5, []uint16{1, 2}); err != nil {
		t.Fatal(err)
	}
}

func TestSerialFrameGap(t *testing.T) {
	if got := SerialFrameGap(9600); got < 4*time.Millisecond || got > 4100*time.Microsecond {
		t.Fatalf("9600 baud gap = %v, want about 4.01ms", got)
	}
	if got := SerialFrameGap(115200); got != 1750*time.Microsecond {
		t.Fatalf("115200 baud gap = %v", got)
	}
}
