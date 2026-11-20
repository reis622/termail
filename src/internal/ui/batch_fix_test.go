package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/mailmsg"
	"termail/internal/toast"
	"termail/internal/undo"
)

func TestDeleteFromSavedDoesNotErrorOrReturn(t *testing.T) {
	m, mem := memModel(t, 3)
	m.cursor = 0
	m.onKey(key("k"))
	if savedCount(m.accts) != 1 {
		t.Fatal("setup: not saved")
	}
	m.showSaved()
	if len(m.visible()) != 1 {
		t.Fatalf("Kaydedilenler: %v", subjects(m.visible()))
	}
	m.onKey(key("enter"))
	m.onKey(key("d"))
	if savedCount(m.accts) != 0 {
		t.Fatalf("deleted flagged mail reappeared in Kaydedilenler: %v", subjects(mergeSaved(m.accts)))
	}
	drainOps(t, m)
	if savedCount(m.accts) != 0 {
		t.Fatal("Kaydedilenler not empty after server move")
	}
	if serverCount(t, mem, "INBOX") != 2 || serverCount(t, mem, "Trash") != 1 {
		t.Fatal("mail was not moved to trash")
	}
	for _, x := range m.toasts.Actions() {
		if x.Kind == toast.Error {
			t.Fatalf("error toast after delete from Kaydedilenler: %+v", x)
		}
	}
	for _, msg := range m.bodyErr {
		if strings.Contains(msg, "Bir sorun oluştu") {
			t.Fatalf("reader error after delete: %q", msg)
		}
	}
}

func TestUnifiedOkunanlarAggregatesAllAccounts(t *testing.T) {
	m, _ := twoAccounts(t)
	now := m.opt.Now()
	m.accts[0].Lists["INBOX"] = []backend.Envelope{
		{UID: 1, Acct: 0, Folder: "INBOX", Subject: "eski", Seen: true, ReadAt: now.Add(-time.Hour)},
	}
	m.accts[1].Lists["INBOX"] = []backend.Envelope{
		{UID: 7, Acct: 1, Folder: "INBOX", Subject: "yeni", Seen: true, ReadAt: now},
	}
	m.accts[0].Loaded["INBOX"] = true
	m.accts[1].Loaded["INBOX"] = true
	m.showVirtualView(viewUnified, false)
	got := subjects(m.visible())
	if len(got) != 2 || (got[0] != "yeni" && got[1] != "yeni") || (got[0] != "eski" && got[1] != "eski") {
		t.Fatalf("unified inbox should merge accounts: %v", got)
	}
}

func TestOAndTAndEnterMarkReadForOkunanlar(t *testing.T) {
	m, a := testModel(t)
	m.bodies.Put(bodyKey{0, "INBOX", 3}, &mailmsg.Parsed{Subject: "Üç", Text: "gövde"})
	old := openFileFn
	openFileFn = func(string) error { return nil }
	defer func() { openFileFn = old }()
	completeOpen(t, m, m.onKey(key("o")))
	if !seen(a, 3) {
		t.Fatal("o did not mark read")
	}
	selectUID(t, m, 2)
	m.onKey(key("t"))
	if !seen(a, 2) {
		t.Fatal("t did not mark read")
	}
	m.onKey(key("esc"))
	m.active = 0
	m.focus = focusList
	selectUID(t, m, 1)
	m.onKey(key("enter"))
	if !seen(a, 1) {
		t.Fatal("enter did not mark read")
	}
	m.showVirtualView(viewUnified, false)
	if !sameUIDs(visUIDs(m), 1, 2, 3) && !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("opened mails must stay in the inbox list: %v", visUIDs(m))
	}
}

func TestFindVerifyCodes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Merhaba, doğrulama kodu: 847291\nİyi günler", "847291"},
		{"Your verification code is 12-34-56.", "123456"},
		{"Onay kodu 9384\n\n9384", "9384"},
		{"OTP: AB12CD", "AB12CD"},
		{"No code here, just a year 2026 and a phone 5551234", ""},
	}
	for _, c := range cases {
		got := findVerifyCodes(c.in)
		if c.want == "" {
			if len(got) != 0 {
				t.Fatalf("%q: got %v, want none", c.in, got)
			}
			continue
		}
		if len(got) == 0 || got[0] != c.want {
			t.Fatalf("%q: got %v, want %s", c.in, got, c.want)
		}
	}
}

