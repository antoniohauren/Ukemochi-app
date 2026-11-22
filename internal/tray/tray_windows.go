package tray

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procRegisterWindowMessageW   = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	procAppendMenuW              = user32.NewProc("AppendMenuW")
	procSetMenuDefaultItem       = user32.NewProc("SetMenuDefaultItem")
	procTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	procDestroyMenu              = user32.NewProc("DestroyMenu")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procLoadIconW                = user32.NewProc("LoadIconW")
	procDestroyIcon              = user32.NewProc("DestroyIcon")
	procCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procShellNotifyIconW         = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW            = shell32.NewProc("ShellExecuteW")
	procGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
	procGetConsoleWindow         = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcessList    = kernel32.NewProc("GetConsoleProcessList")
)

const (
	wmNull        = 0x0000
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmCommand     = 0x0111
	wmUser        = 0x0400
	wmLButtonDbl  = 0x0203
	wmRButtonUp   = 0x0205
	wmTrayNotify  = wmUser + 1
	nimAdd        = 0
	nimDelete     = 2
	nifMessage    = 0x1
	nifIcon       = 0x2
	nifTip        = 0x4
	mfString      = 0x0
	mfSeparator   = 0x800
	tpmRightBtn   = 0x2
	tpmReturnCmd  = 0x100
	tpmBottomAlgn = 0x20
	swHide        = 0
	swShow        = 5
	smCxSmIcon    = 49
	smCySmIcon    = 50
	idiApp        = 32512
	lrDefault     = 0

	cmdOpen    = 1
	cmdConsole = 2
	cmdQuit    = 3
	cmdLog     = 4
)

// notifyIconData is NOTIFYICONDATAW from <shellapi.h>. Go's natural alignment matches the C layout.
type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type point struct{ X, Y int32 }

// state is the one tray icon of the process; the window procedure reads it.
var state struct {
	opts           Options
	hwnd           uintptr
	nid            notifyIconData
	console        uintptr // our own console window, or 0 if we share one (started from a terminal)
	taskbarCreated uint32  // broadcast when Explorer restarts: the icon must be added again
	quitOnce       sync.Once
}

var started bool

// Start adds the icon and runs its message loop on a locked OS thread. stop removes the icon and
// waits for the loop to end. It may be called more than once.
func Start(opts Options) (stop func(), err error) {
	if started {
		return nil, errors.New("tray already started")
	}
	started = true
	state.opts = opts
	ready := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		if err := create(); err != nil {
			ready <- err
			return
		}
		ready <- nil
		loop()
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			procPostMessageW.Call(state.hwnd, wmClose, 0, 0)
			<-done
		})
	}, nil
}

func create() error {
	hinst, _, _ := procGetModuleHandleW.Call(0)
	class := utf16("UkemochiTray")
	wc := wndClassEx{
		LpfnWndProc:   syscall.NewCallback(wndProc),
		HInstance:     hinst,
		LpszClassName: class,
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %w", err)
	}
	// A hidden top-level window, not a message-only one: those miss the TaskbarCreated broadcast.
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(utf16("Ukemochi"))),
		0, 0, 0, 0, 0, 0, 0, hinst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %w", err)
	}
	state.hwnd = hwnd
	state.console = ownConsole()
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(utf16("TaskbarCreated"))))
	state.taskbarCreated = uint32(r)

	nid := &state.nid
	nid.CbSize = uint32(unsafe.Sizeof(*nid))
	nid.HWnd = hwnd
	nid.UID = 1
	nid.UFlags = nifMessage | nifIcon | nifTip
	nid.UCallbackMessage = wmTrayNotify
	nid.HIcon = loadIcon(state.opts.Icon)
	tip, _ := syscall.UTF16FromString(state.opts.Tooltip)
	copy(nid.SzTip[:len(nid.SzTip)-1], tip)
	if r, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(nid))); r == 0 {
		procDestroyWindow.Call(hwnd)
		return fmt.Errorf("Shell_NotifyIcon: %w", err)
	}
	return nil
}

