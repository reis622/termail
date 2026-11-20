package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/edit"
)

const altGrChars = "@#$%{[]}\\|~<>€"
const trLetters = "ğüşıöçİĞÜŞÖÇ"

// typeAltGr sends each rune the way Windows delivers AltGr input after the
// console layer (rune + Alt flag).
func typeAltGr(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true})
	}
}

func typePlain(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestAltGrAndTurkishInLogin(t *testing.T) {
	m, l := loginModelFor(t)
	typePlain(m, "ad")
	typeAltGr(m, "@")
	typePlain(m, "gmail.com"+trLetters)
	typeAltGr(m, altGrChars)
	want := "ad@gmail.com" + trLetters + altGrChars
	if got := l.email.Value(); got != want {
		t.Fatalf("login address %q, want %q", got, want)
	}
}

func TestAltGrInComposeFields(t *testing.T) {
	m, _, c := newCompose(t)
	for _, f := range []struct {
		focus int
		ed    *edit.Editor
	}{{fTo, &c.to}, {fCc, &c.cc}, {fSubject, &c.subject}, {fBody, &c.body}} {
		c.focus = f.focus
		c.applyFocus()
		f.ed.SetValue("")
		typePlain(m, "ad")
		typeAltGr(m, "@")
		typePlain(m, trLetters)
		typeAltGr(m, altGrChars)
		if got, want := f.ed.Value(), "ad@"+trLetters+altGrChars; got != want {
			t.Fatalf("field %d: %q, want %q", f.focus, got, want)
		}
	}
}

func TestAltGrInSearchAndNoShortcut(t *testing.T) {
	m := groupedModel(t)
	cur := m.cur
	typeAltGr(m, "@") // outside a field: no shortcut, nothing happens
	if m.cur != cur || m.search != nil && m.search.focused {
		t.Fatal("AltGr+@ acted as a shortcut")
	}
	press(m, "/")
	typeAltGr(m, "@")
	typePlain(m, "ğ")
	if q := m.search.in.Value(); q != "@ğ" {
		t.Fatalf("search %q", q)
	}
	// Alt+digit stays a shortcut (not typed)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true})
	if q := m.search.in.Value(); q != "@ğ" {
		t.Fatalf("Alt+1 typed into search: %q", q)
	}
}

func TestPasteAddressAllPaths(t *testing.T) {
	m, _, c := newCompose(t)
	clock := time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)
	m.opt.Now = func() time.Time { return clock }
	clip := "ad@gmail.com"
	stubClipboard(t, &clip)
	for _, f := range []struct {
		focus int
		ed    *edit.Editor
	}{{fTo, &c.to}, {fCc, &c.cc}, {fSubject, &c.subject}, {fBody, &c.body}} {
		c.focus = f.focus
		c.applyFocus()
		f.ed.SetValue("")
		clock = clock.Add(2 * time.Second)
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlV}) // our own clipboard read
		if f.ed.Value() != clip {
			t.Fatalf("Ctrl+V field %d: %q", f.focus, f.ed.Value())
		}
		f.ed.SetValue("")
		clock = clock.Add(2 * time.Second)
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(clip), Paste: true}) // bracketed paste
		if f.ed.Value() != clip {
			t.Fatalf("bracketed paste field %d: %q", f.focus, f.ed.Value())
		}
		f.ed.SetValue("")
		// Windows console paste: one key event per char; '@' with Ctrl+Alt
		// arrives (after the input patch) as a plain rune
		typePlain(m, clip)
		if f.ed.Value() != clip {
			t.Fatalf("console paste field %d: %q", f.focus, f.ed.Value())
		}
	}
	// right-click paste into the subject
	m.applySize(120, 40)
	c.subject.SetValue("")
	clock = clock.Add(2 * time.Second)
	m.View()
	x, y := find(t, m, "Konu")
	mouse(m, x+40, y, tea.MouseActionPress, tea.MouseButtonRight)
	if c.subject.Value() != clip {
		t.Fatalf("right-click: %q", c.subject.Value())
	}
	// login address: Ctrl+V and bracketed
	m2, l := loginModelFor(t)
	m2.opt.Now = m.opt.Now
	m2.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if l.email.Value() != clip {
		t.Fatalf("login Ctrl+V: %q", l.email.Value())
	}
}
