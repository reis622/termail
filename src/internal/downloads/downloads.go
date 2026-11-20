// Package downloads finds the user's Downloads folder and picks a free,
// safe file name inside it ("dosya.pdf" -> "dosya (1).pdf").
package downloads

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Dir returns the user's Downloads folder (Windows known folder, XDG on Linux,
// ~/Downloads otherwise). The folder is created if missing.
func Dir() (string, error) {
	if d := os.Getenv("TERMAIL_DOWNLOADS"); d != "" { // used by tests / power users
		return d, os.MkdirAll(d, 0o755)
	}
	d, err := platformDir()
	if err != nil || d == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", errors.Join(err, herr)
		}
		d = filepath.Join(home, "Downloads")
	}
	return d, os.MkdirAll(d, 0o755)
}

var reserved = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true}

// SanitizeName makes an attachment name safe to use as a file name on
// Windows and Unix: strips directories, forbidden characters and reserved names.
func SanitizeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 32, strings.ContainsRune(`<>:"|?*`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	name = strings.TrimRight(strings.TrimSpace(b.String()), ". ")
	if name == "" || name == "." || name == ".." {
		name = "ek"
	}
	base := name
	if i := strings.Index(base, "."); i >= 0 {
		base = base[:i]
	}
	if reserved[strings.ToUpper(base)] {
		name = "_" + name
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = name[:200-len(ext)] + ext
	}
	return name
}

// splitExt splits "rapor.tar.gz" into ("rapor.tar", ".gz") and ".bashrc" into (".bashrc", "").
func splitExt(name string) (string, string) {
	ext := filepath.Ext(name)
	if ext == name {
		return name, ""
	}
	return strings.TrimSuffix(name, ext), ext
}

// UniqueName returns name, or "name (1).ext", "name (2).ext"... - the first
// one for which exists returns false.
func UniqueName(name string, exists func(string) bool) string {
	if !exists(name) {
		return name
	}
	base, ext := splitExt(name)
	for i := 1; ; i++ {
		cand := base + " (" + strconv.Itoa(i) + ")" + ext
		if !exists(cand) {
			return cand
		}
	}
}

// Save writes data into dir using a sanitized, unique file name and returns
// the full path. The file is created exclusively so nothing is overwritten.
func Save(dir, name string, data []byte) (string, error) {
	name = SanitizeName(name)
	for attempt := 0; attempt < 5; attempt++ {
		n := UniqueName(name, func(c string) bool {
			_, err := os.Lstat(filepath.Join(dir, c))
			return err == nil
		})
		p := filepath.Join(dir, n)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			os.Remove(p)
			return "", err
		}
		return p, f.Close()
	}
	return "", os.ErrExist
}
