//go:build !windows

package ui

import (
	"os"
	"os/signal"
	"syscall"
)

// onConsoleClose calls f when the terminal goes away (SIGHUP), the
// counterpart of closing the console window on Windows.
func onConsoleClose(f func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			f()
		case <-done:
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
