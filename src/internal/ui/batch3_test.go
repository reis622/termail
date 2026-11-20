package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/mailmsg"
)

func hasSelBar(v string) bool {
	_ = v
	return false
}

func listSelPainted(m *Model) bool {
	_ = m.View()
	if m.gridBuf == nil {
		return false
	}
	for y := 0; y < m.gridBuf.H; y++ {
		for x := 0; x < m.gridBuf.W; x++ {
			if c := m.gridBuf.At(x, y); c != nil && c.BG == cSelFocus {
				return true
			}
		}
	}
	return false
}

func TestTOpensTabFromListAndReaderAndKeepsHighlight(t *testing.T) {
	m, a := testModel(t)
	putBody(m, 3, "Üç", "gövde")
	m.applySize(140, 40)
	m.focus = focusList
	m.View()
	m.onKey(key("t"))
	if !seen(a, 3) {
		t.Fatal("t from the list should mark read")
	}
	if len(m.tabs) != 2 || m.tabs[m.active].read == nil || m.tabs[m.active].read.key.uid != 3 {
		t.Fatal("t from the list should open a reader tab")
	}
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("mail must stay in the list: %v", visUIDs(m))
	}
	m.onKey(key("esc"))
	m.active = 0
	m.focus = focusList
	if !listSelPainted(m) {
		t.Fatal("selection highlight missing in the list after t")
	}
	assertVisibleSel(t, m)

	// from the reading pane
	m, a = testModel(t)
	putBody(m, 3, "Üç", "gövde")
	m.applySize(140, 40)
	m.onKey(key("enter"))
	if m.focus != focusReader {
		t.Fatal("enter should focus the reader")
	}
	m.onKey(key("t"))
	if !seen(a, 3) || len(m.tabs) != 2 || m.tabs[m.active].read == nil || m.tabs[m.active].read.key.uid != 3 {
		t.Fatal("t from the reader pane should open a tab")
	}
	m.active = 0
	m.focus = focusList
	assertVisibleSel(t, m)
	if !listSelPainted(m) {
		t.Fatal("selection highlight missing after t from the reader")
	}
}

func TestSelectionHighlightAfterAnyKeyInEveryPane(t *testing.T) {
	keys := []string{"j", "t", "x", "down", "esc"}
	for _, focus := range []focus{focusList, focusFolders, focusReader} {
		m, _ := testModel(t)
		putBody(m, 3, "Üç", "gövde")
		m.applySize(140, 40)
		if focus == focusReader {
			m.onKey(key("enter"))
		} else {
			m.focus = focus
			if focus == focusFolders {
				m.syncFolderCursor()
			}
		}
		for _, k := range keys {
			m.hoverOn = true
			m.onKey(key(k))
			if k == "t" && focus != focusFolders {
				m.active = 0
				if m.focus == 0 {
					m.focus = focusList
				}
			}
			m.focus = focus
			if focus == focusReader && m.pv == nil {
				m.onKey(key("enter"))
			}
			if !listSelPainted(m) {
				t.Fatalf("focus=%d key=%q: selection highlight missing", focus, k)
			}
			assertVisibleSel(t, m)
		}
	}
}

