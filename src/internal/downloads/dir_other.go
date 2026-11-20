//go:build !windows

package downloads

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// platformDir reads XDG_DOWNLOAD_DIR from user-dirs.dirs when present.
func platformDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	f, err := os.Open(filepath.Join(cfg, "user-dirs.dirs"))
	if err != nil {
		return filepath.Join(home, "Downloads"), nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, "XDG_DOWNLOAD_DIR="); ok {
			v = strings.Trim(v, `"`)
			return strings.ReplaceAll(v, "$HOME", home), nil
		}
	}
	return filepath.Join(home, "Downloads"), nil
}
