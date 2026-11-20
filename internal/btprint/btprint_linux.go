package btprint

import (
	"fmt"
	"net"
	"os"
	"syscall"
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

	fd, err := syscall.Socket(syscall.AF_BLUETOOTH, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, btprotoRFCOMM)
	if err != nil {
		return fmt.Errorf("bluetooth socket: %w", err)
	}
	f := os.NewFile(uintptr(fd), "rfcomm")
	defer f.Close()
	// RFCOMM uses the send timeout for connect too.
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_SNDTIMEO, &tv); err != nil {
		return fmt.Errorf("bluetooth timeout: %w", err)
	}
	for {
		_, _, errno := syscall.Syscall(syscall.SYS_CONNECT, uintptr(fd), uintptr(unsafe.Pointer(&sa)), unsafe.Sizeof(sa))
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return fmt.Errorf("connect to printer %s: %w", mac, errno)
		}
		break
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write to printer %s: %w", mac, err)
	}
	return nil
}
