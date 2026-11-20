package btprint

import (
	"cmp"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
	"unsafe"
)

const btprotoRFCOMM = 3

// sockaddrRC is struct sockaddr_rc from <bluetooth/rfcomm.h>.
type sockaddrRC struct {
	family  uint16
	bdaddr  [6]byte // little-endian: reversed from the printed form
	channel uint8
	_       uint8
}

func send(mac string, data []byte) error {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return fmt.Errorf("printer address %q: invalid", mac)
	}
	// shortcut: channel 1 is fixed, it is the SPP channel on the BT-583; make it configurable for other printers.
	sa := sockaddrRC{family: syscall.AF_BLUETOOTH, channel: 1}
	for i := range 6 {
		sa.bdaddr[i] = hw[5-i]
	}

	// Reuse the open connection: after closing one, this host's controller answers every new
	// connect with EALREADY until the adapter is power-cycled. A held connection keeps working.
	if conn != nil {
		if err := write(conn, data); err == nil {
			return nil
		}
		conn.Close() // printer was off or dropped the link: reconnect once
		conn = nil
	}
	f, err := dial(sa, time.Now().Add(timeout))
	if err != nil {
		return fmt.Errorf("connect to printer %s: %w", mac, err)
	}
	if err := write(f, data); err != nil {
		f.Close()
		return fmt.Errorf("write to printer %s: %w", mac, err)
	}
	conn = f
	return nil
}

// conn is the open printer connection, guarded by mu. shortcut: one printer only; key it by mac to support more.
var conn *os.File

func write(f *os.File, data []byte) error {
	if err := f.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	_, err := f.Write(data)
	return err
}

// dial opens an RFCOMM connection on a new socket. The caller closes the file.
func dial(sa sockaddrRC, deadline time.Time) (*os.File, error) {
	fd, err := syscall.Socket(syscall.AF_BLUETOOTH, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC|syscall.SOCK_NONBLOCK, btprotoRFCOMM)
	if err != nil {
		return nil, fmt.Errorf("bluetooth socket: %w", err)
	}
	// Non-blocking fd: os.File registers it with the Go poller, so deadlines work and runtime
	// signals cannot interrupt a blocking connect (retrying that gave EALREADY).
	f := os.NewFile(uintptr(fd), "rfcomm")
	if err := connect(f, fd, sa, deadline); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func connect(f *os.File, fd int, sa sockaddrRC, deadline time.Time) error {
	if err := f.SetDeadline(deadline); err != nil {
		return err
	}
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	// Connect once: RFCOMM answers a second connect with EBADFD. Then wait for writable and read SO_ERROR.
	_, _, errno := syscall.Syscall(syscall.SYS_CONNECT, uintptr(fd), uintptr(unsafe.Pointer(&sa)), unsafe.Sizeof(sa))
	if errno != 0 && errno != syscall.EINPROGRESS {
		return errno
	}
	var connErr error
	waited := errno == 0
	err = rc.Write(func(fd uintptr) bool {
		if !waited {
			waited = true
			return false // first call runs before any wait
		}
		e, err := syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_ERROR)
		if err == nil && e != 0 {
			err = syscall.Errno(e)
		}
		connErr = err
		return true
	})
	return cmp.Or(err, connErr)
}
