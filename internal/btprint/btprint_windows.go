package btprint

import (
	"fmt"
	"net"
	"syscall"
	"time"
	"unsafe"
)

const (
	afBTH           = 32     // AF_BTH from <ws2bth.h>
	bthprotoRFCOMM  = 3      // BTHPROTO_RFCOMM
	soSndTimeo      = 0x1005 // SO_SNDTIMEO, missing from package syscall on Windows
	soSndTimeoMilli = uint32(timeout / time.Millisecond)
)

// sockaddrBTH is SOCKADDR_BTH from <ws2bth.h>, which is declared with 1-byte packing (30 bytes).
// Go would pad it, so it is laid out by hand as bytes.
type sockaddrBTH [30]byte

func newSockaddrBTH(hw net.HardwareAddr, port uint32) sockaddrBTH {
	var sa sockaddrBTH
	sa[0] = afBTH // addressFamily, little-endian uint16
	// btAddr: uint64, the address as a number, little-endian (reversed from the printed form).
	for i := range 6 {
		sa[2+i] = hw[5-i]
	}
	// serviceClassId (GUID, 16 bytes at offset 10) stays zero: the port is given directly.
	sa[26] = byte(port) // port, little-endian uint32
	sa[27] = byte(port >> 8)
	sa[28] = byte(port >> 16)
	sa[29] = byte(port >> 24)
	return sa
}

var (
	ws2         = syscall.NewLazyDLL("ws2_32.dll")
	procConnect = ws2.NewProc("connect")
)

func send(mac string, data []byte) error {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return fmt.Errorf("printer address %q: invalid", mac)
	}
	// shortcut: channel 1 is fixed, it is the SPP channel on the BT-583; make it configurable for other printers.
	sa := newSockaddrBTH(hw, 1)

	// Reuse the open connection, as on Linux: one connection per job is slow and the printer
	// accepts only one at a time.
	if conn != syscall.InvalidHandle {
		if err := write(conn, data); err == nil {
			return nil
		}
		syscall.Closesocket(conn) // printer was off or dropped the link: reconnect once
		conn = syscall.InvalidHandle
	}
	s, err := dial(sa)
	if err != nil {
		return fmt.Errorf("connect to printer %s: %w", mac, err)
	}
	if err := write(s, data); err != nil {
		syscall.Closesocket(s)
		return fmt.Errorf("write to printer %s: %w", mac, err)
	}
	conn = s
	return nil
}

// conn is the open printer socket, guarded by mu. shortcut: one printer only; key it by mac to support more.
var conn = syscall.InvalidHandle

func init() {
	var d syscall.WSAData
	if err := syscall.WSAStartup(uint32(0x202), &d); err != nil {
		panic(fmt.Sprintf("WSAStartup: %v", err)) // the net package does the same at init
	}
}

func write(s syscall.Handle, data []byte) error {
	for len(data) > 0 {
		buf := syscall.WSABuf{Len: uint32(len(data)), Buf: &data[0]}
		var sent uint32
		if err := syscall.WSASend(s, &buf, 1, &sent, 0, nil, nil); err != nil {
			return err
		}
		data = data[sent:]
	}
	return nil
}

// dial opens an RFCOMM connection on a new socket. The caller closes it. The connect itself is
// blocking; it runs in a goroutine so a printer that is off fails after timeout.
func dial(sa sockaddrBTH) (syscall.Handle, error) {
	s, err := syscall.Socket(afBTH, syscall.SOCK_STREAM, bthprotoRFCOMM)
	if err != nil {
		return syscall.InvalidHandle, fmt.Errorf("bluetooth socket: %w", err)
	}
	// SO_SNDTIMEO bounds each write, so a link that dies mid-job fails fast.
	to := soSndTimeoMilli
	if err := syscall.Setsockopt(s, syscall.SOL_SOCKET, soSndTimeo, (*byte)(unsafe.Pointer(&to)), int32(unsafe.Sizeof(to))); err != nil {
		syscall.Closesocket(s)
		return syscall.InvalidHandle, err
	}

	done := make(chan error, 1)
	go func() {
		r, _, e := procConnect.Call(uintptr(s), uintptr(unsafe.Pointer(&sa)), uintptr(len(sa)))
		if r != 0 {
			done <- e
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			syscall.Closesocket(s)
			return syscall.InvalidHandle, err
		}
		return s, nil
	case <-time.After(timeout):
		syscall.Closesocket(s) // makes the pending connect return
		<-done
		return syscall.InvalidHandle, fmt.Errorf("timed out after %s", timeout)
	}
}
