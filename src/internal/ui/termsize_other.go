//go:build !windows

package ui

import (
	"os"

	"github.com/charmbracelet/x/term"
)

func termSize() (w, h int, ok bool) {
	w, h, err := term.GetSize(os.Stdout.Fd())
	return w, h, err == nil && w > 0 && h > 0
}

func prepareConsole() {}
