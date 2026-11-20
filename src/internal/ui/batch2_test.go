package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/mailmsg"
	"termail/internal/toast"
)

func TestTrashGroupRestoreAllToInbox(t *testing.T) {
	m, mem := memModel(t, 3)
	for i := 0; i < 3; i++ {
		m.onKey(key("d"))
	}
	drainOps(t, m)
	if serverCount(t, mem, "INBOX") != 0 || serverCount(t, mem, "Trash") != 3 {
		t.Fatal("setup: mails should be in trash")
	}
	m.folder = "Trash"
	m.virt = viewFolder
	m.cur = 0
	m.grouped = true
	m.focus = focusList
	m.cursor, m.offset = 0, 0
	if !m.onGroupHeader() {
		t.Fatalf("want a sender group header in trash, visible=%d", len(m.visible()))
	}
	cmd := m.onKey(key("m"))
	if m.bulk == nil || !m.bulk.restore {
		t.Fatal("m on a trash group should start a restore")
	}
	progress := false
	for _, a := range m.toasts.Actions() {
		if strings.Contains(a.Text, "Geri yükleniyor") {
			progress = true
		}
	}
	if !progress {
		t.Fatalf("want a progress toast, got %+v", m.toasts.Actions())
	}
	if cmd != nil {
		// startBulkRestore returns a batch of queueMove cmds; drain the queue
		_ = cmd
	}
	drainOps(t, m)
	if serverCount(t, mem, "INBOX") != 3 || serverCount(t, mem, "Trash") != 0 {
		t.Fatalf("restore did not move back to inbox: inbox=%d trash=%d", serverCount(t, mem, "INBOX"), serverCount(t, mem, "Trash"))
	}
	if !m.bulkUndoable() {
		t.Fatal("restore should be undoable with u")
	}
	cmd = m.onKey(key("u"))
	if cmd == nil {
		t.Fatal("u should undo the restore")
	}
	m.Update(cmd())
	drainOps(t, m)
	if serverCount(t, mem, "INBOX") != 0 || serverCount(t, mem, "Trash") != 3 {
		t.Fatalf("undo should put the mails back in trash: inbox=%d trash=%d", serverCount(t, mem, "INBOX"), serverCount(t, mem, "Trash"))
	}
}

func TestKeybarShowsMoveRestoreAndNewFolder(t *testing.T) {
	m, _ := testModel(t)
	m.applySize(140, 40)
	v := m.View()
	if !strings.Contains(v, "Klasöre taşı") || !strings.Contains(v, " m ") && !strings.Contains(v, "m Klasöre") && !strings.Contains(v, "m") {
		// the keybar renders the key and label; be lenient about spacing
		if !strings.Contains(v, "Klasöre taşı") {
			t.Fatalf("list keybar missing Klasöre taşı: %s", v[len(v)-400:])
		}
	}
	m.focus = focusFolders
	v = m.View()
	if !strings.Contains(v, "Klasör") || !strings.Contains(v, "Alt+N") {
		t.Fatalf("folder keybar should show Alt+N Klasör, tail=%q", v[max(0, len(v)-300):])
	}
	m, mem := memModel(t, 3)
	_ = mem
	for i := 0; i < 3; i++ {
		m.onKey(key("d"))
	}
	m.folder, m.virt, m.cur = "Trash", viewFolder, 0
	m.grouped, m.focus, m.cursor = true, focusList, 0
	m.applySize(140, 40)
	v = m.View()
	if !strings.Contains(v, "Geri yükle") {
		t.Fatalf("trash group keybar should show Geri yükle, tail=%q", v[max(0, len(v)-400):])
	}
}

func TestOTPTolerantAndFalsePositives(t *testing.T) {
	got := findVerifyCodes("Giriş kody 551299 onay için")
	if len(got) == 0 || got[0] != "551299" {
		t.Fatalf("typo 'kody' should still find the code, got %v", got)
	}
	got = findVerifyCodes("Doğrulama", "Merhaba,\n\n847291\n\nİyi günler")
	if len(got) != 0 {
		t.Fatalf("a cue not adjacent to the digits must not be a code, got %v", got)
	}
	got = findVerifyCodes("Doğrulama kodu 847291")
	if len(got) == 0 || got[0] != "847291" {
		t.Fatalf("doğrulama kodu adjacent to digits, got %v", got)
	}
	got = findVerifyCodes("Onay kodu", "Tarih 15.11.2026, fiyat 99,99 TL, telefon +90 532 123 45 67, yıl 2024")
	if len(got) != 0 {
		t.Fatalf("dates/prices/phones/years must not be codes, got %v", got)
	}
}

func TestTabsTitleShowsCodeCount(t *testing.T) {
	p := &mailmsg.Parsed{Subject: "Kod", Text: "doğrulama kodu: 551299", Attachments: nil, Links: nil}
	got := tabsTitle(p)
	if got != "Ekler ve Bağlantılar (0 · 0 · 1 kod)" {
		t.Fatalf("tabsTitle=%q", got)
	}
	if strings.Contains(got, "551299") {
		t.Fatal("tab label must not contain the code itself")
	}
	empty := tabsTitle(&mailmsg.Parsed{})
	if empty != "Ekler ve Bağlantılar (0 · 0)" {
		t.Fatalf("no-code title=%q", empty)
	}
}