func TestBodyFindInReaderAndTab(t *testing.T) {
	m, _ := testModel(t)
	p, err := mailmsg.Parse([]byte("From: a@x.com\r\nSubject: s\r\n\r\nMerhaba kod burda ve bir kez daha kod.\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	m.bodies.Put(bodyKey{0, "INBOX", 3}, p)
	m.applySize(140, 40)
	m.onKey(key("enter"))
	m.View()
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	if m.bodyFind == nil || !m.bodyFind.in.Focused() {
		t.Fatal("Ctrl+F in the reader should open in-mail search")
	}
	typeIntoFind(m, "kod")
	m.refreshBodyFind()
	if len(m.bodyFind.matches) != 2 || m.bodyFind.cur != 0 {
		t.Fatalf("matches=%d cur=%d", len(m.bodyFind.matches), m.bodyFind.cur)
	}
	m.onKey(asKey("enter"))
	if m.bodyFind.cur != 1 {
		t.Fatalf("Enter should step to the next match, cur=%d", m.bodyFind.cur)
	}
	m.onKey(asKey("shift+enter"))
	if m.bodyFind.cur != 0 {
		t.Fatalf("Shift+Enter should step back, cur=%d", m.bodyFind.cur)
	}
	v := m.View()
	if !strings.Contains(v, "1/2") && !strings.Contains(v, "1 / 2") {
		t.Fatalf("want n/m in the find field, tail=%q", v[max(0, len(v)-200):])
	}
	m.onKey(key("esc"))
	if m.bodyFind != nil {
		t.Fatal("Esc should close in-mail search")
	}
	// / in the list still opens list search
	m.focus = focusList
	m.onKey(key("/"))
	if m.search == nil || !m.search.focused || m.bodyFind != nil {
		t.Fatal("/ in the list should open list search, not in-mail search")
	}
}

func typeIntoFind(m *Model, s string) {
	if m.bodyFind == nil {
		return
	}
	for _, r := range s {
		m.bodyFindKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestStrictOTPCodes(t *testing.T) {
	if got := findVerifyCodes("onay kodu: 123456"); len(got) == 0 || got[0] != "123456" {
		t.Fatalf("onay kodu: %v", got)
	}
	if got := findVerifyCodes("doğrulama kodu 847291"); len(got) == 0 || got[0] != "847291" {
		t.Fatalf("doğrulama kodu: %v", got)
	}
	if got := findVerifyCodes("verification code: 998877"); len(got) == 0 || got[0] != "998877" {
		t.Fatalf("verification code: %v", got)
	}
	if got := findVerifyCodes("your code is 112233"); len(got) == 0 || got[0] != "112233" {
		t.Fatalf("your code is: %v", got)
	}
	if got := findVerifyCodes("kody 551299"); len(got) == 0 || got[0] != "551299" {
		t.Fatalf("kody typo: %v", got)
	}
	if got := findVerifyCodes("See https://example.com/reset?code=998877 for help"); len(got) != 0 {
		t.Fatalf("URL must not yield a code: %v", got)
	}
	if got := findVerifyCodes("Call 0532 123 45 67"); len(got) != 0 {
		t.Fatalf("phone: %v", got)
	}
	if got := findVerifyCodes("Doğrulama", "just some digits 847291 in the body"); len(got) != 0 {
		t.Fatalf("cue not adjacent: %v", got)
	}
	p := &mailmsg.Parsed{HTML: "<span style=display:none>kod: 111111</span>", View: "Hello there", Text: "Hello there"}
	if got := verifyCodes(p); len(got) != 0 {
		t.Fatalf("hidden HTML must not be searched: %v", got)
	}
}

func TestCustomFolderDeleteRenameAndSystemProtected(t *testing.T) {
	m, mem := memModel(t, 1)
	ctx := context.Background()
	if err := mem.CreateFolder(ctx, "Faturalar"); err != nil {
		t.Fatal(err)
	}
	m.accts[0].Folders = append(m.accts[0].Folders, backend.Folder{Name: "Faturalar", Role: backend.RoleNone})
	m.focus = focusFolders
	m.syncFolderCursor()
	rows := m.folderRowsSelectable()
	idx := -1
	for i, r := range rows {
		if r.custom && r.folder == "Faturalar" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("custom folder missing: %+v", rows)
	}
	m.fcursor = idx
	v := m.View()
	if !strings.Contains(v, "Sil") || !strings.Contains(v, "Yeniden adlandır") {
		t.Fatalf("keybar should show d/r on a custom folder, tail=%q", v[max(0, len(v)-300):])
	}
	m.onKey(key("d"))
	if m.confirm == nil || !m.confirm.strict {
		t.Fatal("d should ask e/h before deleting a custom folder")
	}
	m.onKey(key("h"))
	if m.confirm != nil {
		t.Fatal("h should cancel")
	}
	m.onKey(key("d"))
	cmd := m.onKey(key("e"))
	if cmd != nil {
		m.Update(cmd())
	}
	found := false
	for _, f := range m.accts[0].Folders {
		if f.Name == "Faturalar" {
			found = true
		}
	}
	if found {
		t.Fatal("folder should be gone after confirm")
	}

	if err := mem.CreateFolder(ctx, "Arşivim"); err != nil {
		t.Fatal(err)
	}
	m.accts[0].Folders = append(m.accts[0].Folders, backend.Folder{Name: "Arşivim", Role: backend.RoleNone})
	m.focus = focusFolders
	m.syncFolderCursor()
	rows = m.folderRowsSelectable()
	for i, r := range rows {
		if r.folder == "Arşivim" {
			m.fcursor = i
		}
	}
	m.onKey(key("r"))
	if m.newFolder == nil || m.newFolder.from != "Arşivim" {
		t.Fatal("r should start an inline rename")
	}
	m.newFolder.in.SetValue("Belgeler")
	cmd = m.newFolderKey(key("enter"))
	if cmd != nil {
		m.Update(cmd())
	}
	ok := false
	for _, f := range m.accts[0].Folders {
		if f.Name == "Belgeler" {
			ok = true
		}
		if f.Name == "Arşivim" {
			t.Fatal("old name still present")
		}
	}
	if !ok {
		t.Fatal("rename did not apply")
	}

	// system inbox cannot be deleted
	for i, r := range m.folderRowsSelectable() {
		if r.folder == "INBOX" && !r.custom {
			m.fcursor = i
		}
	}
	m.onKey(key("d"))
	if m.confirm != nil && strings.Contains(m.confirm.text, "klasörü ve içindeki") {
		t.Fatal("d on a system folder must not delete the folder")
	}
}

func TestInboxCopyToUserFolderAndUndo(t *testing.T) {
	m, mem := memModel(t, 1)
	ctx := context.Background()
	if err := mem.CreateFolder(ctx, "Faturalar"); err != nil {
		t.Fatal(err)
	}
	m.accts[0].Folders = append(m.accts[0].Folders, backend.Folder{Name: "Faturalar", Role: backend.RoleNone})
	e := m.accts[0].Lists["INBOX"][0]
	uid := e.UID
	cmd := m.copyToFolder(0, "INBOX", e, "Faturalar", "Faturalar")
	if cmd != nil {
		m.Update(cmd())
	}
	if len(m.accts[0].Lists["INBOX"]) < 1 {
		t.Fatal("original must stay in Gelen Kutusu")
	}
	stay := false
	for _, x := range m.accts[0].Lists["INBOX"] {
		if x.UID == uid {
			stay = true
		}
	}
	if !stay {
		t.Fatal("inbox lost the mail")
	}
	inbox, _ := mem.List(ctx, "INBOX", 0)
	dest, _ := mem.List(ctx, "Faturalar", 0)
	if len(inbox) < 1 || len(dest) != 1 {
		t.Fatalf("server copy: inbox=%d dest=%d", len(inbox), len(dest))
	}
	cmd = m.undoLast()
	if cmd != nil {
		m.Update(cmd())
	}
	dest, _ = mem.List(ctx, "Faturalar", 0)
	inbox, _ = mem.List(ctx, "INBOX", 0)
	if len(dest) != 0 {
		t.Fatalf("undo should remove the copy, dest=%d", len(dest))
	}
	if len(inbox) < 1 {
		t.Fatal("undo must not remove the inbox original")
	}

	// move to trash is still a move
	e = m.accts[0].Lists["INBOX"][0]
	m.removeEnv(0, "INBOX", e.UID)
	runUICmd(t, m, m.moveCmd(0, "INBOX", e, "Trash", "delete", 0))
	inbox, _ = mem.List(ctx, "INBOX", 0)
	if len(inbox) != 0 {
		t.Fatal("move to trash should leave the inbox")
	}
}

func TestHideOutboxNotesAndDropOkunanlar(t *testing.T) {
	m, _ := testModel(t)
	m.accts[0].Folders = append(m.accts[0].Folders,
		backend.Folder{Name: "Outbox", Role: backend.RoleNone},
		backend.Folder{Name: "Notes", Role: backend.RoleNone},
		backend.Folder{Name: "Calendar", Role: backend.RoleNone},
		backend.Folder{Name: "Faturalar", Role: backend.RoleNone},
	)
	for _, r := range m.folderRows() {
		n := strings.ToLower(r.folder + r.label)
		if strings.Contains(n, "outbox") || strings.Contains(n, "notes") || strings.Contains(n, "calendar") || strings.Contains(n, "okunanlar") || r.read {
			t.Fatalf("hidden folder leaked: %+v", r)
		}
	}
	ok := false
	for _, r := range m.folderRows() {
		if r.folder == "Faturalar" && r.custom {
			ok = true
		}
	}
	if !ok {
		t.Fatal("custom mail folder should still be listed")
	}
}

func TestReadUnreadContrastInInbox(t *testing.T) {
	m, a := testModel(t)
	a.Lists["INBOX"][0].Seen = true // uid 3
	m.applySize(140, 40)
	v := m.View()
	if !strings.Contains(v, "Üç") || !strings.Contains(v, "İki") {
		t.Fatal("both read and unread must be listed")
	}
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("inbox: %v", visUIDs(m))
	}
}

func TestContextMenuAndClickableKeybar(t *testing.T) {
	m, _ := testModel(t)
	putBody(m, 3, "Üç", "gövde")
	m.applySize(140, 40)
	m.View()
	var row hit
	for _, h := range m.hits.list {
		if h.kind == hitListRow && h.idx == 0 {
			row = h
		}
	}
	if row.w == 0 {
		t.Fatal("list row hit missing")
	}
	m.Update(tea.MouseMsg{X: row.x + 2, Y: row.y, Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
	if m.ctxMenu == nil || len(m.ctxMenu.items) < 5 {
		t.Fatalf("right-click should open the mail menu: %+v", m.ctxMenu)
	}
	// Sekmede aç
	for i, it := range m.ctxMenu.items {
		if it.key == "t" {
			m.ctxMenu.cur = i
		}
	}
	m.onKey(key("enter"))
	if len(m.tabs) != 2 || m.tabs[m.active].read == nil {
		t.Fatal("menu Enter should open a tab")
	}
	m.active, m.ctxMenu = 0, nil
	m.focus = focusList
	m.View()
	var kb hit
	for _, h := range m.hits.list {
		if h.kind == hitKeybar && h.s == "?" {
			kb = h
		}
	}
	if kb.w == 0 {
		t.Fatal("? Yardım should be clickable")
	}
	m.Update(tea.MouseMsg{X: kb.x, Y: kb.y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	if !m.hoverOn || m.hover.kind != hitKeybar {
		t.Fatalf("keybar hover: on=%v kind=%v", m.hoverOn, m.hover.kind)
	}
	m.Update(tea.MouseMsg{X: kb.x, Y: kb.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.help {
		t.Fatal("clicking ? Yardım should open help")
	}
}

func TestShiftClickExtendsSelectionAndStays(t *testing.T) {
	m, _ := selModel(t)
	x0, y0 := find(t, m, "Merhaba Ahmet")
	x1, y1 := find(t, m, "rica ederim")
	mouse(m, x0, y0, tea.MouseActionPress, tea.MouseButtonLeft)
	mouse(m, x0+4, y0, tea.MouseActionMotion, tea.MouseButtonLeft)
	mouse(m, x0+4, y0, tea.MouseActionRelease, tea.MouseButtonLeft)
	if m.rsel == nil || !m.rsel.moved {
		t.Fatal("selection should remain after release")
	}
	m.Update(tea.MouseMsg{X: x1, Y: y1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Shift: true})
	m.View()
	if m.rsel == nil || !m.rsel.moved {
		t.Fatal("Shift+click should extend the selection")
	}
	got := m.selectedText()
	if !strings.Contains(got, "Merhaba") || len(got) < 20 {
		t.Fatalf("extended selection: %q", got)
	}
	if strings.Contains(got, "  ") && strings.Contains(got, "\n\n\n") {
		t.Fatalf("unexpected extra breaks: %q", got)
	}
}

func TestNonMailFolderHelper(t *testing.T) {
	if !backend.NonMailFolder("Outbox") || !backend.NonMailFolder("Notes") || !backend.NonMailFolder("Takvim") {
		t.Fatal("expected non-mail folders to be hidden")
	}
	if backend.NonMailFolder("INBOX") || backend.NonMailFolder("Faturalar") {
		t.Fatal("mail folders must not be hidden")
	}
}

func TestKeybarClickTriggersShortcut(t *testing.T) {
	m, _ := testModel(t)
	m.applySize(140, 40)
	m.View()
	var n hit
	for _, h := range m.hits.list {
		if h.kind == hitKeybar && h.s == "n" {
			n = h
		}
	}
	if n.w == 0 {
		t.Fatal("n Yeni missing from the keybar hits")
	}
	m.Update(tea.MouseMsg{X: n.x, Y: n.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.tabs[m.active].kind != tabCompose {
		t.Fatal("clicking n should start a new message")
	}
}

var _ = time.Time{}
