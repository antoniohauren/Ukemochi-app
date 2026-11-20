//go:build !linux && !windows

package btprint

import "errors"

// shortcut: Bluetooth printing works only on Linux and Windows.
func send(string, []byte) error {
	return errors.New("bluetooth printing is only supported on Linux and Windows")
}
