//go:build !windows

package ui

import (
	"os/exec"
	"runtime"
	"time"
)

// openFile hands a file to its default program; the path is one argument.
// Start() alone would ignore a non-zero exit (a missing handler, a fake
// xdg-open). We wait briefly for that exit; if the process is still running
// it has typically handed off to the browser and we treat that as success.
func openFile(path string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, path)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		return nil
	}
}
