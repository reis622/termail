package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModifierJunkNeverTyped(t *testing.T) {
	m, l := loginModelFor(t)
	typePlain(m, "ad@gmail.com")
	l.setFocus(1)
	typePlain(m, "gizli")
	// what consoles send for Ctrl / AltGr / Shift alone (after the input
	// layer these never arrive, but fields must not take them either)
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{0}},
		{Type: tea.KeyRunes, Runes: []rune{0}, Alt: true},
		{Type: tea.KeyCtrlAt},
		{Type: tea.KeyRunes, Runes: []rune{0x11}},
		{Type: tea.KeyRunes, Runes: []rune{0xe000}},
		{Type: tea.KeyRunes},
	} {
		m.Update(k)
	}
	if l.pass.Value() != "gizli" {
		t.Fatalf("password got junk: %q", l.pass.Value())
	}
	l.setFocus(0)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}})
	if l.email.Value() != "ad@gmail.com" {
		t.Fatalf("address got junk: %q", l.email.Value())
	}
}

func TestAppPasswordPasteOnce(t *testing.T) {
	m, l := loginModelFor(t)
	clock := time.Date(2026, 11, 14, 19, 0, 0, 0, time.UTC)
	m.opt.Now = func() time.Time { return clock }
	typePlain(m, "ad@gmail.com")
	l.setFocus(1)
	clip := "abcd efgh ijkl mnop\r\n"
	stubClipboard(t, &clip)
	clock = clock.Add(2 * time.Second)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	// the terminal also replays the paste key by key right after: dropped
	for _, r := range "abcd efgh ijkl mnop" {
		clock = clock.Add(2 * time.Millisecond)
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if v := l.pass.Value(); v != "abcdefghijklmnop" {
		t.Fatalf("password %q (%d)", v, len(v))
	}
	if l.cleanPass() != "abcdefghijklmnop" {
		t.Fatal("cleanPass")
	}
	// other order: replay first, then Ctrl+V reaches us too
	l.pass.SetValue("")
	clock = clock.Add(5 * time.Second)
	for _, r := range "abcd efgh ijkl mnop" {
		clock = clock.Add(2 * time.Millisecond)
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if v := l.pass.Value(); v != "abcdefghijklmnop" {
		t.Fatalf("replay then Ctrl+V: %q", v)
	}
	// F2 shows / hides
	m.Update(tea.KeyMsg{Type: tea.KeyF2})
	if l.pass.Mask != 0 {
		t.Fatal("F2 should show the password")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyF2})
	if l.pass.Mask == 0 {
		t.Fatal("F2 again hides it")
	}
	// address is trimmed and cleaned
	if got := cleanAddress("  Ad\u200b@Gmail.com \n"); got != "ad@gmail.com" {
		t.Fatalf("cleanAddress %q", got)
	}
}
