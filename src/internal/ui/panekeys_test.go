package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/mailmsg"
)

func withOpenFile(t *testing.T) *[]string {
	t.Helper()
	var opened []string
	old := openFileFn
	openFileFn = func(p string) error { opened = append(opened, p); return nil }
	t.Cleanup(func() { openFileFn = old })
	return &opened
}

func putBody(m *Model, uid uint32, subj, text string) {
	m.bodies.Put(bodyKey{0, "INBOX", uid}, &mailmsg.Parsed{Subject: subj, Text: text})
}

func pressRunes(m *Model, s string) tea.Cmd {
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return cmd
}

func TestOOpensImmediatelyWhenBodyIsCached(t *testing.T) {
	m, a := testModel(t)
	opened := withOpenFile(t)
	putBody(m, 3, "Üç", "gövde")
	cmd := pressRunes(m, "o")
	path := filepath.Join(os.TempDir(), "Termail", "3.html")
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("cached body must write HTML immediately, before the open command: %v", err)
	}
	if seen(a, 3) {
		t.Fatal("o must not mark read until the browser opens")
	}
	completeOpen(t, m, cmd)
	if !seen(a, 3) {
		t.Fatal("o should mark the mail read once the browser opens")
	}
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("Gelen Kutusu after o must keep the mail: %v", visUIDs(m))
	}
	if len(*opened) == 0 || !strings.HasSuffix((*opened)[0], "3.html") {
		t.Fatalf("browser was not opened: %v", *opened)
	}
	assertVisibleSel(t, m)
}

func TestOOnLastUnreadRowStillOpens(t *testing.T) {
	m, a := testModel(t)
	opened := withOpenFile(t)
	putBody(m, 1, "Bir", "gövde")
	m.cursor = 2
	cmd := pressRunes(m, "o")
	completeOpen(t, m, cmd)
	if !seen(a, 1) || !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("last-row o: seen=%v list=%v", seen(a, 1), visUIDs(m))
	}
	if len(*opened) == 0 {
		t.Fatal("o on the last unread row must still open the browser")
	}
	assertVisibleSel(t, m)
}

func TestOFromReaderOpensBrowser(t *testing.T) {
	m, a := testModel(t)
	opened := withOpenFile(t)
	putBody(m, 3, "Üç", "gövde")
	press(m, "enter")
	if m.focus != focusReader {
		t.Fatal("setup: enter should focus the reader")
	}
	cmd := pressRunes(m, "o")
	completeOpen(t, m, cmd)
	if !seen(a, 3) {
		t.Fatal("o from the reader should mark read once the browser opens")
	}
	if len(*opened) == 0 {
		t.Fatal("o from the reader should open the browser")
	}
	assertVisibleSel(t, m)
}

func TestOWithoutBodyOpensWhenBodyArrivesFromListAndReader(t *testing.T) {
	for _, fromReader := range []bool{false, true} {
		m, a := testModel(t)
		opened := withOpenFile(t)
		k := bodyKey{0, "INBOX", 3}
		m.loading[k] = true
		if fromReader {
			press(m, "enter")
		}
		pressRunes(m, "o")
		if !fromReader && seen(a, 3) {
			t.Fatal("o must not mark read until the browser opens")
		}
		if m.wantBrowser == nil || m.wantBrowser.key != k {
			t.Fatal("o must remember the captured envelope until the body arrives")
		}
		if !hasToast(m, "Mail yükleniyor, tarayıcıda açılacak") {
			t.Fatal("want a loading status")
		}
		assertVisibleSel(t, m)
		_, cmd := m.Update(bodyMsg{key: k, p: &mailmsg.Parsed{Subject: "Üç", Text: "gövde"}})
		completeOpen(t, m, cmd)
		if len(*opened) == 0 {
			t.Fatalf("body-arrived must open the browser (reader=%v)", fromReader)
		}
		if m.wantBrowser != nil {
			t.Fatal("pending open should be cleared")
		}
		if !seen(a, 3) {
			t.Fatal("o should mark read once the browser opens")
		}
		if !fromReader && !sameUIDs(visUIDs(m), 3, 2, 1) {
			t.Fatalf("list o must keep the mail in Gelen Kutusu: %v", visUIDs(m))
		}
		assertVisibleSel(t, m)
	}
}

func TestTOpensTabFromListAndReaderIncludingLastRow(t *testing.T) {
	// list, first row, KeyRunes "t"
	m, a := testModel(t)
	opened := withOpenFile(t)
	putBody(m, 3, "Üç", "gövde")
	pressRunes(m, "t")
	if !seen(a, 3) {
		t.Fatal("t should mark read")
	}
	if len(m.tabs) != 2 || m.tabs[m.active].read == nil || m.tabs[m.active].read.key.uid != 3 {
		t.Fatalf("t from the list should open a reader tab, tabs=%d", len(m.tabs))
	}
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("t must keep the mail in Gelen Kutusu: %v", visUIDs(m))
	}
	if len(*opened) != 0 {
		t.Fatal("t must not open the browser")
	}
	assertVisibleSel(t, m)

	// last unread row from the list
	m, a = testModel(t)
	m.cursor = 2
	pressRunes(m, "t")
	if !seen(a, 1) || m.tabs[m.active].read == nil || m.tabs[m.active].read.key.uid != 1 {
		t.Fatal("t on the last row should open that mail in a tab")
	}
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("last-row t: %v", visUIDs(m))
	}
	assertVisibleSel(t, m)

	// from the reading pane (preview after Enter)
	m, a = testModel(t)
	press(m, "enter")
	if m.focus != focusReader {
		t.Fatal("setup")
	}
	pressRunes(m, "t")
	if !seen(a, 3) {
		t.Fatal("t from the reader should mark read")
	}
	if len(m.tabs) != 2 || m.tabs[m.active].read == nil || m.tabs[m.active].read.key.uid != 3 {
		t.Fatalf("t from the reader should open a tab, tabs=%d active=%d", len(m.tabs), m.active)
	}
	assertVisibleSel(t, m)
}

