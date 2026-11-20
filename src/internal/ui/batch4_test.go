package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
)

func altN() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}, Alt: true}
}

func TestFolderShiftDActsOnLeftPaneFolder(t *testing.T) {
	m, mem := memModel(t, 3)
	fillTrash(t, m, mem, 2)
	// Leave the middle list on the inbox, focus the left pane on Trash.
	m.folder, m.virt, m.cur, m.readView = "INBOX", viewFolder, 0, false
	m.focus = focusFolders
	found := false
	for i, r := range m.folderRowsSelectable() {
		if r.kind == rowFolder && r.folder == "Trash" {
			m.fcursor = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("trash folder row not found")
	}
	cmd := m.onKey(key("D"))
	if cmd == nil {
		t.Fatal("Shift+D from the folder pane should start emptying Trash")
	}
	m.Update(cmd())
	if m.confirm == nil {
		t.Fatal("expected empty-folder confirmation for Trash, not inbox bulk delete")
	}
	if m.confirm.title != "Hepsini kalıcı sil" {
		t.Fatalf("wrong confirm: %q", m.confirm.title)
	}
	if m.folder != "Trash" {
		t.Fatalf("should target Trash, folder=%q", m.folder)
	}
	if m.focus != focusFolders {
		t.Fatalf("focus should stay in the folder pane, got %d", m.focus)
	}

	// Inbox under the left cursor: bulk-move to trash, even if the list
	// is still showing Trash after the previous action.
	m.confirm = nil
	found = false
	for i, r := range m.folderRowsSelectable() {
		if r.kind == rowFolder && r.folder == "INBOX" {
			m.fcursor = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("inbox folder row not found")
	}
	m.folder, m.virt = "Trash", viewFolder
	m.focus = focusFolders
	m.onKey(key("D"))
	if m.confirm == nil || m.confirm.title != "Hepsini sil" {
		title := ""
		if m.confirm != nil {
			title = m.confirm.title
		}
		t.Fatalf("Shift+D on inbox should ask to move all to trash, got %q", title)
	}
	if m.focus != focusFolders {
		t.Fatalf("focus should stay in the folder pane, got %d", m.focus)
	}
}

func TestComposeHintVisibleAndClickableInEveryPane(t *testing.T) {
	m, _ := memModel(t, 3)
	m.applySize(140, 40)
	check := func(label string) {
		t.Helper()
		_ = m.View()
		var n hit
		for _, h := range m.hits.list {
			if h.kind == hitKeybar && h.s == "n" {
				n = h
				break
			}
		}
		if n.w == 0 {
			t.Fatalf("%s keybar missing clickable n Yeni, tail=%q", label, m.View()[max(0, len(m.View())-300):])
		}
		m.Update(tea.MouseMsg{X: n.x, Y: n.y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
		if !m.hoverOn || m.hover.kind != hitKeybar || m.hover.s != "n" {
			t.Fatalf("%s: n Yeni should highlight on hover", label)
		}
	}
	m.focus = focusList
	check("list")
	m.focus = focusFolders
	check("folders")
	m.focus = focusReader
	check("reader")
}

func TestAltNCreatesFolderAndPlainNDoesNot(t *testing.T) {
	m, _ := memModel(t, 1)
	m.applySize(120, 40)
	m.focus = focusFolders
	for i, r := range m.folderRowsSelectable() {
		if r.kind == rowAcct {
			m.fcursor = i
			break
		}
	}
	m.onKey(key("N"))
	if m.newFolder != nil {
		t.Fatal("plain N must not open the new-folder field")
	}
	m.onKey(key("n"))
	if m.newFolder != nil {
		t.Fatal("lowercase n should compose, not create a folder")
	}
	// leave compose if it opened
	if len(m.tabs) > 1 {
		m.closeTab(m.active)
	}
	m.focus = focusFolders
	m.onKey(altN())
	if m.newFolder == nil {
		t.Fatal("Alt+N should open the new-folder field")
	}
	m.onKey(key("esc"))
	if m.newFolder != nil {
		t.Fatal("Esc should cancel")
	}
	// Bubble Tea's asKey path too
	m.onKey(asKey("alt+n"))
	if m.newFolder == nil {
		t.Fatal("asKey alt+n should open the new-folder field")
	}
}

func TestNewFolderCancelXClick(t *testing.T) {
	m, _ := memModel(t, 1)
	m.applySize(120, 40)
	m.focus = focusFolders
	for i, r := range m.folderRowsSelectable() {
		if r.kind == rowAcct {
			m.fcursor = i
			break
		}
	}
	m.onKey(asKey("alt+n"))
	if m.newFolder == nil {
		t.Fatal("need the new-folder field")
	}
	_ = m.View()
	var x hit
	for _, h := range m.hits.list {
		if h.kind == hitFolderCancel {
			x = h
			break
		}
	}
	if x.w == 0 {
		t.Fatal("new-folder field should have a clickable ✕")
	}
	m.Update(tea.MouseMsg{X: x.x, Y: x.y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	if !m.hoverOn || m.hover.kind != hitFolderCancel {
		t.Fatal("✕ should highlight on hover")
	}
	m.Update(tea.MouseMsg{X: x.x, Y: x.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.newFolder != nil {
		t.Fatal("clicking ✕ should cancel like Esc")
	}

	// rename field uses the same bar
	var custom *backend.Folder
	for i := range m.accts[0].Folders {
		f := &m.accts[0].Folders[i]
		if f.Role == "" && f.Name != "INBOX" && f.Name != "Trash" {
			custom = f
			break
		}
	}
	if custom == nil {
		// create one in memory so rename can be tested
		m.accts[0].Folders = append(m.accts[0].Folders, backend.Folder{Name: "Faturalar"})
		custom = &m.accts[0].Folders[len(m.accts[0].Folders)-1]
	}
	m.setOpen(0, true)
	found := false
	for i, r := range m.folderRowsSelectable() {
		if r.kind == rowFolder && r.folder == custom.Name {
			m.fcursor = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("custom folder not in the list")
	}
	m.startRenameFolder(m.folderRowsSelectable()[m.fcursor])
	if m.newFolder == nil || m.newFolder.from == "" {
		t.Fatal("rename field should open")
	}
	_ = m.View()
	x = hit{}
	for _, h := range m.hits.list {
		if h.kind == hitFolderCancel {
			x = h
			break
		}
	}
	if x.w == 0 {
		t.Fatal("rename field should have a clickable ✕")
	}
	m.Update(tea.MouseMsg{X: x.x, Y: x.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.newFolder != nil {
		t.Fatal("clicking ✕ should cancel rename")
	}
}

func TestConfirmButtonsClickable(t *testing.T) {
	// Permanent delete (Enter confirms).
	m, mem := memModel(t, 0)
	fillTrash(t, m, mem, 1)
	m.folder, m.virt, m.cur = "Trash", viewFolder, 0
	m.focus = focusList
	m.cursor = 0
	deleted := false
	m.confirm = &confirmBox{title: "Kalıcı olarak sil", text: "silinsin mi? Bu işlem geri alınamaz.",
		yes: "Sil", onYes: func() tea.Cmd {
			deleted = true
			return nil
		}}
	m.applySize(120, 40)
	_ = m.View()
	var yes, no hit
	for _, h := range m.hits.list {
		switch h.kind {
		case hitConfirmYes:
			yes = h
		case hitConfirmNo:
			no = h
		}
	}
	if yes.w == 0 || no.w == 0 {
		t.Fatal("confirm popup buttons must be hit targets")
	}
	m.Update(tea.MouseMsg{X: yes.x, Y: yes.y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	if !m.hoverOn || m.hover.kind != hitConfirmYes {
		t.Fatal("confirm button should highlight on hover")
	}
	// Click outside: must not confirm.
	m.Update(tea.MouseMsg{X: 0, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.confirm == nil {
		t.Fatal("clicking outside must not confirm")
	}
	// Cancel button = Esc
	m.Update(tea.MouseMsg{X: no.x, Y: no.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.confirm != nil || deleted {
		t.Fatal("cancel button should dismiss like Esc without confirming")
	}
	m.confirm = &confirmBox{title: "Kalıcı olarak sil", text: "silinsin mi? Bu işlem geri alınamaz.",
		yes: "Sil", onYes: func() tea.Cmd { deleted = true; return nil }}
	_ = m.View()
	for _, h := range m.hits.list {
		if h.kind == hitConfirmYes {
			yes = h
		}
	}
	m.Update(tea.MouseMsg{X: yes.x, Y: yes.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !deleted || m.confirm != nil {
		t.Fatal("clicking the confirm button should be the same as Enter")
	}

	// Strict confirm (only e / click yes).
	called := false
	m.confirm = &confirmBox{strict: true, title: "Hepsini kalıcı sil", text: "hepsi silinsin mi?",
		yes: "Evet, hepsini sil", onYes: func() tea.Cmd { called = true; return nil }}
	_ = m.View()
	yes, no = hit{}, hit{}
	for _, h := range m.hits.list {
		switch h.kind {
		case hitConfirmYes:
			yes = h
		case hitConfirmNo:
			no = h
		}
	}
	if yes.w == 0 || no.w == 0 {
		t.Fatal("strict confirm buttons must be hit targets")
	}
	m.Update(tea.MouseMsg{X: 0, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if called || m.confirm == nil {
		t.Fatal("outside click must not confirm a strict dialog")
	}
	m.Update(tea.MouseMsg{X: no.x, Y: no.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if called || m.confirm != nil {
		t.Fatal("cancel should dismiss without running onYes")
	}
	m.confirm = &confirmBox{strict: true, title: "Hepsini kalıcı sil", text: "hepsi silinsin mi?",
		yes: "Evet, hepsini sil", onYes: func() tea.Cmd { called = true; return nil }}
	_ = m.View()
	for _, h := range m.hits.list {
		if h.kind == hitConfirmYes {
			yes = h
		}
	}
	m.Update(tea.MouseMsg{X: yes.x, Y: yes.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !called {
		t.Fatal("clicking the confirm button should be the same as Enter/e")
	}
}

func TestFindVerifyCodesOnayKodunuzSpacedColon(t *testing.T) {
	got := findVerifyCodes("onay kodu", "onay kodunuz : 323212")
	if len(got) != 1 || got[0] != "323212" {
		t.Fatalf("got %v, want [323212]", got)
	}
	// more adjacent Turkish / separator forms
	for _, in := range []struct{ subj, body, want string }{
		{"", "doğrulama kodunuzdur: 998877", "998877"},
		{"", "güvenlik kodu - 112233", "112233"},
		{"", "şifreniz : AB12CD", "AB12CD"},
		{"", "OTP: 445566", "445566"},
		{"", "tek kullanımlık kodunuz is 778899", "778899"},
	} {
		got = findVerifyCodes(in.subj, in.body)
		if len(got) == 0 || got[0] != in.want {
			t.Fatalf("%q / %q: got %v, want %s", in.subj, in.body, got, in.want)
		}
	}
	// negatives: phones, dates, order numbers without a keyword
	for _, in := range []string{
		"Ara 0532 123 45 67",
		"Tarih 15.11.2026 teslim",
		"Sipariş no: 123456",
		"Order 987654 without a code keyword",
		"Just a number 323212 in the body",
	} {
		if got = findVerifyCodes(in); len(got) != 0 {
			t.Fatalf("false positive on %q: %v", in, got)
		}
	}
}

func TestMiddleClickClosesExtraTabOnly(t *testing.T) {
	m, _ := memModel(t, 3)
	m.applySize(140, 40)
	e, ok := m.selected()
	if !ok {
		t.Fatal("no mail")
	}
	ai, fo := m.loc(e)
	m.openReader(ai, fo, e)
	if len(m.tabs) != 2 {
		t.Fatalf("expected a reader tab, got %d tabs", len(m.tabs))
	}
	_ = m.View()
	var main, extra hit
	for _, h := range m.hits.list {
		if h.kind == hitTab && h.idx == 0 {
			main = h
		}
		if h.kind == hitTab && h.idx == 1 {
			extra = h
		}
	}
	if main.w == 0 || extra.w == 0 {
		t.Fatal("tab titles should be hit targets")
	}
	m.Update(tea.MouseMsg{X: main.x + 1, Y: main.y, Action: tea.MouseActionPress, Button: tea.MouseButtonMiddle})
	if len(m.tabs) != 2 {
		t.Fatal("middle-click must not close the main Mail tab")
	}
	m.Update(tea.MouseMsg{X: extra.x + 1, Y: extra.y, Action: tea.MouseActionPress, Button: tea.MouseButtonMiddle})
	if len(m.tabs) != 1 {
		t.Fatalf("middle-click on an extra tab should close it, tabs=%d", len(m.tabs))
	}
	if m.active != 0 || m.tabs[0].kind != tabList {
		t.Fatal("should return to the main Mail tab")
	}
}

func TestFolderKeybarClickableAltNAndShiftD(t *testing.T) {
	m, mem := memModel(t, 3)
	fillTrash(t, m, mem, 1)
	m.applySize(160, 40)
	m.focus = focusFolders
	for i, r := range m.folderRowsSelectable() {
		if r.kind == rowFolder && r.folder == "Trash" {
			m.fcursor = i
			break
		}
	}
	_ = m.View()
	var alt, shift hit
	for _, h := range m.hits.list {
		if h.kind == hitKeybar && h.s == "alt+n" {
			alt = h
		}
		if h.kind == hitKeybar && h.s == "D" {
			shift = h
		}
	}
	if alt.w == 0 {
		t.Fatalf("folder keybar should have clickable Alt+N, tail=%q", m.View()[max(0, len(m.View())-300):])
	}
	if shift.w == 0 {
		t.Fatalf("folder keybar should have clickable Shift+D on Trash, tail=%q", m.View()[max(0, len(m.View())-300):])
	}
	m.Update(tea.MouseMsg{X: alt.x, Y: alt.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.newFolder == nil {
		t.Fatal("clicking Alt+N should open the new-folder field")
	}
}
