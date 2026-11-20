package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/mailmsg"
)

func assertVisibleSel(t *testing.T, m *Model) {
	t.Helper()
	_ = m.View()
	v := m.visible()
	if len(v) == 0 {
		if m.cursor != 0 || m.offset != 0 {
			t.Fatalf("empty list should reset cursor/offset, got %d/%d", m.cursor, m.offset)
		}
		return
	}
	if m.cursor < 0 || m.cursor >= len(v) {
		t.Fatalf("cursor %d out of range for %d rows", m.cursor, len(v))
	}
	if m.offset < 0 || m.offset >= len(v) {
		t.Fatalf("offset %d out of range for %d rows (no row would be drawn)", m.offset, len(v))
	}
}

func TestTOpensReaderTabLikeDoubleClick(t *testing.T) {
	m, a := testModel(t)
	var opened []string
	old := openFileFn
	openFileFn = func(p string) error { opened = append(opened, p); return nil }
	defer func() { openFileFn = old }()

	out := m.View()
	if !strings.Contains(out, "Sekmede aç") {
		t.Fatal("list key bar should show t Sekmede aç")
	}
	m.onKey(key("t"))
	if !seen(a, 3) {
		t.Fatal("t should mark the selected mail read")
	}
	if len(m.tabs) != 2 || m.tabs[m.active].read == nil || m.tabs[m.active].read.key.uid != 3 {
		t.Fatalf("t should open a Termail reader tab, tabs=%d active=%d", len(m.tabs), m.active)
	}
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("Gelen Kutusu after t must keep the mail: %v", visUIDs(m))
	}
	if e, _ := m.selected(); e.UID != 3 {
		t.Fatalf("cursor should stay on the opened mail, got %d", e.UID)
	}
	if len(opened) != 0 {
		t.Fatal("t must not open the system browser")
	}
	assertVisibleSel(t, m)
}

func TestTOnGroupHeaderOpensFirstMail(t *testing.T) {
	m := groupedModel(t)
	press(m, "G")
	m.cursor = 0
	press(m, "t")
	if len(m.tabs) != 2 || m.tabs[m.active].read == nil {
		t.Fatal("t on a group header should open the first mail in a tab")
	}
	assertVisibleSel(t, m)
}

func TestGroupHeaderDeletesAllSenderMail(t *testing.T) {
	m := groupedModel(t)
	press(m, "G")
	m.cursor = 0
	out := m.View()
	if !strings.Contains(out, "Göndereni sil") {
		t.Fatalf("key bar on a group header should show d Göndereni sil\n%s", out)
	}
	press(m, "d")
	if m.confirm == nil {
		t.Fatal("d on a group header should confirm first")
	}
	if !strings.Contains(m.confirm.text, "Google göndericisinin 2 maili silinsin mi? (e/h)") {
		t.Fatalf("confirm text: %q", m.confirm.text)
	}
	press(m, "h")
	if m.confirm != nil || len(m.accts[0].Lists["INBOX"]) != 3 {
		t.Fatal("h should cancel and leave the mails")
	}

	press(m, "d")
	press(m, "e")
	if m.confirm != nil {
		t.Fatal("e should accept")
	}
	got := visUIDs(m)
	if !sameUIDs(got, 9) {
		t.Fatalf("after confirming, only Ayşe should remain, got %v", got)
	}
	var left []uint32
	for _, e := range m.accts[0].Lists["INBOX"] {
		left = append(left, e.UID)
	}
	if !sameUIDs(left, 9) {
		t.Fatalf("INBOX after sender delete: %v", left)
	}
	assertVisibleSel(t, m)
}

func TestDeleteOnPlainRowUnchangedWhenGrouped(t *testing.T) {
	m := groupedModel(t)
	press(m, "G")
	// Ayşe is a single-mail row (not a header)
	m.cursor = 1
	e, ok := m.selected()
	if !ok || e.UID != 9 {
		t.Fatalf("want Ayşe, got %+v ok=%v", e, ok)
	}
	press(m, "d")
	if m.confirm != nil {
		t.Fatal("d on a normal row must not confirm a sender delete")
	}
	var left []uint32
	for _, e := range m.accts[0].Lists["INBOX"] {
		left = append(left, e.UID)
	}
	if !sameUIDs(left, 10, 8) {
		t.Fatalf("d should delete only Ayşe, inbox=%v", left)
	}
	v := m.visible()
	if len(v) != 1 || !isGroupRow(v[0]) {
		t.Fatalf("want the remaining Google group, got %+v", v)
	}
	assertVisibleSel(t, m)
}

func TestDeleteOnGroupChildDeletesOnlyThatMail(t *testing.T) {
	m := groupedModel(t)
	press(m, "G")
	press(m, "enter")
	press(m, "down")
	e, ok := m.selected()
	if !ok || e.UID != 10 {
		t.Fatalf("want first child, got %+v ok=%v", e, ok)
	}
	press(m, "d")
	if m.confirm != nil {
		t.Fatal("d on a child must not ask to delete the whole sender")
	}
	left := visUIDs(m)
	if !sameUIDs(left, 9, 8) {
		t.Fatalf("only the selected child should be gone, got %v", left)
	}
	assertVisibleSel(t, m)
}

