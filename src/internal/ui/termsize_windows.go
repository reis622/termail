//go:build windows

package ui

import (
	"os"

	"golang.org/x/sys/windows"
)

// termSize returns the visible window size of the console. Bubble Tea's
// resize events on Windows carry the screen *buffer* size, which in classic
// consoles can be thousands of rows tall (scrollback) or wider than the
// window; drawing that much makes the console scroll and corrupts the screen.
func termSize() (w, h int, ok bool) {
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		var info windows.ConsoleScreenBufferInfo
		if err := windows.GetConsoleScreenBufferInfo(windows.Handle(f.Fd()), &info); err == nil {
			w = int(info.Window.Right-info.Window.Left) + 1
			h = int(info.Window.Bottom-info.Window.Top) + 1
			if w > 0 && h > 0 {
				return w, h, true
			}
		}
	}
	return 0, 0, false
}

// prepareConsole asks the console to delay the automatic line wrap at the
// last column (like every other terminal), so a full-width row never pushes
// the cursor down by itself. Bubble Tea keeps existing mode bits.
func prepareConsole() {
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) == nil {
		_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN)
	}
}
