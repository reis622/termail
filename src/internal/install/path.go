// Package install puts termail.exe in the user's programs folder and on the
// user's PATH ("termail kur"), and removes it again ("termail kaldır").
// Nothing needs administrator rights.
package install

import (
	"path/filepath"
	"strings"
)

// norm makes a PATH entry comparable: Windows paths are case-insensitive
// and a trailing backslash does not matter.
func norm(p string) string {
	p = strings.TrimSpace(strings.Trim(strings.TrimSpace(p), `"`))
	p = strings.TrimRight(p, `\/`)
	return strings.ToLower(p)
}

func split(path string) []string {
	var out []string
	for _, e := range strings.Split(path, ";") {
		if strings.TrimSpace(e) != "" {
			out = append(out, e)
		}
	}
	return out
}

// MergePath adds dir to a PATH value unless it is already there. Existing
// entries are kept exactly as they were (order, spelling, %VARIABLES%);
// empty entries are dropped. changed reports whether dir was added.
func MergePath(path, dir string) (merged string, changed bool) {
	entries := split(path)
	for _, e := range entries {
		if norm(e) == norm(dir) {
			return strings.Join(entries, ";"), false
		}
	}
	return strings.Join(append(entries, dir), ";"), true
}

// RemovePath removes every spelling of dir from a PATH value and keeps all
// other entries unchanged.
func RemovePath(path, dir string) (out string, changed bool) {
	var keep []string
	for _, e := range split(path) {
		if norm(e) == norm(dir) {
			changed = true
			continue
		}
		keep = append(keep, e)
	}
	return strings.Join(keep, ";"), changed
}

// Dir is where "termail kur" puts the program, given %LOCALAPPDATA%.
func Dir(localAppData string) string {
	return filepath.Join(localAppData, "Programs", "Termail")
}
