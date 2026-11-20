//go:build windows

package ui

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// openFile hands a file to its default Windows program (ShellExecute "open").
// A return value of 32 or less is an error (the x/sys wrapper already maps
// that to a Go error); we surface it so the UI can show a toast and skip
// marking the mail read.
func openFile(path string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	dir, err := windows.UTF16PtrFromString(filepath.Dir(path))
	if err != nil {
		return err
	}
	if err := windows.ShellExecute(0, verb, file, nil, dir, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("%v", err)
	}
	return nil
}
