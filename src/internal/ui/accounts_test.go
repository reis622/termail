package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/account"
	"termail/internal/backend"
	"termail/internal/grid"
	"termail/internal/toast"
)

func twoAccounts(t *testing.T) (*Model, []*backend.Memory) {
	t.Helper()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.Local)
	var accts []*Acct
	var mems []*backend.Memory
	for i, email := range []string{"ben@kisisel.com", "ben@firma.com"} {
		folders := []backend.Folder{{Name: "INBOX", Role: backend.RoleInbox}, {Name: "Archive", Role: backend.RoleArchive}, {Name: "Trash", Role: backend.RoleTrash}}
		mem := backend.NewMemory(folders)
		a := &Acct{Cfg: account.Account{Name: []string{"Kişisel", "İş"}[i], Email: email}, B: mem, Folders: folders}
		accts = append(accts, a)
		mems = append(mems, mem)
	}
	m := New(Options{Accounts: accts, Now: func() time.Time { return now }})
	m.w, m.h = 140, 40
	accts[0].Lists["INBOX"] = []backend.Envelope{
		{UID: 7, From: "A", Subject: "k-yeni", Date: now.Add(-1 * time.Hour)},
		{UID: 6, From: "B", Subject: "k-eski", Date: now.Add(-5 * time.Hour)},
	}
	accts[1].Lists["INBOX"] = []backend.Envelope{
		{UID: 7, From: "C", Subject: "i-orta", Date: now.Add(-3 * time.Hour), MessageID: "<c@x>"},
	}
	return m, mems
}

func TestMergeInboxesSorted(t *testing.T) {
	m, _ := twoAccounts(t)
	got := mergeInboxes(m.accts)
	want := []string{"k-yeni", "i-orta", "k-eski"}
	if len(got) != 3 {
		t.Fatalf("len %d", len(got))
	}
	for i, w := range want {
		if got[i].Subject != w {
			t.Fatalf("row %d = %q want %q", i, got[i].Subject, w)
		}
	}
	if got[1].Acct != 1 || got[1].Folder != "INBOX" || got[0].Acct != 0 {
		t.Fatal("account / folder not recorded on merged rows")
	}
}

func TestMergeSavedDedup(t *testing.T) {
	m, _ := twoAccounts(t)
	d := time.Now()
	m.accts[0].Saved = []backend.Envelope{
		{UID: 1, Folder: "INBOX", MessageID: "<a@x>", Subject: "a", Date: d},
		{UID: 9, Folder: "Archive", MessageID: "<a@x>", Subject: "a (kopya)", Date: d},
		{UID: 2, Folder: "Archive", MessageID: "<b@x>", Subject: "b", Date: d.Add(-time.Hour)},
	}
	m.accts[1].Saved = []backend.Envelope{{UID: 1, Folder: "INBOX", MessageID: "<a@x>", Subject: "başka hesap", Date: d.Add(time.Hour)}}
	got := mergeSaved(m.accts)
	if len(got) != 3 || got[0].Subject != "başka hesap" || got[0].Acct != 1 || got[1].Subject != "a" || got[2].Subject != "b" {
		t.Fatalf("merge wrong: %+v", got)
	}
}

func TestUnifiedRowsAndReplyFrom(t *testing.T) {
	m, _ := twoAccounts(t)
	m.showUnified()
	m.onKey(key("down")) // the İş mail
	e, _ := m.selected()
	if ai, fo := m.loc(e); ai != 1 || fo != "INBOX" || e.Subject != "i-orta" {
		t.Fatalf("selected %q at %d/%s", e.Subject, ai, fo)
	}
	m.onKey(key("r"))
	c := m.tabs[len(m.tabs)-1].compose
	if c == nil || c.from != 1 {
		t.Fatal("reply in the merged inbox is not sent from the receiving account")
	}
	m.active = 0
	m.onKey(key("f"))
	if c := m.tabs[len(m.tabs)-1].compose; c.from != 1 {
		t.Fatal("forward not from the receiving account")
	}
}

