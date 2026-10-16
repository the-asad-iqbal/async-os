package modbus

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// SerialConfig describes an RS-485/RS-232 line.
type SerialConfig struct {
	Device   string // e.g. /dev/ttyUSB0
	BaudRate int
	DataBits int    // 7 or 8
	Parity   string // "N", "E" or "O"
	StopBits int    // 1 or 2
}

// serialPort is a tty opened non-blocking so os.File deadlines work.
type serialPort struct {
	*os.File
}

func (p *serialPort) FlushInput() error {
	return unix.IoctlSetInt(int(p.Fd()), unix.TCFLSH, unix.TCIFLUSH)
}

// NewSerialRTUTransport returns an RTU transport on a serial line. The port
// is opened on first use and kept open.
func NewSerialRTUTransport(cfg SerialConfig, timeout time.Duration) *RTUTransport {
	open := func(context.Context) (Stream, error) { return openSerial(cfg) }
	return NewRTUTransport(open, timeout, SerialFrameGap(cfg.BaudRate))
}

func openSerial(cfg SerialConfig) (*serialPort, error) {
	fd, err := unix.Open(cfg.Device, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("modbus: open %s: %w", cfg.Device, err)
	}
	if err := configureSerial(fd, cfg); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("modbus: configure %s: %w", cfg.Device, err)
	}
	// A non-blocking fd makes os.File use the runtime poller, so
	// SetDeadline bounds every read and write.
	return &serialPort{File: os.NewFile(uintptr(fd), cfg.Device)}, nil
}

func configureSerial(fd int, cfg SerialConfig) error {
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS2)
	if err != nil {
		return err
	}

	// Raw mode: no echo, no signals, no line editing, no translation.
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON | unix.IXOFF | unix.IXANY | unix.INPCK
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB | unix.PARODD | unix.CSTOPB | unix.CRTSCTS | unix.CBAUD
	t.Cflag |= unix.CLOCAL | unix.CREAD

	switch cfg.DataBits {
	case 7:
		t.Cflag |= unix.CS7
	case 8, 0:
		t.Cflag |= unix.CS8
	default:
		return fmt.Errorf("unsupported data bits %d", cfg.DataBits)
	}
	switch cfg.Parity {
	case "N", "":
	case "E":
		t.Cflag |= unix.PARENB
		t.Iflag |= unix.INPCK
	case "O":
		t.Cflag |= unix.PARENB | unix.PARODD
		t.Iflag |= unix.INPCK
	default:
		return fmt.Errorf("unsupported parity %q", cfg.Parity)
	}
	switch cfg.StopBits {
	case 1, 0:
	case 2:
		t.Cflag |= unix.CSTOPB
	default:
		return fmt.Errorf("unsupported stop bits %d", cfg.StopBits)
	}
	if cfg.BaudRate <= 0 {
		return fmt.Errorf("invalid baud rate %d", cfg.BaudRate)
	}
	// termios2 with BOTHER accepts any baud rate the adapter supports.
	t.Cflag |= unix.BOTHER
	t.Ispeed = uint32(cfg.BaudRate)
	t.Ospeed = uint32(cfg.BaudRate)

	// Reads return whatever is available; deadlines handle timeouts.
	t.Cc[unix.VMIN] = 0
	t.Cc[unix.VTIME] = 0

	return unix.IoctlSetTermios(fd, unix.TCSETS2, t)
}
