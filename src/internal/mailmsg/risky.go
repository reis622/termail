package mailmsg

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"strings"
)

// riskyExt are file types Gmail, Outlook and most mail services refuse to
// deliver (programs and scripts), even inside a zip archive.
var riskyExt = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`ade adp apk appx appxbundle bat cab chm cmd com cpl diagcab diagcfg
		diagpack dll dmg ex ex_ exe hta img ins iso isp jar jnlp js jse lib lnk mde msc msi msix
		msixbundle msp mst nsh pif ps1 scr sct shb sys vb vbe vbs vhd vxd wsc wsf wsh xll`) {
		riskyExt["."+e] = true
	}
}

// Risky reports an attachment that mail services will most likely block:
// a program/script by name, by content (Windows program header) or inside
// a zip archive.
func Risky(name string, data []byte) bool {
	if riskyExt[strings.ToLower(filepath.Ext(name))] {
		return true
	}
	if len(data) >= 64 && data[0] == 'M' && data[1] == 'Z' {
		return true // Windows program, whatever its name
	}
	if strings.EqualFold(filepath.Ext(name), ".zip") || bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		if zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err == nil {
			for _, f := range zr.File {
				if riskyExt[strings.ToLower(filepath.Ext(f.Name))] {
					return true
				}
			}
		}
	}
	return false
}