func TestAccountKeys(t *testing.T) {
	cases := map[string]int{"f1": -1, "f2": 0, "f3": 1, "f12": 10}
	for k, want := range cases {
		if got, ok := accountKey(k); !ok || got != want {
			t.Errorf("accountKey(%q) = %d,%v want %d", k, got, ok, want)
		}
	}
	for _, k := range []string{"1", "9", "0", "ctrl+2", "ctrl+0", "a", "f13", "f0", "ctrl+a", "alt+1", "enter"} {
		if _, ok := accountKey(k); ok {
			t.Errorf("accountKey(%q) should not switch", k)
		}
	}
	// kitty "\x1b[50;5u" (Ctrl+2) and xterm "\x1b[27;5;51~" (Ctrl+3) as Bubble Tea reports them
	if k, ok := ctrlDigitFromCSI("?CSI[53 48 59 53 117]?"); !ok || k != "ctrl+2" {
		t.Errorf("kitty: %q %v", k, ok)
	}
	if k, ok := ctrlDigitFromCSI("?CSI[50 55 59 53 59 53 49 126]?"); !ok || k != "ctrl+3" {
		t.Errorf("xterm: %q %v", k, ok)
	}
	if _, ok := ctrlDigitFromCSI("?CSI[53 48 59 51 117]?"); ok { // Alt+2, not Ctrl
		t.Error("Alt+2 taken for Ctrl+2")
	}
}

func TestAccountSwitchKeysOutsideTextOnly(t *testing.T) {
	m, _ := twoAccounts(t)
	m.onKey(tea.KeyMsg{Type: tea.KeyF3})
	if m.cur != 1 || m.virt != viewFolder {
		t.Fatal("F3 did not switch to the second account")
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlA})
	if m.acctPicker == nil {
		t.Fatal("Ctrl+A did not open the account picker")
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyF1})
	if m.virt != viewUnified || m.acctPicker != nil {
		t.Fatal("picker F1 did not open the merged inbox")
	}
	// Ctrl+A and digits are text while composing
	m.onKey(key("n"))
	c := m.tabs[m.active].compose
	m.onKey(key("1"))
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlA})
	if m.acctPicker != nil || m.active == 0 || c.to.Value() != "1" {
		t.Fatalf("keys leaked out of the compose field: picker=%v to=%q", m.acctPicker != nil, c.to.Value())
	}
}

func TestToggleSavedAndRollback(t *testing.T) {
	m, mems := twoAccounts(t)
	ctx := context.Background()
	uid := mems[1].Add("INBOX", backend.Envelope{From: "C", Subject: "kaydet beni", Date: time.Now(), MessageID: "<s@x>"}, []byte("Subject: x\r\n\r\nx"))
	m.accts[1].Lists["INBOX"] = []backend.Envelope{{UID: uid, From: "C", Subject: "kaydet beni", Date: time.Now(), MessageID: "<s@x>"}}
	m.switchAccount(1)
	m.accts[1].Lists["INBOX"][0].Seen = true
	m.openRead(1) // a read mail lives in Okunanlar
	cmd := m.onKey(key("k"))
	if !m.accts[1].Lists["INBOX"][0].Flagged || len(mergeSaved(m.accts)) != 1 {
		t.Fatal("k did not mark the mail saved at once")
	}
	_ = cmd
	drainOps(t, m)
	if envs, _ := mems[1].Flagged(ctx, "INBOX", 0); len(envs) != 1 {
		t.Fatal("server flag not set")
	}
	// now unsave while the server refuses: the change is rolled back
	mems[1].FailFlags(true)
	cmd = m.onKey(key("k"))
	if m.accts[1].Lists["INBOX"][0].Flagged || len(mergeSaved(m.accts)) != 0 {
		t.Fatal("unsave not applied optimistically")
	}
	drainOps(t, m)
	if !m.accts[1].Lists["INBOX"][0].Flagged || len(mergeSaved(m.accts)) != 1 {
		t.Fatal("failed unsave was not rolled back")
	}
	found := false
	for _, a := range m.toasts.Actions() {
		if a.Kind == toast.Error && strings.HasPrefix(a.Text, "Kayıt kaldırılamadı") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no friendly error toast after rollback: %+v", m.toasts.Actions())
	}
}

