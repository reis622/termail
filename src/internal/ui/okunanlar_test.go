package ui

import (
	"strings"
	"testing"
	"time"

	"termail/internal/backend"
	"termail/internal/mailmsg"
)

func visUIDs(m *Model) []uint32 {
	var out []uint32
	for _, e := range m.visible() {
		if !isGroupRow(e) {
			out = append(out, e.UID)
		}
	}
	return out
}

func sameUIDs(got []uint32, want ...uint32) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestEnterMovesMailToOkunanlar(t *testing.T) {
	m, a := testModel(t)
	m.onKey(key("enter")) // opens Üç (3) in the reading pane
	if !seen(a, 3) || m.focus != focusReader {
		t.Fatal("Enter did not open and mark read")
	}
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("list jumped while reading: %v", visUIDs(m))
	}
	if m.pv == nil || m.pv.key.uid != 3 {
		t.Fatalf("Gelen Kutusu opened the wrong mail: pv=%v", m.pv)
	}
	m.onKey(key("esc"))
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("Gelen Kutusu after reading must keep the mail: %v", visUIDs(m))
	}
	if e, _ := m.selected(); e.UID != 3 {
		t.Fatalf("cursor should stay on the opened mail: %d", e.UID)
	}
	rows := m.folderRows()
	for _, r := range rows {
		if r.read || strings.Contains(r.label, "Okunanlar") {
			t.Fatalf("Okunanlar must be gone from the folder list, got %+v", r)
		}
	}
}

func TestOpenInBrowserLeavesAtOnce(t *testing.T) {
	m, a := testModel(t)
	m.bodies.Put(bodyKey{0, "INBOX", 3}, &mailmsg.Parsed{Subject: "Üç", Text: "gövde"})
	old := openFileFn
	openFileFn = func(string) error { return nil }
	defer func() { openFileFn = old }()
	completeOpen(t, m, m.onKey(key("o")))
	if !seen(a, 3) || !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("o: %v", visUIDs(m))
	}
	if e, _ := m.selected(); e.UID != 3 {
		t.Fatalf("cursor on %d, want the opened mail", e.UID)
	}
	if len(m.tabs) != 1 {
		t.Fatal("o should open the browser, not a Termail tab")
	}
}

func TestEnterInOkunanlarKeepsSelectedMail(t *testing.T) {
	m, _ := testModel(t)
	now := time.Now()
	for _, uid := range []uint32{3, 2, 1} {
		m.setSeenLocal(0, "INBOX", uid, true)
	}
	// keep newest-by-date first so this regression stays about Enter, not sort
	for i, e := range m.accts[0].Lists["INBOX"] {
		switch e.UID {
		case 3:
			m.accts[0].Lists["INBOX"][i].ReadAt = now
		case 2:
			m.accts[0].Lists["INBOX"][i].ReadAt = now.Add(-time.Minute)
		case 1:
			m.accts[0].Lists["INBOX"][i].ReadAt = now.Add(-2 * time.Minute)
		}
	}
	m.openRead(0)
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("setup: %v", visUIDs(m))
	}

	// Enter on the first mail must open that mail and leave it in the list.
	press(m, "enter")
	if m.focus != focusReader {
		t.Fatal("Enter should open the reading pane")
	}
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("opened mail disappeared from Okunanlar: %v", visUIDs(m))
	}
	if e, ok := m.selected(); !ok || e.UID != 3 || m.pv == nil || m.pv.key.uid != 3 {
		t.Fatalf("Enter opened the wrong mail: sel=%d pv=%v", e.UID, m.pv)
	}
	press(m, "esc")
	if m.focus != focusList || !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("after Esc: focus=%d list=%v", m.focus, visUIDs(m))
	}
	if e, _ := m.selected(); e.UID != 3 {
		t.Fatalf("cursor moved off the opened mail: %d", e.UID)
	}

	// Middle of the list: same rule.
	press(m, "down")
	press(m, "enter")
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("middle mail disappeared: %v", visUIDs(m))
	}
	if e, _ := m.selected(); e.UID != 2 || m.pv == nil || m.pv.key.uid != 2 {
		t.Fatalf("Enter on the middle mail opened the wrong one: sel=%d pv=%v", e.UID, m.pv)
	}

	// o opens the selected mail in the browser; the row stays in Okunanlar.
	press(m, "esc")
	press(m, "down")
	m.bodies.Put(bodyKey{0, "INBOX", 1}, &mailmsg.Parsed{Subject: "Bir", Text: "gövde"})
	press(m, "o")
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("o removed the mail from Okunanlar: %v", visUIDs(m))
	}
	if e, _ := m.selected(); e.UID != 1 {
		t.Fatalf("o moved the cursor: %d", e.UID)
	}
	if len(m.tabs) != 1 {
		t.Fatal("o should not open a Termail tab")
	}

	// Double-click on a list row also opens that mail, not the next one.
	m.active = 0
	m.focus = focusList
	m.cursor = 0
	m.View()
	var row hit
	for _, x := range m.hits.list {
		if x.kind == hitListRow && x.idx == 0 {
			row = x
		}
	}
	if row.w == 0 {
		t.Fatal("list row hit missing")
	}
	click(m, row.x+2, row.y)
	click(m, row.x+2, row.y)
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("double-click removed a mail: %v", visUIDs(m))
	}
	if e, _ := m.selected(); e.UID != 3 {
		t.Fatalf("double-click moved the cursor: %d", e.UID)
	}
	if tb := m.tabs[m.active]; tb.read == nil || tb.read.key.uid != 3 {
		t.Fatal("double-click opened the wrong mail")
	}
}