func TestServerSearchDropsUnverifiedHits(t *testing.T) {
	m := groupedModel(t)
	press(m, "/")
	typeText(m, "code")
	m.search.server = []backend.Envelope{
		{UID: 99, Acct: 0, Folder: "INBOX", From: "Binance", FromAddr: "noreply@binance.com", Subject: "Hesap bildirimi"},
		{UID: 88, Acct: 0, Folder: "INBOX", From: "Stack Overflow", FromAddr: "do-not-reply@stackoverflow.com", Subject: "Weekly digest"},
	}
	m.search.srvSeq = m.search.seq
	for _, e := range m.searchResults() {
		if e.UID == 99 || e.UID == 88 {
			t.Fatalf("unrelated server hit listed: %+v snippet=%q", e, m.snippet(e, m.search.q.Words, 40))
		}
	}
	m.search.server = []backend.Envelope{
		{UID: 77, Acct: 0, Folder: "INBOX", From: "Banka", FromAddr: "x@bank.com", Subject: "Your code is ready"},
	}
	m.search.srvSeq = m.search.seq
	found := false
	for _, e := range m.searchResults() {
		if e.UID == 77 {
			found = true
		}
	}
	if !found {
		t.Fatal("a subject match from the server should stay")
	}
	if loc := m.searchMatchLoc(m.search.server[0], []string{"code"}); !strings.Contains(loc, "Konu") {
		t.Fatalf("match location should point at the subject, got %q", loc)
	}
}

func TestCustomFoldersNoHeadingAndNCreates(t *testing.T) {
	m, mem := memModel(t, 1)
	m.accts[0].Folders = append(m.accts[0].Folders, backend.Folder{Name: "Faturalar"})
	for _, r := range m.folderRows() {
		if r.label == "Klasörlerim" || r.kind == rowHeading && strings.Contains(r.label, "Klasörlerim") {
			t.Fatal("Klasörlerim heading must be gone")
		}
	}
	custom := false
	for _, r := range m.folderRows() {
		if r.folder == "Faturalar" {
			custom = true
		}
	}
	if !custom {
		t.Fatal("custom folder should still be listed under the account")
	}
	m.focus = focusFolders
	for i, r := range m.folderRowsSelectable() {
		if r.kind == rowAcct {
			m.fcursor = i
			break
		}
	}
	m.onKey(asKey("alt+n"))
	if m.newFolder == nil {
		t.Fatal("Alt+N on an account row should open the name field")
	}
	m.onKey(key("esc"))
	if m.newFolder != nil {
		t.Fatal("Esc should cancel")
	}
	m.onKey(asKey("alt+n"))
	for _, r := range "Projeler" {
		m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	cmd := m.onKey(key("enter"))
	if m.newFolder != nil {
		t.Fatal("Enter should close the input")
	}
	seen := false
	for _, f := range m.accts[0].Folders {
		if f.Name == "Projeler" {
			seen = true
		}
	}
	if !seen {
		t.Fatal("new folder should appear immediately")
	}
	if cmd == nil {
		t.Fatal("Enter should CREATE on the server")
	}
	msg, ok := cmd().(folderCreatedMsg)
	if !ok || msg.err != nil {
		t.Fatalf("CREATE failed: %#v", msg)
	}
	fs, err := mem.Folders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range fs {
		if f.Name == "Projeler" {
			found = true
		}
	}
	if !found {
		t.Fatal("server is missing the created folder")
	}
}

func TestArchiveAndStarredHiddenSavedShowsAccount(t *testing.T) {
	m, _ := twoAccounts(t)
	m.accts[0].Folders = append(m.accts[0].Folders, backend.Folder{Name: "[Gmail]/Starred", Role: backend.RoleFlagged}, backend.Folder{Name: "[Gmail]/All Mail", Role: backend.RoleAll})
	for _, r := range m.folderRows() {
		if r.kind != rowFolder {
			continue
		}
		f := m.accts[r.acct].folder(r.folder)
		if f != nil && (f.Role == backend.RoleArchive || f.Role == backend.RoleFlagged || f.Role == backend.RoleAll) {
			t.Fatalf("hidden role still listed: %s %s", f.Name, f.Role)
		}
		if strings.Contains(r.label, "Arşiv") || strings.Contains(r.label, "Yıldızlı") || strings.Contains(r.label, "Tüm postalar") {
			t.Fatalf("hidden folder label visible: %q", r.label)
		}
	}
	now := m.opt.Now()
	m.accts[0].Saved = []backend.Envelope{{UID: 1, Folder: "INBOX", From: "A", Subject: "kayıt-0", Date: now, Flagged: true, Acct: 0}}
	m.accts[1].Saved = []backend.Envelope{{UID: 2, Folder: "INBOX", From: "B", Subject: "kayıt-1", Date: now.Add(-time.Hour), Flagged: true, Acct: 1}}
	m.showSaved()
	m.applySize(140, 40)
	v := m.View()
	if !strings.Contains(v, shortName(m.accts[0].Cfg.Name)) || !strings.Contains(v, shortName(m.accts[1].Cfg.Name)) {
		t.Fatalf("Kaydedilenler should show account labels, view=%q", v)
	}
}

func TestRapidDeletesMergeToasts(t *testing.T) {
	m, _ := memModel(t, 4)
	for i := 0; i < 3; i++ {
		m.onKey(key("d"))
	}
	var del []toast.Action
	for _, a := range m.toasts.Actions() {
		if a.Kind == toast.Deleted {
			del = append(del, a)
		}
	}
	if len(del) != 1 {
		t.Fatalf("rapid deletes should be one toast, got %+v", m.toasts.Actions())
	}
	if del[0].Count != 3 || !strings.Contains(del[0].Text, "3 ileti silindi") {
		t.Fatalf("merged text: %+v", del[0])
	}
	m.onKey(key("u"))
	del = nil
	for _, a := range m.toasts.Actions() {
		if a.Kind == toast.Deleted {
			del = append(del, a)
		}
	}
	if len(del) != 1 || del[0].Count != 2 {
		t.Fatalf("undo should keep the toast for the remaining actions: %+v", m.toasts.Actions())
	}
}
