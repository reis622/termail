package tea

import "unicode"

// Termail patch: printable characters typed with AltGr (reported by Windows
// as Ctrl+Alt) or synthesised by the console for a paste (Windows Terminal
// and conhost send pasted text as key events carrying the layout's
// modifiers, e.g. Ctrl+Alt for '@' on a Turkish Q keyboard) are text, not
// shortcuts. Without this, '@' with Left Ctrl+Left Alt became KeyCtrlAt
// (NUL) and AltGr+Q arrived as "alt+@", which text fields ignore.
//
// altGrText reports whether a console key event with this character and
// modifier state must be delivered as plain rune input.
func altGrText(char rune, ctrl, leftAlt, rightAlt bool) bool {
	if char < 0x20 || char == 0x7f || (char >= 0x80 && char < 0xa0) || !unicode.IsPrint(char) {
		return false
	}
	alt := leftAlt || rightAlt
	if ctrl && alt {
		return true // AltGr, or Ctrl+Alt: Windows only gives a printable char when the layout makes one
	}
	if rightAlt && !ctrl {
		// right Alt alone: AltGr on layouts that report it this way; keep
		// Alt+letter/digit shortcuts working
		return !(char < 0x80 && (unicode.IsLetter(char) || unicode.IsDigit(char)))
	}
	return false
}
