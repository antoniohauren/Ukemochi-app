//go:build !linux

package btprint

import "errors"

// shortcut: Bluetooth printing works only on Linux; Windows needs AF_BTH sockets or a paired COM port.
func send(string, []byte) error {
	return errors.New("bluetooth printing is only supported on Linux")
}
