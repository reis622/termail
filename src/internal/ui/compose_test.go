package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/localstore"
	"termail/internal/mailmsg"
)

func newCompose(t *testing.T) (*Model, *backend.Memory, *composer) {
	t.Helper()
	m, mem := memModel(t, 2)
	m.openCompose(composeNew, nil, backend.Envelope{})
	c := m.tabs[m.active].compose
	if c == nil {
		t.Fatal("no compose tab")
	}
	c.to.SetValue("arthur@gmail.com")
	c.subject.SetValue("bak bura")
	c.body.SetValue("Selam,\nşuna bir bak.")
	return m, mem, c
}

func hasToast(m *Model, text string) bool {
	for _, a := range m.toasts.Actions() {
		if strings.Contains(a.Text, text) || strings.Contains(a.Label, text) {
			return true
		}
	}
	return false
}

func TestSendClosesTabAtOnceAndReportsLater(t *testing.T) {
	m, mem, _ := newCompose(t)
	cmd := m.onKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("no background send")
	}
	if len(m.tabs) != 1 || !hasToast(m, "Gönderiliyor") {
		t.Fatal("compose tab must close immediately with a 'Gönderiliyor…' notice")
	}
	if mem.CallCount("Send") != 0 {
		t.Fatal("send ran inside the keypress")
	}
	m.Update(cmd())
	if !hasToast(m, "Gönderildi") || hasToast(m, "Gönderiliyor") || len(mem.Sent) != 1 {
		t.Fatal("no 'Gönderildi'")
	}
}

func TestFailedSendReopensDraftWithText(t *testing.T) {
	m, mem, c := newCompose(t)
	mem.FailSend = errors.New("550 5.7.1 This message was blocked because its content presents a potential security issue")
	cmd := m.onKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m.Update(cmd())
	if len(m.tabs) != 2 || m.tabs[m.active].compose != c {
		t.Fatal("failed mail not reopened")
	}
	if c.body.Value() != "Selam,\nşuna bir bak." || c.to.Value() != "arthur@gmail.com" || c.subject.Value() != "bak bura" {
		t.Fatal("text lost")
	}
	if !strings.Contains(c.status, "güvenlik") || c.sending {
		t.Fatalf("status %q", c.status)
	}
	// fixing and sending again works
	mem.FailSend = nil
	m.Update(m.onKey(tea.KeyMsg{Type: tea.KeyCtrlS})())
	if len(m.tabs) != 1 || len(mem.Sent) != 1 {
		t.Fatal("resend failed")
	}
}

func TestRiskyAttachmentAsksFirst(t *testing.T) {
	m, mem, c := newCompose(t)
	c.atts = []mailmsg.Attachment{{Name: "termail.exe", Data: append([]byte("MZ"), make([]byte, 100)...)}}
	if cmd := m.onKey(tea.KeyMsg{Type: tea.KeyCtrlS}); cmd != nil || m.confirm == nil || !m.confirm.strict {
		t.Fatal("program attachment must ask first")
	}
	if !strings.Contains(m.confirm.text, "termail.exe") || !strings.Contains(m.confirm.text, "Gmail") {
		t.Fatalf("%q", m.confirm.text)
	}
	m.onKey(key("enter")) // Enter cancels
	if len(m.tabs) != 2 || mem.CallCount("Send") != 0 {
		t.Fatal("Enter must not send")
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	cmd := m.onKey(key("e"))
	if cmd == nil || len(m.tabs) != 1 {
		t.Fatal("e must send")
	}
}

func TestQuitWaitsForSend(t *testing.T) {
	m, _, _ := newCompose(t)
	cmd := m.onKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.onKey(key("q")); m.quitting || !m.quitWait || !hasToast(m, "bitince kapanacak") {
		t.Fatal("q during a send must wait for it (and say so)")
	}
	m.Update(cmd()) // the send finishes: now it leaves by itself
	if !m.quitting {
		t.Fatal("did not leave after the send")
	}
}

func TestQuitSendWaitIsCapped(t *testing.T) {
	m, _, _ := newCompose(t)
	m.opt.Store = &localstore.Store{Dir: t.TempDir()}
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlS}) // never completes
	m.onKey(key("q"))
	m.Update(quitCapMsg{})
	if !m.quitting {
		t.Fatal("waited past the cap")
	}
	ds := m.opt.Store.TakeDrafts()
	if len(ds) != 1 || !ds[0].Sending || ds[0].To != "arthur@gmail.com" || !strings.Contains(ds[0].Body, "şuna bir bak") {
		t.Fatalf("unsent message not kept: %+v", ds)
	}
	// second q leaves at once
	m2, _, _ := newCompose(t)
	m2.onKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m2.onKey(key("q"))
	m2.onKey(key("q"))
	if !m2.quitting {
		t.Fatal("second q did not leave")
	}
}