func loop() {
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0 is WM_QUIT, -1 an error
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func wndProc(hwnd, message, wparam, lparam uintptr) uintptr {
	switch uint32(message) {
	case wmTrayNotify:
		switch uint32(lparam) {
		case wmLButtonDbl:
			shellOpen(state.opts.URL)
		case wmRButtonUp:
			showMenu()
		}
		return 0
	case wmCommand:
		command(wparam & 0xffff)
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&state.nid)))
		if state.nid.HIcon != 0 {
			procDestroyIcon.Call(state.nid.HIcon)
		}
		// A hidden console would stay hidden after the app exits if another process shares it.
		if state.console != 0 {
			procShowWindow.Call(state.console, swShow)
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	if state.taskbarCreated != 0 && uint32(message) == state.taskbarCreated {
		procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&state.nid)))
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, message, wparam, lparam)
	return r
}

func showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	appendItem(menu, cmdOpen, "Abrir no navegador")
	if state.opts.LogPath != "" {
		appendItem(menu, cmdLog, "Abrir log")
	}
	if state.console != 0 {
		if consoleVisible() {
			appendItem(menu, cmdConsole, "Ocultar console")
		} else {
			appendItem(menu, cmdConsole, "Mostrar console")
		}
	}
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	appendItem(menu, cmdQuit, "Sair")
	procSetMenuDefaultItem.Call(menu, cmdOpen, 0)

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Without the foreground window, the menu would not close on a click outside it.
	procSetForegroundWindow.Call(state.hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmRightBtn|tpmReturnCmd|tpmBottomAlgn,
		uintptr(pt.X), uintptr(pt.Y), 0, state.hwnd, 0)
	procPostMessageW.Call(state.hwnd, wmNull, 0, 0)
	command(cmd)
}

func command(cmd uintptr) {
	switch cmd {
	case cmdOpen:
		shellOpen(state.opts.URL)
	case cmdLog:
		shellOpen(state.opts.LogPath)
	case cmdConsole:
		if consoleVisible() {
			procShowWindow.Call(state.console, swHide)
		} else {
			procShowWindow.Call(state.console, swShow)
			procSetForegroundWindow.Call(state.console)
		}
	case cmdQuit:
		if state.opts.OnQuit != nil {
			state.quitOnce.Do(func() { go state.opts.OnQuit() })
		}
	}
}

func appendItem(menu, id uintptr, text string) {
	procAppendMenuW.Call(menu, mfString, id, uintptr(unsafe.Pointer(utf16(text))))
}

// shellOpen opens a URL or file with its default program (the browser, or Notepad for the log).
func shellOpen(target string) {
	if target == "" {
		return
	}
	procShellExecuteW.Call(state.hwnd, uintptr(unsafe.Pointer(utf16("open"))),
		uintptr(unsafe.Pointer(utf16(target))), 0, 0, swShow)
}

// ownConsole returns the console window if this process is its only user, which is the case when
// the exe was started by a double-click. Hiding a console shared with a terminal would hide the terminal.
func ownConsole() uintptr {
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return 0
	}
	var pids [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	if n != 1 {
		return 0
	}
	return hwnd
}

func consoleVisible() bool {
	r, _, _ := procIsWindowVisible.Call(state.console)
	return r != 0
}

// loadIcon makes a small icon from PNG bytes. Windows reads PNG data directly as an icon image.
func loadIcon(png []byte) uintptr {
	if len(png) > 0 {
		cx, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
		cy, _, _ := procGetSystemMetrics.Call(smCySmIcon)
		h, _, _ := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&png[0])), uintptr(len(png)),
			1, 0x00030000, cx, cy, lrDefault)
		if h != 0 {
			return h
		}
	}
	// Shared system icon: DestroyIcon on it is a harmless no-op.
	h, _, _ := procLoadIconW.Call(0, idiApp)
	return h
}

func utf16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("")
	}
	return p
}
