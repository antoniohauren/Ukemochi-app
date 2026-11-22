// Package tray shows a notification-area (system tray) icon on Windows, so the app can be opened,
// shown or quit without a terminal. On other systems Start does nothing.
package tray

// Options configures the tray icon.
type Options struct {
	Tooltip string // shown on hover, cut to 127 characters
	URL     string // opened by "Abrir no navegador" and a double-click
	Icon    []byte // PNG image; the default application icon is used if empty or invalid
	OnQuit  func() // called once, in its own goroutine, when "Sair" is chosen
}
