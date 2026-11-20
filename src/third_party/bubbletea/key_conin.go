package tea

import (
	"unicode"

	"github.com/erikgeiser/coninput"
)

// Termail patch: the Windows console key path, without a build tag so it
// can be tested on any system with raw KEY_EVENT_RECORDs.
//
// Rules: key-up events and modifier-only presses (Ctrl, Alt, AltGr, Shift,
// Win, Caps/Num/Scroll Lock) never produce anything; a rune key is only
// sent when its character is printable (no NUL, control, private-use or
// lone surrogate characters); AltGr / Ctrl+Alt characters are plain text
// (see altgr.go).
func keyEventMsgs(e coninput.KeyEventRecord) []Msg {
	if !e.KeyDown || modifierVK(e.VirtualKeyCode) {
		return nil
	}
	ctrl := e.ControlKeyState.Contains(coninput.LEFT_CTRL_PRESSED | coninput.RIGHT_CTRL_PRESSED)
	lalt := e.ControlKeyState.Contains(coninput.LEFT_ALT_PRESSED)
	ralt := e.ControlKeyState.Contains(coninput.RIGHT_ALT_PRESSED)
	n := int(e.RepeatCount)
	if n < 1 {
		n = 1
	}
	var msgs []Msg
	if altGrText(e.Char, ctrl, lalt, ralt) {
		for i := 0; i < n; i++ {
			msgs = append(msgs, KeyMsg{Type: KeyRunes, Runes: []rune{e.Char}})
		}
		return msgs
	}
	t := keyType(e)
	if t == KeyRunes && !printableRune(e.Char) {
		return nil
	}
	for i := 0; i < n; i++ {
		k := KeyMsg{Type: t, Alt: lalt || ralt}
		if t == KeyRunes {
			k.Runes = []rune{e.Char}
		}
		msgs = append(msgs, k)
	}
	return msgs
}

func modifierVK(vk coninput.VirtualKeyCode) bool {
	switch vk {
	case coninput.VK_SHIFT, coninput.VK_CONTROL, coninput.VK_MENU,
		coninput.VK_LSHIFT, coninput.VK_RSHIFT, coninput.VK_LCONTROL, coninput.VK_RCONTROL,
		coninput.VK_LMENU, coninput.VK_RMENU, coninput.VK_LWIN, coninput.VK_RWIN,
		coninput.VK_CAPITAL, coninput.VK_NUMLOCK, coninput.VK_SCROLL:
		return true
	}
	return false
}

// printableRune: text a field may receive.
func printableRune(r rune) bool {
	switch {
	case r < 0x20, r >= 0x7f && r < 0xa0:
		return false
	case r >= 0xd800 && r <= 0xdfff: // half of a UTF-16 pair
		return false
	case unicode.Is(unicode.Co, r): // private use
		return false
	}
	return unicode.IsPrint(r) || r == ' '
}