func TestIToggleDoesNotJump(t *testing.T) {
	m, a := testModel(t)
	m.onKey(key("i")) // read: stays under the cursor
	if !seen(a, 3) || !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("i jumped: %v", visUIDs(m))
	}
	m.onKey(key("down"))
	m.onKey(key("up"))
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatal("moving around made it jump")
	}
	m.refresh()
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("after refresh the read mail must stay: %v", visUIDs(m))
	}
	m.onKey(key("i")) // unread again
	if seen(a, 3) || !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("i unread: %v", visUIDs(m))
	}
}

func TestGroupingAndSearchInBothViews(t *testing.T) {
	m := groupedModel(t)
	m.accts[0].Lists["INBOX"][2].Seen = true // Google Play
	press(m, "G")
	// Google (10+8) is a closed group header; visUIDs skips it; Ayşe (9) is a single row.
	if !sameUIDs(visUIDs(m), 9) {
		t.Fatalf("Gelen Kutusu grouped: %v", visUIDs(m))
	}
	press(m, "/")
	typeText(m, "satin")
	if !sameUIDs(visUIDs(m), 8) {
		t.Fatalf("search: %v", visUIDs(m))
	}
}

func TestUnifiedOkunanlar(t *testing.T) {
	m, _ := twoAccounts(t)
	m.accts[1].Lists["INBOX"][0].Seen = true
	m.showUnified()
	if len(m.visible()) != 3 {
		t.Fatalf("unified inbox lists read and unread, got %d", len(m.visible()))
	}
}

func TestBulkDeleteInbox(t *testing.T) {
	m, mem := memModel(t, 3)
	m.onKey(key("D"))
	if m.confirm == nil || !m.confirm.strict ||
		!strings.Contains(m.confirm.text, "Gelen Kutusu'ndaki 3 iletinin TAMAMI silinecek. Emin misin? (e/h)") {
		t.Fatalf("confirm: %+v", m.confirm)
	}
	m.onKey(key("enter"))
	if m.confirm != nil || serverCount(t, mem, "INBOX") != 3 {
		t.Fatal("Enter must cancel")
	}
	m.onKey(key("D"))
	m.onKey(key("e"))
	if len(m.visible()) != 0 {
		t.Fatal("list not emptied at once")
	}
	drainOps(t, m)
	if serverCount(t, mem, "INBOX") != 0 || serverCount(t, mem, "Trash") != 3 {
		t.Fatal("not moved to the trash (never deleted for good)")
	}
	cmd := m.onKey(key("u"))
	if cmd == nil {
		t.Fatal("u did nothing")
	}
	m.Update(cmd())
	if serverCount(t, mem, "INBOX") != 3 || serverCount(t, mem, "Trash") != 0 {
		t.Fatal("undo did not bring them back")
	}
}

func TestBulkDeleteRollbackAndOkunanlar(t *testing.T) {
	m, mem := memModel(t, 3)
	l := m.accts[0].Lists["INBOX"]
	l[0].Seen = true
	m.onKey(key("D"))
	if m.confirm == nil || !strings.Contains(m.confirm.text, "Gelen Kutusu'ndaki 3 iletinin TAMAMI") {
		t.Fatalf("confirm: %+v", m.confirm)
	}
	mem.FailMoves(true)
	m.onKey(key("e"))
	drainOps(t, m)
	if serverCount(t, mem, "INBOX") != 3 || len(m.visible()) != 3 {
		t.Fatal("failed move not rolled back")
	}
	if m.bulk != nil {
		t.Fatal("nothing moved: no undo should remain")
	}
}

func TestBulkDeleteUnifiedListsAccounts(t *testing.T) {
	m, _ := twoAccounts(t)
	m.showUnified()
	m.onKey(key("D"))
	if m.confirm == nil || !strings.Contains(m.confirm.text, "3 iletinin TAMAMI") ||
		!strings.Contains(m.confirm.text, "ben@kisisel.com: 2") || !strings.Contains(m.confirm.text, "ben@firma.com: 1") {
		t.Fatalf("confirm: %+v", m.confirm)
	}
	m.onKey(key("h"))
	if m.confirm != nil || len(m.visible()) != 3 {
		t.Fatal("h must cancel")
	}
	_ = backend.Envelope{}
}