func TestCursorStaysVisibleAfterToggleReadInOkunanlar(t *testing.T) {
	m, a := testModel(t)
	for _, uid := range []uint32{3, 2, 1} {
		m.setSeenLocal(0, "INBOX", uid, true)
	}
	if !seen(a, 3) || !seen(a, 1) {
		t.Fatal("setup")
	}
	m.openRead(0)
	if len(m.visible()) != 3 {
		t.Fatalf("Okunanlar should have 3, got %v", visUIDs(m))
	}
	m.cursor = 2
	press(m, "i")
	// i pins the row in this half, so the list must not jump
	if len(m.visible()) != 3 {
		t.Fatalf("i in Okunanlar should keep the row, got %v", visUIDs(m))
	}
	assertVisibleSel(t, m)

	m.stickySec = nil
	m.cursor, m.offset = 2, 2
	if len(m.visible()) != 3 {
		t.Fatalf("toggling read must keep the mail in the list, got %v", visUIDs(m))
	}
	assertVisibleSel(t, m)
}

func TestCursorClampedAfterListShrinkAndStaleOffset(t *testing.T) {
	m, _ := testModel(t)
	m.cursor, m.offset = 99, 80
	assertVisibleSel(t, m)
	if m.cursor != 2 {
		t.Fatalf("cursor should clamp to the last row, got %d", m.cursor)
	}

	// o on the last unread leaves a non-empty list; highlight must remain
	m.cursor = 2
	m.bodies.Put(bodyKey{0, "INBOX", 1}, &mailmsg.Parsed{Subject: "Bir", Text: "gövde"})
	old := openFileFn
	openFileFn = func(string) error { return nil }
	defer func() { openFileFn = old }()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	completeOpen(t, m, cmd)
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("after o on last unread: %v", visUIDs(m))
	}
	assertVisibleSel(t, m)
}

func TestCursorClampedWhenGroupCollapsesAfterOpenInBrowser(t *testing.T) {
	m := groupedModel(t)
	press(m, "G")
	press(m, "enter")
	m.cursor = 2 // last Google child (UID 8); o leaves one Google mail, group becomes a single row
	m.bodies.Put(bodyKey{0, "INBOX", 8}, &mailmsg.Parsed{Subject: "Satın alma", Text: "gövde"})
	old := openFileFn
	openFileFn = func(string) error { return nil }
	defer func() { openFileFn = old }()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	completeOpen(t, m, cmd)
	v := m.visible()
	if len(v) < 2 {
		t.Fatalf("opened mail must stay, so the group does not collapse, got %d rows", len(v))
	}
	assertVisibleSel(t, m)
}

func TestOpenInBrowserWaitsForBodyThenOpens(t *testing.T) {
	m, a := testModel(t)
	var opened []string
	old := openFileFn
	openFileFn = func(p string) error { opened = append(opened, p); return nil }
	defer func() { openFileFn = old }()

	k := bodyKey{0, "INBOX", 3}
	m.loading[k] = true // body fetch already in flight
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if seen(a, 3) {
		t.Fatal("o must not mark read until the browser opens")
	}
	if m.wantBrowser == nil || m.wantBrowser.key != k {
		t.Fatal("o should remember to open the browser when the body arrives")
	}
	if !hasToast(m, "Mail yükleniyor, tarayıcıda açılacak") {
		t.Fatal("want a loading status while the body is missing")
	}
	assertVisibleSel(t, m)
	if len(opened) != 0 {
		t.Fatal("must not open the browser before the body is here")
	}

	_, cmd = m.Update(bodyMsg{key: k, p: &mailmsg.Parsed{Subject: "Üç", Text: "gövde"}})
	completeOpen(t, m, cmd)
	if len(opened) == 0 {
		t.Fatal("browser should open once the body arrives")
	}
	if m.wantBrowser != nil {
		t.Fatal("pending browser request should be cleared")
	}
	if !hasToast(m, "Tarayıcıda açılıyor") {
		t.Fatal("want a success toast after opening")
	}
	assertVisibleSel(t, m)
}

func TestOpenInBrowserShowsErrorWhenBodyCannotLoad(t *testing.T) {
	m, _ := testModel(t)
	m.accts[0].B = nil
	press(m, "o")
	if m.wantBrowser != nil {
		t.Fatal("must not wait forever when the body cannot be loaded")
	}
	if !hasToast(m, "İleti yüklenemedi, tarayıcıda açılamadı") {
		t.Fatal("want a clear error when the browser cannot be opened")
	}
	if seen(m.accts[0], 3) {
		t.Fatal("must not mark read when the body cannot be loaded")
	}
	assertVisibleSel(t, m)
}

func TestOpenInBrowserReportsOpenError(t *testing.T) {
	m, _ := testModel(t)
	m.bodies.Put(bodyKey{0, "INBOX", 3}, &mailmsg.Parsed{Subject: "Üç", Text: "gövde"})
	old := openFileFn
	openFileFn = func(string) error { return errors.New("shell execute failed") }
	defer func() { openFileFn = old }()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	completeOpen(t, m, cmd)
	if !hasToast(m, "Tarayıcıda açılamadı") {
		t.Fatal("want a visible error when the default program fails")
	}
	if seen(m.accts[0], 3) {
		t.Fatal("a failed open must not mark the mail read")
	}
	if hasToast(m, "Tarayıcıda açılıyor") && !hasToast(m, "Tarayıcıda açılamadı") {
		t.Fatal("must not show a success toast on failure")
	}
	assertVisibleSel(t, m)
}

func TestIKeepsSelectionInGelenKutusu(t *testing.T) {
	m, _ := testModel(t)
	m.cursor = 2
	press(m, "i")
	// sticky: the mail stays in Gelen Kutusu, cursor stays put
	if len(m.visible()) != 3 {
		t.Fatalf("i in Gelen Kutusu should keep the row, got %v", visUIDs(m))
	}
	assertVisibleSel(t, m)
}

func TestHelpListsTabShortcut(t *testing.T) {
	m, _ := testModel(t)
	press(m, "?")
	out := m.View()
	if !strings.Contains(out, "Sekmede aç") {
		t.Fatal("? help should list t / Sekmede aç")
	}
}