func TestShiftGWorksFromFolderAndReaderPanes(t *testing.T) {
	m := groupedModel(t)
	m.focus = focusFolders
	pressRunes(m, "G")
	if !m.grouped {
		t.Fatal("Shift+G from the folder pane should toggle grouping")
	}
	if m.focus != focusList {
		t.Fatalf("Shift+G should focus the list, focus=%d", m.focus)
	}

	m.focus = focusFolders
	pressRunes(m, "G")
	if m.grouped {
		t.Fatal("second Shift+G should turn grouping off")
	}
	if m.focus != focusList {
		t.Fatal("should focus the list again")
	}

	m, a := testModel(t)
	_ = a
	press(m, "enter")
	if m.focus != focusReader {
		t.Fatal("setup: reader focus")
	}
	pressRunes(m, "G")
	if !m.grouped {
		t.Fatal("Shift+G from the reader should toggle grouping")
	}
	if m.focus != focusList {
		t.Fatalf("Shift+G from the reader should focus the list, focus=%d", m.focus)
	}
}

func TestListActionsFromFolderPane(t *testing.T) {
	m, a := testModel(t)
	opened := withOpenFile(t)
	putBody(m, 3, "Üç", "gövde")
	m.focus = focusFolders
	cmd := pressRunes(m, "o")
	completeOpen(t, m, cmd)
	if !seen(a, 3) {
		t.Fatal("o from the folder pane should open the selected mail in the browser")
	}
	if len(*opened) == 0 {
		t.Fatal("o from the folder pane did not open the browser")
	}
	if m.focus != focusList {
		t.Fatal("o from the folder pane should focus the list")
	}

	m, a = testModel(t)
	m.focus = focusFolders
	pressRunes(m, "t")
	if !seen(a, 3) || len(m.tabs) != 2 || m.tabs[m.active].read == nil {
		t.Fatal("t from the folder pane should open a reader tab")
	}

	m, _ = testModel(t)
	m.focus = focusFolders
	pressRunes(m, "d")
	if !sameUIDs(visUIDs(m), 2, 1) {
		t.Fatalf("d from the folder pane should delete the selected mail, got %v", visUIDs(m))
	}
}

func TestUnhandledKeyDoesNotFlickerCursor(t *testing.T) {
	m, _ := testModel(t)
	press(m, "enter")
	m.freeScroll = true
	m.offset = 0
	cur, off, focus := m.cursor, m.offset, m.focus
	pressRunes(m, "j") // not bound in the reader
	if m.cursor != cur || m.offset != off || m.focus != focus {
		t.Fatalf("unhandled reader key moved cursor/offset/focus: %d/%d/%d", m.cursor, m.offset, m.focus)
	}
	if !m.freeScroll {
		t.Fatal("unhandled key must not snap the list back to the cursor (freeScroll)")
	}

	m, _ = testModel(t)
	m.focus = focusFolders
	m.freeScroll = true
	cur, off, focus = m.cursor, m.offset, m.focus
	pressRunes(m, "j")
	if m.cursor != cur || m.offset != off || m.focus != focus {
		t.Fatalf("unhandled folder key moved cursor/offset/focus: %d/%d/%d", m.cursor, m.offset, m.focus)
	}
	if !m.freeScroll {
		t.Fatal("unhandled folder key must not clear freeScroll")
	}

	assertIdle := func(t *testing.T, m *Model, where string) {
		t.Helper()
		if !m.hoverOn {
			t.Fatalf("%s: unhandled key must keep hoverOn (pointer)", where)
		}
		if !m.noRender {
			t.Fatalf("%s: unhandled key must set noRender", where)
		}
		if !m.freeScroll {
			t.Fatalf("%s: unhandled key must not clear freeScroll", where)
		}
	}

	// list pane
	m, _ = testModel(t)
	m.focus = focusList
	m.hoverOn, m.freeScroll, m.offset = true, true, 0
	cur, off, focus = m.cursor, m.offset, m.focus
	pressRunes(m, "j")
	if m.cursor != cur || m.offset != off || m.focus != focus {
		t.Fatalf("unhandled list key moved cursor/offset/focus: %d/%d/%d", m.cursor, m.offset, m.focus)
	}
	assertIdle(t, m, "list")

	// grouped list (headers and children use listKey)
	m = groupedModel(t)
	m.grouped = true
	m.focus = focusList
	m.hoverOn, m.freeScroll, m.offset = true, true, 0
	if !m.onGroupHeader() && len(m.visible()) > 0 {
		// still a list key even on a child or a single row
	}
	cur, off, focus = m.cursor, m.offset, m.focus
	pressRunes(m, "j")
	if m.cursor != cur || m.offset != off || m.focus != focus {
		t.Fatalf("unhandled group-list key moved cursor/offset/focus: %d/%d/%d", m.cursor, m.offset, m.focus)
	}
	assertIdle(t, m, "group")

	// also assert the original panes keep hover / noRender
	m, _ = testModel(t)
	press(m, "enter")
	m.hoverOn, m.freeScroll, m.offset = true, true, 0
	pressRunes(m, "j")
	assertIdle(t, m, "reader")
	m, _ = testModel(t)
	m.focus = focusFolders
	m.hoverOn, m.freeScroll = true, true
	pressRunes(m, "j")
	assertIdle(t, m, "folders")
}
