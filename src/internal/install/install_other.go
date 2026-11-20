//go:build !windows

package install

import (
	"errors"
	"io"
)

var errWindowsOnly = errors.New("bu komut yalnızca Windows'ta çalışır")

// Install is only available on Windows.
func Install(io.Writer) error { return errWindowsOnly }

// Uninstall is only available on Windows.
func Uninstall(io.Writer) error { return errWindowsOnly }
