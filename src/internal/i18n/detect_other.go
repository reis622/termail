//go:build !windows

package i18n

import (
	"os"
	"strings"
)

// Detect uses LANG / LC_ALL / LC_MESSAGES when not running on Windows.
// A Turkish locale becomes tr; anything else (including unset) is en.
func Detect() Lang {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(k)
		if v == "" || v == "C" || v == "POSIX" {
			continue
		}
		v = strings.ToLower(strings.ReplaceAll(v, "-", "_"))
		if v == "tr" || strings.HasPrefix(v, "tr_") || strings.HasPrefix(v, "tr.") {
			return TR
		}
		return EN
	}
	return EN
}
