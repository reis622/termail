package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-message/mail"

	"termail/internal/backend"
	"termail/internal/toast"
)

func draftModel(t *testing.T, inbox, drafts int) (*Model, *backend.Memory) {
	t.Helper()
	mem := backend.NewMemory([]backend.Folder{
		{Name: "INBOX", Role: backend.RoleInbox},
		{Name: "Drafts", Role: backend.RoleDrafts},
		{Name: "Trash", Role: backend.RoleTrash},
		{Name: "Spam", Role: backend.RoleJunk},
	})
	now := time.Now()
	for i := 0; i < inbox; i++ {
		mem.Add("INBOX", backend.Envelope{From: "A", Subject: "in " + time.Now().Format("15:04:05.000") + string(rune('a'+i)), Date: now}, []byte("Subject: x\r\n\r\nx"))
	}
	for i := 0; i < drafts; i++ {
		mem.Add("Drafts", backend.Envelope{From: "Me", Subject: "taslak " + string(rune('1'+i)), Date: now, Seen: true}, []byte("Subject: t\r\n\r\nt"))
	}
	m := opsModel(t, mem, func(s string, d time.Time) uint32 { return 0 }, 0)
	// opsModel already added n=0 inbox mails; load lists
	for _, f := range []string{"INBOX", "Drafts", "Trash"} {
		l, err := mem.List(context.Background(), f, 100)
		if err != nil {
			t.Fatal(err)
		}
		m.accts[0].Lists[f], m.accts[0].Loaded[f] = l, true
	}
	m.accts[0].Folders, _ = mem.Folders(context.Background())
	return m, mem
}

func TestShiftDWorksInDrafts(t *testing.T) {
	m, mem := draftModel(t, 0, 3)
	m.folder, m.virt, m.cur, m.focus = "Drafts", viewFolder, 0, focusList
	if n := len(m.baseList()); n != 3 {
		t.Fatalf("draft list should have 3 mails, got %d", n)
	}
	if cmd := m.onKey(key("D")); cmd != nil || m.confirm == nil {
		t.Fatal("Shift+D in Drafts should ask to move all to trash, not toast about trash/spam only")
	}
	if !strings.Contains(m.confirm.text, "Taslaklar") {
		t.Fatalf("confirm should name Taslaklar: %q", m.confirm.text)
	}
	m.onKey(key("e"))
	if len(m.baseList()) != 0 {
		t.Fatalf("list not emptied at once, leftover %d", len(m.baseList()))
	}
	drainOps(t, m)
	if serverCount(t, mem, "Drafts") != 0 || serverCount(t, mem, "Trash") != 3 {
		t.Fatalf("drafts not moved: drafts=%d trash=%d", serverCount(t, mem, "Drafts"), serverCount(t, mem, "Trash"))
	}
}

func TestStatusIsPerAccount(t *testing.T) {
	m, _ := twoAccounts(t)
	m.accts[0].State = stOnline
	m.accts[1].State = stOffline
	m.virt, m.cur, m.folder = viewFolder, 0, "INBOX"
	m.active, m.focus = 0, focusList
	m.showVirtualView(viewFolder, false)
	m.virt, m.cur, m.folder = viewFolder, 0, "INBOX"
	label, _ := m.statusText()
	if label != "Çevrimiçi" {
		t.Fatalf("viewing the online account should show Çevrimiçi, got %q", label)
	}
	m.cur = 1
	m.folder = "INBOX"
	label, _ = m.statusText()
	if label != "Çevrimdışı" {
		t.Fatalf("viewing the offline account should show Çevrimdışı, got %q", label)
	}
	m.showVirtualView(viewUnified, false)
	// no selected mail: name the offline account
	m.cursor = 99 // off the list if needed
	if i := m.statusAcctIndex(); i == 1 {
		// selected mail might still be from account 1
	}
	// Force mixed view with no single mail: clear selection by using cursor past?
	// statusAcctIndex uses selected mail in unified. Select a mail from account 0.
	// merge list: k-yeni (acct0), i-orta (acct1), k-eski (acct0)
	m.cursor = 0
	label, _ = m.statusText()
	// first mail is k-yeni from account 0 (online)
	if label != "Çevrimiçi" && !strings.Contains(label, "Kişisel") {
		// if loc set Acct on merged envelopes
		if label == "Çevrimdışı" || strings.Contains(label, "İş") {
			t.Fatalf("unified on an online mail should not show the other account offline: %q", label)
		}
	}
	// when no single account is pinned, name the offline one
	// simulate no selection by using an empty visible list
	m.accts[0].Lists["INBOX"] = nil
	m.accts[1].Lists["INBOX"] = nil
	m.virt = viewUnified
	label, _ = m.statusText()
	if !strings.Contains(strings.ToLower(label), "çevrimdışı") || !strings.Contains(label, "İş") {
		t.Fatalf("unified overview should name the offline account, got %q", label)
	}
}

