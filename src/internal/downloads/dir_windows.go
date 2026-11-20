//go:build windows

package downloads

import "golang.org/x/sys/windows"

func platformDir() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Downloads, 0)
}