// drainOps runs every queued server change now (ignoring the coalescing
// window) and feeds the results back, like the real event loop would.
func drainOps(t *testing.T, m *Model) {
	t.Helper()
	for i := 0; i < 50; i++ {
		bs := m.ops.Ready(time.Now().Add(time.Hour))
		if len(bs) == 0 {
			return
		}
		for _, b := range bs {
			m.Update(m.runBatch(b)())
		}
	}
	t.Fatal("ops never drained")
}

func TestFKeysSwitchAccountsNotInCompose(t *testing.T) {
	m, _ := twoAccounts(t)
	m.Update(tea.KeyMsg{Type: tea.KeyF3})
	if m.virt != viewFolder || m.cur != 1 {
		t.Fatalf("F3 should open account 2, got virt %d cur %d", m.virt, m.cur)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyF1})
	if m.virt != viewUnified {
		t.Fatal("F1 should open Tüm Gelen Kutuları")
	}
	m.onKey(key("n")) // composing: F-keys never switch accounts
	m.Update(tea.KeyMsg{Type: tea.KeyF2})
	if m.tabs[m.active].kind != tabCompose || m.virt != viewUnified {
		t.Fatal("F2 left the compose tab")
	}
}

func TestDigitsDoNotSwitchAccounts(t *testing.T) {
	m, _ := twoAccounts(t)
	cur, virt := m.cur, m.virt
	for _, k := range []string{"1", "2", "0"} {
		m.onKey(key(k))
		if m.cur != cur || m.virt != virt {
			t.Fatalf("%q switched account: cur=%d virt=%d", k, m.cur, m.virt)
		}
	}
	// inside a mail too
	m.onKey(key("enter"))
	if m.focus != focusReader {
		t.Fatal("setup: enter reader")
	}
	m.onKey(key("1"))
	if m.cur != cur || m.virt != virt || m.focus != focusReader {
		t.Fatalf("digit in the reader switched account: cur=%d virt=%d focus=%d", m.cur, m.virt, m.focus)
	}
}

func TestAccountChipsFKeysAndActiveFill(t *testing.T) {
	m, _ := twoAccounts(t)
	m.applySize(120, 30)
	g := grid.New(120, 2, cFG, cBar)
	m.drawAccountChips(g, 0, 120)
	text := ""
	for x := 0; x < g.W; x++ {
		if c := g.At(x, 0); c != nil {
			text += c.Ch
		}
	}
	if !strings.Contains(text, "F2") || !strings.Contains(text, "F3") {
		t.Fatalf("chips should use F-keys, got %q", text)
	}
	if strings.Contains(text, " 1 ") && !strings.Contains(text, "F2") {
		t.Fatalf("digit labels still present: %q", text)
	}
	// first account is active: some cell of F2 has the solid chip background
	found := false
	for x := 0; x < g.W; x++ {
		c := g.At(x, 0)
		if c != nil && c.Ch == "F" && x+1 < g.W && g.At(x+1, 0).Ch == "2" && c.BG == cChip && c.FG == cChipFG && c.Bold {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("active account chip not filled with accent: %q", text)
	}
	// second account stays dim
	dim := false
	for x := 0; x < g.W-1; x++ {
		c := g.At(x, 0)
		if c != nil && c.Ch == "F" && g.At(x+1, 0).Ch == "3" && c.BG == cBar && c.FG == cFaint {
			dim = true
			break
		}
	}
	if !dim {
		t.Fatalf("inactive account should stay dim: %q", text)
	}
}
