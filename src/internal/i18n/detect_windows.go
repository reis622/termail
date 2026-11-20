//go:build windows

package i18n

import "golang.org/x/sys/windows"

// primary language ID for Turkish (LANG_TURKISH).
const langTurkish = 0x1F

var procGetUserDefaultUILanguage = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

// Detect returns tr when the Windows UI language is Turkish, otherwise en.
func Detect() Lang {
	r, _, err := procGetUserDefaultUILanguage.Call()
	if r == 0 && err != nil && err != windows.ERROR_SUCCESS {
		return EN
	}
	if uint16(r)&0x3FF == langTurkish {
		return TR
	}
	return EN
}
