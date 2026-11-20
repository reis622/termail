package ui

import (
	"strconv"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
)

func groupedModel(t *testing.T) *Model {
	t.Helper()
	m, _ := twoAccounts(t)
	now := m.opt.Now()
	m.accts[0].Lists["INBOX"] = []backend.Envelope{
		{UID: 10, From: "Google", FromAddr: "no-reply@accounts.google.com", Subject: "Güvenlik uyarısı", Date: now.Add(-1 * time.Hour)},
		{UID: 9, From: "Ayşe", FromAddr: "ayse@ornek.com", Subject: "Fatura İletimi", Date: now.Add(-2 * time.Hour)},
		{UID: 8, From: "Google Play", FromAddr: "play@google.com", Subject: "Satın alma", Date: now.Add(-3 * time.Hour)},
	}
	m.accts[0].Loaded["INBOX"] = true
	m.cur, m.folder = 0, "INBOX"
	m.applySize(140, 40)
	return m
}

func TestSearchTypingFiltersTurkish(t *testing.T) {
	m := groupedModel(t)
	press(m, "/")
	if m.search == nil || !m.search.focused {
		t.Fatal("/ should open the search")
	}
	typeText(m, "FATURA ILETIMI") // I/İ and case folding
	v := m.visible()
	if len(v) != 1 || v[0].UID != 9 {
		t.Fatalf("want Ayşe only, got %+v", v)
	}
	press(m, "esc") // clears
	if m.search == nil || m.search.in.Value() != "" {
		t.Fatal("first Esc clears the text")
	}
	press(m, "esc") // closes
	if m.search != nil && m.search.focused {
		t.Fatal("second Esc closes")
	}
}

func TestSearchCtrlFAndScope(t *testing.T) {
	m := groupedModel(t)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	if m.search == nil || !m.search.focused {
		t.Fatal("Ctrl+F opens the search")
	}
	typeText(m, "i-orta")
	if len(m.visible()) != 0 {
		t.Fatal("other account must not match in Bu klasör")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if v := m.visible(); len(v) != 1 || v[0].Acct != 1 {
		t.Fatalf("Tüm hesaplar should find the İş mail, got %+v", v)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.prefs.Recent) == 0 || m.prefs.Recent[0] != "i-orta" {
		t.Fatalf("Enter should remember the query, got %v", m.prefs.Recent)
	}
}

func TestStaleServerResultIgnored(t *testing.T) {
	m := groupedModel(t)
	press(m, "/")
	typeText(m, "zzz")
	old := m.search.seq
	m.Update(searchDoneMsg{seq: old - 1, envs: []backend.Envelope{{UID: 99, Subject: "eski", Folder: "INBOX"}}})
	if len(m.visible()) != 0 {
		t.Fatal("stale server answer must be ignored")
	}
}

func TestGroupingKeys(t *testing.T) {
	m := groupedModel(t)
	press(m, "G")
	if !m.grouped {
		t.Fatal("Shift+G groups")
	}
	v := m.visible()
	if len(v) != 2 || !isGroupRow(v[0]) {
		t.Fatalf("want Google group + Ayşe row, got %d rows", len(v))
	}
	m.cursor = 0
	press(m, "d") // confirms first; must not delete until e
	if m.confirm == nil {
		t.Fatal("d on a group header should ask first")
	}
	if n := len(m.accts[0].Lists["INBOX"]); n != 3 {
		t.Fatalf("d on a group header deleted mail (%d left)", n)
	}
	press(m, "h") // cancel
	if m.confirm != nil {
		t.Fatal("h should dismiss the confirm")
	}
	if n := len(m.accts[0].Lists["INBOX"]); n != 3 {
		t.Fatalf("cancelled group delete removed mail (%d left)", n)
	}
	press(m, "enter")
	if len(m.visible()) != 4 {
		t.Fatal("Enter expands the group")
	}
	press(m, "down")
	if e, ok := m.selected(); !ok || e.UID != 10 {
		t.Fatal("first child selectable")
	}
	press(m, "left")
	if len(m.visible()) != 2 || m.cursor != 0 {
		t.Fatal("← inside a group collapses it onto the header")
	}
	press(m, "right")
	if len(m.visible()) != 4 {
		t.Fatal("→ on a group header should expand, not open a mail")
	}
	press(m, "left")
	press(m, "/")
	typeText(m, "google")
	for _, e := range m.visible() {
		if isGroupRow(e) {
			t.Fatal("search results stay flat")
		}
	}
}

func TestAccountTreeKeys(t *testing.T) {
	m := groupedModel(t)
	m.focus = focusFolders
	rows := m.folderRows()
	ai := -1
	for i, r := range rows {
		if r.kind == rowAcct && r.acct == 1 {
			ai = i
		}
	}
	if ai < 0 {
		t.Fatal("no account row")
	}
	m.fcursor = ai
	open := m.accts[1].Open
	press(m, "enter")
	if m.accts[1].Open == open {
		t.Fatal("Enter toggles the account")
	}
	if !m.accts[1].Open {
		press(m, "enter")
	}
	press(m, "left")
	if m.accts[1].Open {
		t.Fatal("← collapses")
	}
	press(m, "right")
	if !m.accts[1].Open {
		t.Fatal("→ expands")
	}
	m.accts[1].Open = false
	m.focus = focusList
	m.Update(tea.KeyMsg{Type: tea.KeyF3})
	if !m.accts[1].Open {
		t.Fatal("F3 should expand the second account")
	}
}

func TestSearchMouseOpens(t *testing.T) {
	m := groupedModel(t)
	m.View()
	var h hit
	for _, x := range m.hits.list {
		if x.kind == hitSearch {
			h = x
		}
	}
	if h.w == 0 {
		t.Fatal("no search hit area")
	}
	mouse(m, h.x+1, h.y, tea.MouseActionPress, tea.MouseButtonLeft)
	mouse(m, h.x+1, h.y, tea.MouseActionRelease, tea.MouseButtonLeft)
	if m.search == nil || !m.search.focused {
		t.Fatal("click opens the search")
	}
}

// BenchmarkSearchTyping: one typed letter with 400 cached headers and bodies.
func BenchmarkSearchTyping(b *testing.B) {
	m := benchModel(400)
	press(m, "/")
	letters := []string{"t", "o", "p", "l"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%5 == 4 {
			m.search.in.SetValue("")
			m.searchChanged()
		} else {
			typeText(m, letters[i%5])
		}
		_ = m.View()
	}
}

func TestSearchTypingBudget(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	r := testing.Benchmark(BenchmarkSearchTyping)
	ms := float64(r.NsPerOp()) / 1e6
	t.Logf("search typing (400 mails with bodies): %.3f ms/letter", ms)
	if ms > 16 {
		t.Fatalf("search letter takes %.2f ms", ms)
	}
	_ = strconv.Itoa
}

// selectUID puts the list cursor on the mail with this UID (sections and
// group headers make plain indexes fragile).
func selectUID(t *testing.T, m *Model, uid uint32) {
	t.Helper()
	for i, e := range m.visible() {
		if !isGroupRow(e) && e.UID == uid {
			m.cursor = i
			return
		}
	}
	t.Fatalf("UID %d not on screen", uid)
}
