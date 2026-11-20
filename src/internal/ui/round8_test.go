package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/localstore"
	"termail/internal/mailmsg"
)

func TestArrowStepsFoldersListReader(t *testing.T) {
	m, _ := testModel(t)
	m.focus = focusFolders
	m.syncFolderCursor()
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if m.focus != focusList {
		t.Fatalf("→ from the folder pane should enter the list, focus=%d", m.focus)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if m.focus != focusReader || m.pv == nil {
		t.Fatalf("→ on a selected mail should enter the reader, focus=%d pv=%v", m.focus, m.pv != nil)
	}
	if !seen(m.accts[0], 3) {
		t.Fatal("entering the mail should mark it read")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if m.focus != focusList {
		t.Fatalf("← from the reader should return to the list, focus=%d", m.focus)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if m.focus != focusFolders {
		t.Fatalf("← from the list should return to the folders, focus=%d", m.focus)
	}
}

func TestArrowWorksWithAltBitFromWindows(t *testing.T) {
	m, _ := testModel(t)
	m.focus = focusFolders
	m.syncFolderCursor()
	// conhost sometimes delivers arrows with Alt still set
	m.Update(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if m.focus != focusList {
		t.Fatalf("alt+right (Type=KeyRight) from folders should enter the list, focus=%d", m.focus)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if m.focus != focusReader {
		t.Fatalf("alt+right on a mail should enter the reader, focus=%d", m.focus)
	}
}

func TestSmallFirstPageDoesNotWipeCachedList(t *testing.T) {
	m, a := testModel(t)
	now := time.Now()
	var cached []backend.Envelope
	for i := 0; i < 40; i++ {
		cached = append(cached, backend.Envelope{
			UID: uint32(100 - i), From: "K", Subject: "önbellek", Date: now.Add(-time.Duration(i) * time.Hour),
		})
	}
	a.Lists["INBOX"] = cached
	a.Loaded["INBOX"] = true
	a.fromCache = true
	if f := a.folder("INBOX"); f != nil {
		f.Unread, f.Total = 80, 80
	}
	// a first server page of only the newest 5 (as if firstPage was tiny)
	var page []backend.Envelope
	for i := 0; i < 5; i++ {
		page = append(page, backend.Envelope{UID: uint32(100 - i), From: "K", Subject: "sunucu", Date: now.Add(-time.Duration(i) * time.Hour)})
	}
	m.onList(listMsg{acct: 0, folder: "INBOX", envs: page, page: true, total: 80, kind: "unseen", asked: now})
	if n := len(a.Lists["INBOX"]); n < 40 {
		t.Fatalf("first page wiped the cache: have %d, want at least 40", n)
	}
	have, total, ok := m.listProgress()
	if !ok || have < 40 || total < 80 {
		t.Fatalf("progress have=%d total=%d ok=%v", have, total, ok)
	}
	v := m.View()
	if !strings.Contains(v, "Yükleniyor 40/80") && !strings.Contains(v, "Yükleniyor") {
		t.Fatalf("missing loading indicator in %q", v)
	}
}

func TestInboxListsReadAndUnreadTogether(t *testing.T) {
	m, a := testModel(t)
	now := m.opt.Now()
	if now.IsZero() {
		now = time.Now()
		m.opt.Now = func() time.Time { return now }
	}
	a.Lists["INBOX"] = []backend.Envelope{
		{UID: 3, From: "Ali", Subject: "yeni", Date: now, Seen: true},
		{UID: 2, From: "Ayşe", Subject: "orta", Date: now.Add(-time.Hour), Seen: false},
		{UID: 1, From: "Can", Subject: "eski", Date: now.Add(-2 * time.Hour), Seen: true},
	}
	m.openFolder(0, "INBOX")
	if got := visUIDs(m); !sameUIDs(got, 3, 2, 1) {
		t.Fatalf("inbox should list read and unread by date: %v", got)
	}
}

func TestOpeningMailStaysInInbox(t *testing.T) {
	m, a := testModel(t)
	m.cursor = 2 // uid 1
	m.previewCmd()
	m.onKey(key("enter"))
	if !seen(a, 1) {
		t.Fatal("enter should mark the mail read")
	}
	m.onKey(key("esc"))
	got := visUIDs(m)
	if !sameUIDs(got, 3, 2, 1) {
		t.Fatalf("opened mail must stay in Gelen Kutusu: %v", got)
	}
	if e, _ := m.selected(); e.UID != 1 {
		t.Fatalf("cursor should stay on the opened mail, got %d", e.UID)
	}
}

func TestReaderNextPrevKeys(t *testing.T) {
	m, a := testModel(t)
	m.bodies.Put(bodyKey{0, "INBOX", 3}, &mailmsg.Parsed{Text: "üç"})
	m.bodies.Put(bodyKey{0, "INBOX", 2}, &mailmsg.Parsed{Text: "iki"})
	m.onKey(key("enter"))
	if m.focus != focusReader || m.pv.key.uid != 3 {
		t.Fatal("setup: entered first mail")
	}
	m.onKey(key("]"))
	if m.focus != focusReader || m.pv == nil || m.pv.key.uid != 2 {
		t.Fatalf("] should open the next mail in the reader: uid=%v focus=%d", uidOf(m), m.focus)
	}
	if !seen(a, 2) {
		t.Fatal("next mail should be marked read")
	}
	m.onKey(key("["))
	if m.pv == nil || m.pv.key.uid != 3 {
		t.Fatalf("[ should go back: uid=%v", uidOf(m))
	}
	// n stays Yeni even in the reader — we use ] / [ so there is no conflict
	n := len(m.tabs)
	m.onKey(key("n"))
	if len(m.tabs) != n+1 || m.tabs[len(m.tabs)-1].kind != tabCompose {
		t.Fatal("n in the reader should still open a new message, not step")
	}
}

func uidOf(m *Model) uint32 {
	if m.pv == nil {
		return 0
	}
	return m.pv.key.uid
}

func TestBodyPrefersDiskCache(t *testing.T) {
	m, a := testModel(t)
	st := &localstore.Store{Dir: t.TempDir()}
	m.opt.Store = st
	raw := []byte("Subject: diskten\r\n\r\nGövde önbellekten")
	if err := st.SaveBody(a.Cfg.Email, "INBOX", 3, raw); err != nil {
		t.Fatal(err)
	}
	cmd := m.loadBody(bodyKey{0, "INBOX", 3})
	if cmd == nil {
		t.Fatal("expected a disk-cache body cmd")
	}
	msg := cmd()
	bm, ok := msg.(bodyMsg)
	if !ok || bm.p == nil || !strings.Contains(bm.p.Text, "önbellekten") {
		t.Fatalf("disk body not used: %#v", msg)
	}
	if bm.err != nil {
		t.Fatalf("disk body error: %v", bm.err)
	}
}
