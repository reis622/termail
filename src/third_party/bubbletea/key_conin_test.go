package tea

import (
	"testing"

	"github.com/erikgeiser/coninput"
)

const (
	lctrl = coninput.LEFT_CTRL_PRESSED
	ralt  = coninput.RIGHT_ALT_PRESSED
	lalt  = coninput.LEFT_ALT_PRESSED
	shift = coninput.SHIFT_PRESSED
)

func rec(down bool, vk coninput.VirtualKeyCode, ch rune, st coninput.ControlKeyState) coninput.KeyEventRecord {
	return coninput.KeyEventRecord{KeyDown: down, RepeatCount: 1, VirtualKeyCode: vk, Char: ch, ControlKeyState: st}
}

func TestModifierOnlyEventsProduceNothing(t *testing.T) {
	for name, e := range map[string]coninput.KeyEventRecord{
		"ctrl down":       rec(true, coninput.VK_CONTROL, 0, lctrl),
		"ctrl up":         rec(false, coninput.VK_CONTROL, 0, 0),
		"altgr ctrl part": rec(true, coninput.VK_CONTROL, 0, lctrl),
		"altgr down":      rec(true, coninput.VK_MENU, 0, lctrl|ralt),
		"altgr up":        rec(false, coninput.VK_MENU, 0, lctrl),
		"rmenu":           rec(true, coninput.VK_RMENU, 0, lctrl|ralt),
		"shift down":      rec(true, coninput.VK_SHIFT, 0, shift),
		"lshift":          rec(true, coninput.VK_LSHIFT, 0, shift),
		"lwin":            rec(true, coninput.VK_LWIN, 0, 0),
		"caps":            rec(true, coninput.VK_CAPITAL, 0, 0),
		"key up of a":     rec(false, coninput.VirtualKeyCode('A'), 'a', 0),
		"unknown vk nul":  rec(true, 0xff, 0, lctrl),
		"nul no mods":     rec(true, 0xff, 0, 0),
		"control char":    rec(true, 0xff, 0x07, 0),
		"private use":     rec(true, 0xff, 0xe000, 0),
		"lone surrogate":  rec(true, 0, 0xd83d, 0),
	} {
		if msgs := keyEventMsgs(e); len(msgs) != 0 {
			t.Errorf("%s: produced %v", name, msgs)
		}
	}
}

func TestTextAndShortcutsFromRecords(t *testing.T) {
	one := func(e coninput.KeyEventRecord) KeyMsg {
		t.Helper()
		msgs := keyEventMsgs(e)
		if len(msgs) != 1 {
			t.Fatalf("%+v: %d msgs", e, len(msgs))
		}
		return msgs[0].(KeyMsg)
	}
	// AltGr+Q on Turkish Q → '@' as plain text; synthesised paste (Ctrl+Alt) too
	for _, st := range []coninput.ControlKeyState{lctrl | ralt, lctrl | lalt} {
		if k := one(rec(true, coninput.VirtualKeyCode('Q'), '@', st)); k.Type != KeyRunes || k.Alt || string(k.Runes) != "@" {
			t.Fatalf("AltGr @: %+v", k)
		}
	}
	for _, r := range "ğüşıöçİĞÜŞÖÇ€{}" {
		if k := one(rec(true, 0xde, r, 0)); string(k.Runes) != string(r) || k.Alt {
			t.Fatalf("%q: %+v", r, k)
		}
	}
	if k := one(rec(true, coninput.VirtualKeyCode('V'), 0x16, lctrl)); k.Type != KeyCtrlV {
		t.Fatalf("Ctrl+V: %v", k)
	}
	if k := one(rec(true, coninput.VirtualKeyCode('1'), '1', lalt)); k.String() != "alt+1" {
		t.Fatalf("Alt+1: %v", k)
	}
	if k := one(rec(true, coninput.VK_SPACE, ' ', 0)); string(k.Runes) != " " && k.Type != KeySpace {
		t.Fatalf("space: %v", k)
	}
	// a whole Ctrl press / release around Ctrl+V gives exactly one message
	var n int
	for _, e := range []coninput.KeyEventRecord{
		rec(true, coninput.VK_CONTROL, 0, lctrl),
		rec(true, coninput.VirtualKeyCode('V'), 0x16, lctrl),
		rec(false, coninput.VirtualKeyCode('V'), 0x16, lctrl),
		rec(false, coninput.VK_CONTROL, 0, 0),
	} {
		n += len(keyEventMsgs(e))
	}
	if n != 1 {
		t.Fatalf("Ctrl+V sequence: %d messages", n)
	}
}