func TestPrewarmOnCompose(t *testing.T) {
	m, mem := memModel(t, 1)
	m.prewarm(0)()
	if mem.CallCount("Prewarm") != 1 {
		t.Fatal("outgoing connection not prepared")
	}
}

func TestComposeFieldNavigation(t *testing.T) {
	m, _, c := newCompose(t)
	c.focus = fTo
	c.applyFocus()
	order := []int{fCc, fSubject, fBody, fAttach, fTo}
	for _, want := range order {
		m.onKey(tea.KeyMsg{Type: tea.KeyTab})
		if c.focus != want {
			t.Fatalf("Tab -> %d, want %d", c.focus, want)
		}
	}
	for _, want := range []int{fAttach, fBody, fSubject, fCc, fTo} {
		m.onKey(tea.KeyMsg{Type: tea.KeyShiftTab})
		if c.focus != want {
			t.Fatalf("Shift+Tab -> %d, want %d", c.focus, want)
		}
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlDown})
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlDown})
	if c.focus != fSubject {
		t.Fatal("Ctrl+Down fallback")
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlUp})
	if c.focus != fCc {
		t.Fatal("Ctrl+Up fallback")
	}
	// single-line fields: ↑ ↓
	m.onKey(key("down"))
	m.onKey(key("down"))
	if c.focus != fBody {
		t.Fatalf("Down from Konu -> %d", c.focus)
	}
	// body: Tab moves on, never inserts a tab
	before := c.body.Value()
	m.onKey(tea.KeyMsg{Type: tea.KeyTab})
	if c.focus != fAttach || c.body.Value() != before {
		t.Fatal("Tab in body inserted text or did not move")
	}
	m.onKey(key("up")) // Ekler -> body
	if c.focus != fBody {
		t.Fatal("Up from Ekler")
	}
	// cursor is on the last line: Down goes to Ekler
	c.body.CursorEnd()
	m.onKey(key("down"))
	if c.focus != fAttach {
		t.Fatal("Down on the last body line must go to Ekler")
	}
	m.onKey(key("up"))
	// Up inside the body first moves lines, then leaves to Konu at the top
	m.onKey(key("up"))
	if c.focus != fBody || c.body.Cursor().Line != 0 {
		t.Fatalf("Up moved focus too early (line %d focus %d)", c.body.Cursor().Line, c.focus)
	}
	m.onKey(key("up"))
	if c.focus != fSubject {
		t.Fatal("Up on the first body line must go to Konu")
	}
	v := m.View()
	if !strings.Contains(v, "Ctrl+S") || !strings.Contains(v, "Gönder") || !strings.Contains(v, "Tab") {
		t.Fatal("compose key bar should show Ctrl+S Gönder and Tab")
	}
}

func TestIdenticalFramesForDiffRenderer(t *testing.T) {
	m, _ := memModel(t, 30)
	a, b := m.View(), m.View()
	if a != b {
		t.Fatal("unchanged screen must render identical frames (no full repaint)")
	}
	m.Update(tickMsg{})
	if m.View() != a {
		t.Fatal("a timer tick without changes must not alter the frame")
	}
	if strings.HasSuffix(a, "\n") || strings.Count(a, "\n") != m.h-1 {
		t.Fatal("exact height without trailing newline")
	}
}
