//go:build !windows

package tray

// Start does nothing: the tray icon exists only on Windows.
func Start(Options) (stop func(), err error) {
	return func() {}, nil
}