func TestOTPShownInAttTabAndCopies(t *testing.T) {
	m, _ := testModel(t)
	p := &mailmsg.Parsed{Subject: "Kod", Text: "Giriş doğrulama kodu: 551299\nTeşekkürler"}
	m.bodies.Put(bodyKey{0, "INBOX", 3}, p)
	m.applySize(140, 40)
	selectUID(t, m, 3)
	m.onKey(key("enter"))
	if m.pv == nil {
		t.Fatal("enter did not open the reading pane")
	}
	m.pv.gotoAttachments(m)
	r := m.activeReader()
	if r == nil {
		r = m.pv
	}
	if !r.attMode {
		t.Fatal("Ekler tab did not open")
	}
	v := m.View()
	if !strings.Contains(v, "551299") || !strings.Contains(v, "Doğrulama kodu") {
		t.Fatalf("Ekler tab did not show the code clearly:\n%s", v)
	}
	var copied []string
	old := copyTextFn
	copyTextFn = func(s string) { copied = append(copied, s) }
	defer func() { copyTextFn = old }()
	r.openItem(m, p, tabItems(p)[0])
	if len(copied) != 1 || copied[0] != "551299" {
		t.Fatalf("code not copied: %v", copied)
	}
	if !hasToast(m, "Kod kopyalandı") {
		t.Fatalf("no copy toast: %+v", m.toasts.Actions())
	}
}

func TestEOpensAttachmentsNeverArchives(t *testing.T) {
	m, mem := memModel(t, 3)
	m.accts[0].Folders = append(m.accts[0].Folders, backend.Folder{Name: "Archive", Role: backend.RoleArchive})
	uid := m.accts[0].Lists["INBOX"][0].UID
	m.bodies.Put(bodyKey{0, "INBOX", uid}, &mailmsg.Parsed{Subject: "x", Text: "gövde", Attachments: []mailmsg.Attachment{{Name: "a.pdf", ContentType: "application/pdf"}}})
	n := len(m.accts[0].Lists["INBOX"])
	m.onKey(key("e"))
	r := m.activeReader()
	if r == nil || !r.attMode {
		t.Fatal("e on the list must open Ekler, not archive")
	}
	if len(m.accts[0].Lists["INBOX"]) != n || serverCount(t, mem, "INBOX") != n {
		t.Fatal("e archived the mail")
	}
}