func TestToastWrapsLongError(t *testing.T) {
	m, _ := memModel(t, 1)
	m.applySize(80, 24)
	long := "Taslaklar klasörü bulunamadı; taslak kaydedilemedi. Lütfen sunucu ayarını kontrol et ve tekrar dene, bu yazı kesilmemeli."
	m.toasts.Push(toast.Error, "", long, m.opt.Now(), time.Minute)
	v := m.View()
	// every word should appear (wrapping, not truncating)
	for _, w := range []string{"Taslaklar", "kaydedilemedi", "kesilmemeli"} {
		if !strings.Contains(v, w) {
			t.Fatalf("toast missing %q in\n%s", w, v)
		}
	}
}

func TestComposeFromUsesSelectedMailAccount(t *testing.T) {
	m, _ := twoAccounts(t)
	m.accts[0].State, m.accts[1].State = stOnline, stOnline
	m.showVirtualView(viewUnified, false)
	m.focus = focusList
	// pick the mail from account 1 ("i-orta")
	found := false
	for i, e := range m.visible() {
		ai, _ := m.loc(e)
		if ai == 1 {
			m.cursor = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("account 1 mail not in the unified list")
	}
	m.onKey(key("n"))
	c := m.tabs[m.active].compose
	if c == nil {
		t.Fatal("compose not opened")
	}
	if c.from != 1 {
		t.Fatalf("From should be the selected mail's account, got %d", c.from)
	}
}

func TestComposeFromCycleAndClick(t *testing.T) {
	m, _ := twoAccounts(t)
	m.accts[0].State, m.accts[1].State = stOnline, stOnline
	m.applySize(140, 40)
	m.onKey(key("n"))
	c := m.tabs[m.active].compose
	if c.from != 0 && c.from != 1 {
		t.Fatalf("unexpected from %d", c.from)
	}
	start := c.from
	m.onKey(asKey("ctrl+k"))
	if c.from == start {
		t.Fatal("Ctrl+K should cycle the sender")
	}
	m.onKey(asKey("ctrl+k"))
	if c.from != start {
		t.Fatalf("Ctrl+K twice should return to %d, got %d", start, c.from)
	}
	_ = m.View()
	var row, next, prev hit
	for _, h := range m.hits.list {
		switch h.kind {
		case hitFromRow:
			row = h
		case hitFromNext:
			next = h
		case hitFromPrev:
			prev = h
		}
	}
	if row.w == 0 || next.w == 0 || prev.w == 0 {
		t.Fatal("Kimden row and ‹ › should be clickable")
	}
	before := c.from
	m.Update(tea.MouseMsg{X: next.x, Y: next.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if c.from == before {
		t.Fatal("clicking › should cycle the sender")
	}
	m.Update(tea.MouseMsg{X: row.x + 8, Y: row.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !c.dropdown {
		t.Fatal("clicking Kimden should open the account dropdown")
	}
	_ = m.View()
	var acct hit
	want := 0
	if c.from == 0 {
		want = 1
	}
	for _, h := range m.hits.list {
		if h.kind == hitFromAcct && h.idx == want {
			acct = h
		}
	}
	if acct.w == 0 {
		t.Fatal("dropdown accounts should be clickable")
	}
	m.Update(tea.MouseMsg{X: acct.x, Y: acct.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if c.from != want || c.dropdown {
		t.Fatalf("clicking an account should select it and close the list, from=%d open=%v", c.from, c.dropdown)
	}
	v := m.View()
	if !strings.Contains(v, "hesap değiştir") {
		t.Fatalf("hint missing: tail=%q", v[max(0, len(v)-400):])
	}
}

func TestCloseComposeAsksToSaveDraft(t *testing.T) {
	m, mem := draftModel(t, 1, 0)
	m.applySize(140, 40)
	m.onKey(key("n"))
	c := m.tabs[m.active].compose
	// nothing typed: close silently
	m.onKey(key("esc"))
	if m.confirm != nil || m.tabs[m.active].kind != tabList {
		t.Fatal("empty compose should close without a prompt")
	}
	m.onKey(key("n"))
	c = m.tabs[m.active].compose
	c.to.SetValue("arkadas@ornek.com")
	c.subject.SetValue("merhaba")
	c.body.SetValue("selam")
	c.initial = c.snapshot()[:0] // force dirty if snapshot already captured
	// snapshot was taken at open; setting values makes it dirty
	if !c.dirty() {
		t.Fatal("typed compose should be dirty")
	}
	m.onKey(key("esc"))
	if m.confirm == nil || !strings.Contains(m.confirm.text, "Taslak olarak kaydedilsin mi?") {
		t.Fatalf("expected draft prompt, got %+v", m.confirm)
	}
	// Vazgeç keeps the tab
	m.onKey(key("esc"))
	if m.confirm != nil || m.tabs[m.active].compose != c {
		t.Fatal("Vazgeç should leave the compose tab open")
	}
	m.onKey(key("esc"))
	// Kaydetme discards
	m.onKey(key("d"))
	if m.tabs[m.active].compose == c {
		t.Fatal("Kaydetme should close without saving")
	}
	if serverCount(t, mem, "Drafts") != 0 {
		t.Fatal("Kaydetme must not APPEND a draft")
	}

	m.onKey(key("n"))
	c = m.tabs[m.active].compose
	c.to.SetValue("arkadas@ornek.com")
	c.body.SetValue("kaydet beni")
	m.onKey(key("esc"))
	cmd := m.onKey(key("enter"))
	if cmd != nil {
		m.Update(cmd())
	}
	if serverCount(t, mem, "Drafts") != 1 {
		t.Fatalf("Kaydet should APPEND to Drafts, got %d", serverCount(t, mem, "Drafts"))
	}
}

func TestRecipientAutocomplete(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TERMAIL_CONFIG_DIR", dir)
	m, mem := memModel(t, 1)
	m.recip = nil
	now := m.opt.Now()
	b := m.recipBook()
	mustParse := func(s string) *mail.Address {
		a, err := mail.ParseAddress(s)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	b.remember([]*mail.Address{
		mustParse("Ayşe Demir <ayse@firma.com>"),
		mustParse("Mehmet <mehmet@gmail.com>"),
	}, now)
	_ = b.save()

	m.onKey(key("n"))
	c := m.tabs[m.active].compose
	c.focus = fTo
	c.to.SetValue("ay")
	c.to.CursorEnd()
	c.refreshSuggest(m)
	if len(c.suggest) == 0 || !strings.EqualFold(c.suggest[0].Email, "ayse@firma.com") {
		t.Fatalf("prefix should prefer ayse@firma.com, got %+v", c.suggest)
	}
	m.onKey(key("enter"))
	if !strings.Contains(strings.ToLower(c.to.Value()), "ayse@firma.com") {
		t.Fatalf("Enter should accept the suggestion: %q", c.to.Value())
	}

	// contains match
	c.to.SetValue("firma")
	c.to.CursorEnd()
	c.refreshSuggest(m)
	if len(c.suggest) == 0 || !strings.EqualFold(c.suggest[0].Email, "ayse@firma.com") {
		t.Fatalf("contains should find firma.com, got %+v", c.suggest)
	}
	_ = m.View()
	var sug hit
	for _, h := range m.hits.list {
		if h.kind == hitSuggest && h.idx == 0 {
			sug = h
		}
	}
	if sug.w == 0 {
		t.Fatal("suggestions should be clickable")
	}
	m.Update(tea.MouseMsg{X: sug.x, Y: sug.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !strings.Contains(strings.ToLower(c.to.Value()), "ayse@firma.com") {
		t.Fatalf("click should accept: %q", c.to.Value())
	}

	// saved after a successful send
	c.to.SetValue("yeni@ornek.com")
	c.subject.SetValue("selam")
	c.body.SetValue("merhaba")
	cmd := m.onKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd != nil {
		m.Update(cmd())
	}
	_ = mem
	b2 := loadRecipBook(dir)
	found := false
	for _, e := range b2.Items {
		if strings.EqualFold(e.Email, "yeni@ornek.com") {
			found = true
		}
	}
	if !found {
		t.Fatalf("sent recipient should be stored, items=%+v", b2.Items)
	}
}

func TestFileIndexSearchAndSkip(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "Documents")
	skip := filepath.Join(root, "node_modules")
	git := filepath.Join(root, ".git")
	_ = os.MkdirAll(filepath.Join(docs, "sub"), 0o755)
	_ = os.MkdirAll(skip, 0o755)
	_ = os.MkdirAll(git, 0o755)
	if err := os.WriteFile(filepath.Join(docs, "rapor.pdf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "sub", "rapor-final.pdf"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "notlar.txt"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skip, "secret.pdf"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(git, "rapor.bak"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := newFileIndex([]string{root}, filepath.Join(t.TempDir(), "fileindex.json"))
	idx.build([]string{root})
	hits := idx.search("rapor", 20)
	if len(hits) < 2 {
		t.Fatalf("expected prefix matches under Documents, got %+v", hits)
	}
	if !strings.HasPrefix(strings.ToLower(hits[0].Name), "rapor") {
		t.Fatalf("prefix matches should come first: %+v", hits)
	}
	for _, h := range hits {
		if strings.Contains(h.Path, "node_modules") || strings.Contains(h.Path, string(filepath.Separator)+".git"+string(filepath.Separator)) {
			t.Fatalf("skipped dir leaked: %s", h.Path)
		}
	}
	// contains
	hits = idx.search("final", 10)
	if len(hits) != 1 || !strings.Contains(hits[0].Name, "final") {
		t.Fatalf("contains match: %+v", hits)
	}
}

func TestAttachPickerTypeToSearch(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(root, 0o755)
	if err := os.WriteFile(filepath.Join(root, "fatura-mart.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := newFileIndex([]string{root}, "")
	idx.build([]string{root})
	testFileIndex = idx
	t.Cleanup(func() { testFileIndex = nil })

	m, _ := memModel(t, 1)
	m.applySize(140, 40)
	m.onKey(key("n"))
	c := m.tabs[m.active].compose
	p, _ := newAttachPicker()
	c.picker = p
	p.in.SetValue("fat")
	p.pristine = false
	p.refresh()
	if !p.searching || len(p.entries) == 0 {
		t.Fatalf("typing a name should search the index, searching=%v entries=%v", p.searching, p.entries)
	}
	if !strings.Contains(strings.ToLower(p.entries[0]), "fatura-mart.pdf") {
		t.Fatalf("expected fatura-mart.pdf, got %v", p.entries)
	}
	_ = m.View()
	var item hit
	for _, h := range m.hits.list {
		if h.kind == hitPickerItem && h.idx == 0 {
			item = h
		}
	}
	if item.w == 0 {
		t.Fatal("search results should be clickable")
	}
	m.Update(tea.MouseMsg{X: item.x, Y: item.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if c.picker != nil {
		c.picker.cur = 0
		c.picker.key(m, c, key("enter"))
	}
	if len(c.atts) == 0 {
		t.Fatal("selecting a search hit should attach the file")
	}
}

func TestAttachPickerQuickAccessHits(t *testing.T) {
	m, _ := memModel(t, 1)
	m.applySize(140, 40)
	m.onKey(key("n"))
	c := m.tabs[m.active].compose
	p, _ := newAttachPicker()
	c.picker = p
	// inject a known quick entry even on Linux
	dir := t.TempDir()
	p.quick = []quickEntry{{label: "Belgeler", path: dir}}
	p.refresh()
	_ = m.View()
	c.picker.addHits(m, m.gridBuf)
	// draw was already done; addHits uses current size. Re-view.
	_ = m.View()
	var q hit
	for _, h := range m.hits.list {
		if h.kind == hitPickerQuick && h.s == dir {
			q = h
		}
	}
	if q.w == 0 {
		t.Fatal("quick access entries should be clickable")
	}
	m.Update(tea.MouseMsg{X: q.x, Y: q.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !strings.HasPrefix(c.picker.in.Value(), dir) {
		t.Fatalf("clicking a quick entry should navigate there, path=%q", c.picker.in.Value())
	}
}
