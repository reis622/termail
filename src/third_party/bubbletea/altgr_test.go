package tea

import "testing"

func TestAltGrText(t *testing.T) {
	for _, r := range "@#$%{[]}\\|~<>€ğüşıöçİĞÜŞÖÇ" {
		if !altGrText(r, true, false, true) { // AltGr: Left Ctrl + Right Alt
			t.Errorf("AltGr %q not text", r)
		}
		if !altGrText(r, true, true, false) { // synthesised paste: Left Ctrl + Left Alt
			t.Errorf("Ctrl+Alt %q not text", r)
		}
	}
	if altGrText('@', false, false, false) {
		t.Error("no modifiers: normal path")
	}
	if altGrText('1', false, true, false) || altGrText('1', false, false, true) {
		t.Error("Alt+1 must stay a shortcut")
	}
	if !altGrText('@', false, false, true) {
		t.Error("right Alt alone + @ is text")
	}
	if altGrText(0, true, false, true) || altGrText(0x11, true, true, false) {
		t.Error("control characters are not text")
	}
}
