//go:build windows

package ui

import (
	"sync"

	"golang.org/x/sys/windows"
)

const (
	ctrlCloseEvent    = 2
	ctrlLogoffEvent   = 5
	ctrlShutdownEvent = 6
)

var (
	procSetConsoleCtrlHandler = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleCtrlHandler")
	closeMu                   sync.Mutex
	closeFn                   func()
	closeCB                   uintptr
)

// onConsoleClose calls f when the console window is closed (X button), the
// user logs off or Windows shuts down. Windows ends the process a few
// seconds after such an event in any case; f saves what must survive and
// exits at once instead of letting the program wait for the server.
// Ctrl+C is not handled here (it arrives as a key in the UI).
func onConsoleClose(f func()) (stop func()) {
	closeMu.Lock()
	closeFn = f
	if closeCB == 0 {
		closeCB = windows.NewCallback(func(ctrl uint32) uintptr {
			switch ctrl {
			case ctrlCloseEvent, ctrlLogoffEvent, ctrlShutdownEvent:
				closeMu.Lock()
				fn := closeFn
				closeMu.Unlock()
				if fn != nil {
					fn() // exits the process
				}
				return 1
			}
			return 0 // Ctrl+C / Ctrl+Break: default handling
		})
	}
	cb := closeCB
	closeMu.Unlock()
	procSetConsoleCtrlHandler.Call(cb, 1)
	return func() {
		procSetConsoleCtrlHandler.Call(cb, 0)
		closeMu.Lock()
		closeFn = nil
		closeMu.Unlock()
	}
}
