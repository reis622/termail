package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/account"
	"termail/internal/backend"
	"termail/internal/mailmsg"
	"termail/internal/toast"
)

func testModel(t *testing.T) (*Model, *Acct) {
	t.Helper()
	mem := backend.NewMemory([]backend.Folder{{Name: "INBOX", Role: backend.RoleInbox}, {Name: "Trash", Role: backend.RoleTrash}})
	a := &Acct{Cfg: account.Account{Name: "Test", Email: "ben@test.local"}, B: mem,
		Folders: []backend.Folder{{Name: "INBOX", Role: backend.RoleInbox, Total: 3, Unread: 3}, {Name: "Trash", Role: backend.RoleTrash}}}
	m := New(Options{Accounts: []*Acct{a}})
	now := time.Now()
	a.Lists["INBOX"] = []backend.Envelope{
		{UID: 3, From: "Ali", FromAddr: "ali@x.com", Subject: "Üç", Date: now},
		{UID: 2, From: "Ayşe", FromAddr: "ayse@x.com", Subject: "İki", Date: now.Add(-time.Hour)},
		{UID: 1, From: "Can", FromAddr: "can@x.com", Subject: "Bir", Date: now.Add(-2 * time.Hour)},
	}
	a.Loaded["INBOX"] = true
	m.w, m.h = 140, 40
	m.cursor = 0
	m.previewCmd()
	return m, a
}

func seen(a *Acct, uid uint32) bool {
	for _, e := range a.Lists["INBOX"] {
		if e.UID == uid {
			return e.Seen
		}
	}
	return false
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestPreviewDoesNotMarkRead(t *testing.T) {
	m, a := testModel(t)
	if m.pv == nil || m.pv.key.uid != 3 {
		t.Fatal("reading pane does not follow the cursor")
	}
	m.onKey(key("down"))
	m.onKey(key("down"))
	if seen(a, 3) || seen(a, 2) || seen(a, 1) {
		t.Fatal("moving through the list marked mail as read")
	}
	if m.pv.key.uid != 1 || m.focus != focusList {
		t.Fatalf("pane on %d focus %d", m.pv.key.uid, m.focus)
	}
}

func TestPreviewNeverMarksRead(t *testing.T) {
	m, a := testModel(t)
	m.toasts.AddMail(toast.Mail{Account: 0, Folder: "INBOX", UID: 3, From: "Ali"})
	k3 := bodyKey{0, "INBOX", 3}
	// highlighted far longer than the old 3 s: the stale timer message is a no-op
	m.opt.Now = func() time.Time { return time.Now().Add(10 * time.Second) }
	m.Update(seenTimerMsg{key: k3, seq: m.selSeq})
	if seen(a, 3) || !m.toasts.HasMail() {
		t.Fatal("previewing a mail marked it read")
	}
	if v := m.visible(); len(v) != 3 {
		t.Fatalf("Gelen Kutusu lost a previewed mail: %d", len(v))
	}
}

func TestStaleBodyLoadIgnored(t *testing.T) {
	m, _ := testModel(t)
	k3 := bodyKey{0, "INBOX", 3}
	old := m.selSeq
	m.onKey(key("down"))
	if _, cmd := m.Update(bodyLoadMsg{key: k3, seq: old}); cmd != nil || m.loading[k3] {
		t.Fatal("a fetch was started for a mail the cursor already left")
	}
	k2 := bodyKey{0, "INBOX", 2}
	if _, cmd := m.Update(bodyLoadMsg{key: k2, seq: m.selSeq}); cmd == nil || !m.loading[k2] {
		t.Fatal("settled cursor did not start the body fetch")
	}
}

func TestStaleBodyResponseOnlyCached(t *testing.T) {
	m, _ := testModel(t)
	m.onKey(key("down"))
	cur := m.pv
	cur.scroll = 4
	k3 := bodyKey{0, "INBOX", 3}
	m.loading[k3] = true
	m.Update(bodyMsg{key: k3, p: &mailmsg.Parsed{Text: "eski yanıt"}})
	if m.pv != cur || m.pv.key.uid != 2 || m.pv.scroll != 4 {
		t.Fatal("late body for a previous mail disturbed the reading pane")
	}
	if m.body(k3) == nil || m.loading[k3] {
		t.Fatal("late body was not cached")
	}
	if !strings.Contains(m.View(), "Yükleniyor…") {
		t.Fatal("pane for the highlighted (not yet loaded) mail should show the placeholder")
	}
}

func TestEnterReaderFocusAndReply(t *testing.T) {
	m, a := testModel(t)
	k3 := bodyKey{0, "INBOX", 3}
	m.bodies.Put(k3, &mailmsg.Parsed{Text: strings.Repeat("satır\n", 200), Subject: "Üç"})
	m.onKey(key("enter"))
	if m.focus != focusReader || !seen(a, 3) {
		t.Fatalf("Enter: focus=%d seen=%v", m.focus, seen(a, 3))
	}
	_ = m.View() // lays out the pane (page height)
	m.onKey(key("down"))
	m.onKey(key("down"))
	if m.pv.scroll != 2 || m.cursor != 0 {
		t.Fatalf("↓ inside the mail should scroll, scroll=%d cursor=%d", m.pv.scroll, m.cursor)
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.pv.scroll <= 2 {
		t.Fatal("PgDn did not scroll")
	}
	m.onKey(key("enter"))
	if len(m.tabs) != 2 || m.tabs[1].kind != tabCompose || m.tabs[1].compose.mode != composeReply {
		t.Fatal("Enter inside the mail did not open a reply tab")
	}
	m.active = 0
	m.onKey(key("esc"))
	if m.focus != focusList {
		t.Fatal("Esc did not return to the list")
	}
}

func TestRightOpensMailLikeEnter(t *testing.T) {
	m, a := testModel(t)
	m.onKey(key("right"))
	if m.focus != focusReader || !seen(a, 3) || m.pv == nil || m.pv.key.uid != 3 {
		t.Fatalf("→ should open the selected mail like Enter: focus=%d seen=%v", m.focus, seen(a, 3))
	}
}

func TestBodyCachePinsOpenTabs(t *testing.T) {
	m, _ := testModel(t)
	if e, ok := m.selected(); ok {
		ai, fo := m.loc(e)
		m.openReader(ai, fo, e)
	}
	k3 := bodyKey{0, "INBOX", 3}
	m.bodies.Put(k3, &mailmsg.Parsed{Text: "açık sekme"})
	m.active = 0
	m.onKey(key("down")) // the pane moves on; only the tab keeps uid 3
	for i := 0; i < bodyCacheCap+10; i++ {
		m.bodies.Put(bodyKey{0, "INBOX", uint32(100 + i)}, &mailmsg.Parsed{})
	}
	if m.body(k3) == nil {
		t.Fatal("body of an open tab was evicted")
	}
}