func TestNewMailToastClickOpensChosenMail(t *testing.T) {
	m, _ := memModel(t, 4)
	m.applySize(120, 40)
	now := time.Now()
	list := m.accts[0].Lists["INBOX"]
	// newest-first in the toast: last AddMail
	m.toasts.AddMail(toast.Mail{Account: 0, Folder: "INBOX", UID: list[2].UID, From: "Ali", Subject: "birinci", At: now})
	m.toasts.AddMail(toast.Mail{Account: 0, Folder: "INBOX", UID: list[1].UID, From: "Ayşe", Subject: "ikinci", At: now.Add(time.Second)})
	m.toasts.AddMail(toast.Mail{Account: 0, Folder: "INBOX", UID: list[0].UID, From: "Can", Subject: "üçüncü", At: now.Add(2 * time.Second)})
	m.View()
	var item *hit
	for _, h := range m.hits.list {
		if h.kind == hitMailToastItem && h.idx == 1 { // Can, Ayşe, Ali — row 1 is Ayşe
			hh := h
			item = &hh
			break
		}
	}
	if item == nil {
		t.Fatalf("no clickable row for the second new mail, view=\n%s", m.View())
	}
	m.Update(tea.MouseMsg{X: item.x + 1, Y: item.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.Update(tea.MouseMsg{X: item.x + 1, Y: item.y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	left := m.toasts.Mails()
	if len(left) != 2 {
		t.Fatalf("opening one mail dismissed the whole popup: %d left", len(left))
	}
	want := list[1].UID
	for _, x := range left {
		if x.UID == want {
			t.Fatal("opened mail still on the toast")
		}
	}
}

func TestAccountChipClickSwitches(t *testing.T) {
	m, _ := twoAccounts(t)
	m.applySize(140, 30)
	clickChip := func(idx int) {
		t.Helper()
		m.View()
		for _, h := range m.hits.list {
			if h.kind == hitAcctChip && h.idx == idx {
				m.Update(tea.MouseMsg{X: h.x + 1, Y: h.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
				m.Update(tea.MouseMsg{X: h.x + 1, Y: h.y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
				return
			}
		}
		t.Fatalf("no chip hit idx=%d, view header=%q", idx, m.View()[:min(200, len(m.View()))])
	}
	clickChip(-1)
	if m.virt != viewUnified {
		t.Fatal("F1 chip did not open Tüm Gelen Kutuları")
	}
	clickChip(1)
	if m.virt != viewFolder || m.cur != 1 {
		t.Fatalf("F3 chip did not switch account: virt=%d cur=%d", m.virt, m.cur)
	}
	clickChip(0)
	if m.virt != viewFolder || m.cur != 0 {
		t.Fatalf("F2 chip did not switch account: virt=%d cur=%d", m.virt, m.cur)
	}
}

func TestUndoDeleteReturnsToOriginalPlace(t *testing.T) {
	m, mem := memModel(t, 5)
	m.cursor = 2
	e := m.accts[0].Lists["INBOX"][2]
	before := subjects(m.accts[0].Lists["INBOX"])
	m.onKey(key("d"))
	drainOps(t, m)
	m.cursor = 0
	runUICmd(t, m, m.onKey(key("u")))
	after := subjects(m.accts[0].Lists["INBOX"])
	if len(after) != len(before) {
		t.Fatalf("undo did not restore the list: %v -> %v", before, after)
	}
	for i := range before {
		if after[i] != before[i] {
			t.Fatalf("not the original order: %v -> %v", before, after)
		}
	}
	if sel, _ := m.selected(); sel.MessageID != e.MessageID && sel.Subject != e.Subject {
		t.Fatalf("cursor not on restored mail: got %+v want %+v", sel, e)
	}
	if serverCount(t, mem, "INBOX") != 5 || serverCount(t, mem, "Trash") != 0 {
		t.Fatal("server undo failed")
	}
	if hasToast(m, "Geri alınacak bir şey yok") {
		t.Fatal("undo said nothing to undo")
	}
}

func TestUndoArchiveMoveAndFlags(t *testing.T) {
	m, mem := memModel(t, 4)
	m.accts[0].Folders = append(m.accts[0].Folders, backend.Folder{Name: "Archive", Role: backend.RoleArchive})
	e := m.accts[0].Lists["INBOX"][0]
	runUICmd(t, m, m.archiveMsg(0, "INBOX", e))
	if serverCount(t, mem, "INBOX") != 3 {
		t.Fatal("archive did not move")
	}
	runUICmd(t, m, m.undoLast())
	if serverCount(t, mem, "INBOX") != 4 {
		t.Fatal("undo archive failed")
	}
	if hasToast(m, "Geri alınacak bir şey yok") {
		t.Fatal("undo archive: empty stack")
	}

	e = m.accts[0].Lists["INBOX"][0]
	m.removeEnv(0, "INBOX", e.UID)
	id := m.undo.Push(undo.Entry{Op: undo.OpMove, Account: 0, From: "INBOX", Trash: "Spam", OrigUID: e.UID, MessageID: e.MessageID, Subject: e.Subject, Data: e})
	runUICmd(t, m, m.moveCmd(0, "INBOX", e, "Spam", "move", id))
	runUICmd(t, m, m.undoLast())
	if serverCount(t, mem, "INBOX") != 4 {
		t.Fatal("undo move failed")
	}

	m.virt, m.readView, m.folder, m.cur = viewFolder, false, "INBOX", 0
	m.focus, m.cursor, m.confirm, m.picker, m.help = focusList, 0, nil, nil, false
	if m.pv != nil {
		m.pv.attMode = false
	}
	m.clampCursor()
	e, ok := m.selected()
	if !ok {
		t.Fatalf("no mail selected: visible=%v", subjects(m.visible()))
	}
	m.toggleSaved(0, "INBOX", e)
	drainOps(t, m)
	e, ok = m.selected()
	if !ok || !e.Flagged {
		t.Fatalf("k did not save: selected=%+v visible=%v saved=%d", e, subjects(m.visible()), savedCount(m.accts))
	}
	m.undoLast()
	drainOps(t, m)
	if e, _ = m.selected(); e.Flagged {
		t.Fatal("undo did not unsave")
	}

	e, _ = m.selected()
	uid := e.UID
	m.setSeenLocal(0, "INBOX", uid, true)
	m.queueFlagUndo(0, "INBOX", e, backend.FlagSeen, true)
	drainOps(t, m)
	if !seen(m.accts[0], uid) {
		t.Fatal("i did not mark read")
	}
	m.undoLast()
	drainOps(t, m)
	if seen(m.accts[0], uid) {
		t.Fatal("undo did not restore unread")
	}
	if hasToast(m, "Geri alınacak bir şey yok") {
		t.Fatal("flag/read undo said empty")
	}
}

func TestTrashFolderShowsTotalCount(t *testing.T) {
	m, _ := memModel(t, 3)
	m.onKey(key("d"))
	m.onKey(key("d"))
	var n int
	for _, r := range m.folderRows() {
		if r.kind == rowFolder && r.folder == "Trash" {
			n = r.count
		}
	}
	if n != 2 {
		t.Fatalf("Çöp Kutusu count=%d, want 2", n)
	}
}