func keyType(e coninput.KeyEventRecord) KeyType {
	code := e.VirtualKeyCode

	shiftPressed := e.ControlKeyState.Contains(coninput.SHIFT_PRESSED)
	ctrlPressed := e.ControlKeyState.Contains(coninput.LEFT_CTRL_PRESSED | coninput.RIGHT_CTRL_PRESSED)

	switch code { //nolint:exhaustive
	case coninput.VK_RETURN:
		return KeyEnter
	case coninput.VK_BACK:
		return KeyBackspace
	case coninput.VK_TAB:
		if shiftPressed {
			return KeyShiftTab
		}
		return KeyTab
	case coninput.VK_SPACE:
		return KeyRunes // this could be KeySpace but on unix space also produces KeyRunes
	case coninput.VK_ESCAPE:
		return KeyEscape
	case coninput.VK_UP:
		switch {
		case shiftPressed && ctrlPressed:
			return KeyCtrlShiftUp
		case shiftPressed:
			return KeyShiftUp
		case ctrlPressed:
			return KeyCtrlUp
		default:
			return KeyUp
		}
	case coninput.VK_DOWN:
		switch {
		case shiftPressed && ctrlPressed:
			return KeyCtrlShiftDown
		case shiftPressed:
			return KeyShiftDown
		case ctrlPressed:
			return KeyCtrlDown
		default:
			return KeyDown
		}
	case coninput.VK_RIGHT:
		switch {
		case shiftPressed && ctrlPressed:
			return KeyCtrlShiftRight
		case shiftPressed:
			return KeyShiftRight
		case ctrlPressed:
			return KeyCtrlRight
		default:
			return KeyRight
		}
	case coninput.VK_LEFT:
		switch {
		case shiftPressed && ctrlPressed:
			return KeyCtrlShiftLeft
		case shiftPressed:
			return KeyShiftLeft
		case ctrlPressed:
			return KeyCtrlLeft
		default:
			return KeyLeft
		}
	case coninput.VK_HOME:
		switch {
		case shiftPressed && ctrlPressed:
			return KeyCtrlShiftHome
		case shiftPressed:
			return KeyShiftHome
		case ctrlPressed:
			return KeyCtrlHome
		default:
			return KeyHome
		}
	case coninput.VK_END:
		switch {
		case shiftPressed && ctrlPressed:
			return KeyCtrlShiftEnd
		case shiftPressed:
			return KeyShiftEnd
		case ctrlPressed:
			return KeyCtrlEnd
		default:
			return KeyEnd
		}
	case coninput.VK_PRIOR:
		return KeyPgUp
	case coninput.VK_NEXT:
		return KeyPgDown
	case coninput.VK_DELETE:
		return KeyDelete
	case coninput.VK_F1:
		return KeyF1
	case coninput.VK_F2:
		return KeyF2
	case coninput.VK_F3:
		return KeyF3
	case coninput.VK_F4:
		return KeyF4
	case coninput.VK_F5:
		return KeyF5
	case coninput.VK_F6:
		return KeyF6
	case coninput.VK_F7:
		return KeyF7
	case coninput.VK_F8:
		return KeyF8
	case coninput.VK_F9:
		return KeyF9
	case coninput.VK_F10:
		return KeyF10
	case coninput.VK_F11:
		return KeyF11
	case coninput.VK_F12:
		return KeyF12
	case coninput.VK_F13:
		return KeyF13
	case coninput.VK_F14:
		return KeyF14
	case coninput.VK_F15:
		return KeyF15
	case coninput.VK_F16:
		return KeyF16
	case coninput.VK_F17:
		return KeyF17
	case coninput.VK_F18:
		return KeyF18
	case coninput.VK_F19:
		return KeyF19
	case coninput.VK_F20:
		return KeyF20
	default:
		switch {
		case e.ControlKeyState.Contains(coninput.LEFT_CTRL_PRESSED) && e.ControlKeyState.Contains(coninput.RIGHT_ALT_PRESSED):
			// AltGr is pressed, then it's a rune.
			fallthrough
		case !e.ControlKeyState.Contains(coninput.LEFT_CTRL_PRESSED) && !e.ControlKeyState.Contains(coninput.RIGHT_CTRL_PRESSED):
			return KeyRunes
		}

		switch e.Char {
		case '@':
			return KeyCtrlAt
		case '\x01':
			return KeyCtrlA
		case '\x02':
			return KeyCtrlB
		case '\x03':
			return KeyCtrlC
		case '\x04':
			return KeyCtrlD
		case '\x05':
			return KeyCtrlE
		case '\x06':
			return KeyCtrlF
		case '\a':
			return KeyCtrlG
		case '\b':
			return KeyCtrlH
		case '\t':
			return KeyCtrlI
		case '\n':
			return KeyCtrlJ
		case '\v':
			return KeyCtrlK
		case '\f':
			return KeyCtrlL
		case '\r':
			return KeyCtrlM
		case '\x0e':
			return KeyCtrlN
		case '\x0f':
			return KeyCtrlO
		case '\x10':
			return KeyCtrlP
		case '\x11':
			return KeyCtrlQ
		case '\x12':
			return KeyCtrlR
		case '\x13':
			return KeyCtrlS
		case '\x14':
			return KeyCtrlT
		case '\x15':
			return KeyCtrlU
		case '\x16':
			return KeyCtrlV
		case '\x17':
			return KeyCtrlW
		case '\x18':
			return KeyCtrlX
		case '\x19':
			return KeyCtrlY
		case '\x1a':
			return KeyCtrlZ
		case '\x1b':
			return KeyCtrlOpenBracket // KeyEscape
		case '\x1c':
			return KeyCtrlBackslash
		case '\x1f':
			return KeyCtrlUnderscore
		}

		switch code { //nolint:exhaustive
		case coninput.VK_OEM_4:
			return KeyCtrlOpenBracket
		case coninput.VK_OEM_6:
			return KeyCtrlCloseBracket
		}

		return KeyRunes
	}
}
