package ui

import (
	"strings"
	"testing"
	"time"

	"termail/internal/backend"
)

func TestStaleRefreshKeepsOptimisticSave(t *testing.T) {
	m, mem := memModel(t, 2)
	m.cursor = 0
	e, _ := m.selected()
	staleList := m.loadList(0, "INBOX", false)() // asked before the STORE
	staleSaved := m.loadSaved(0)
	m.onKey(key("k"))
	if savedCount(m.accts) != 1 {
		t.Fatal("k did not save at once")
	}
	var savedMsg any
	if staleSaved != nil {
		savedMsg = staleSaved()
	}
	drainOps(t, m) // the server accepts
	m.Update(staleList)
	if savedMsg != nil {
		m.Update(savedMsg)
	}
	if savedCount(m.accts) != 1 || !m.accts[0].Lists["INBOX"][0].Flagged {
		t.Fatal("a stale refresh undid the save")
	}
	// a refresh right after the confirmation (servers may lag) keeps it too
	if envs, _ := mem.Flagged(t.Context(), "INBOX", 0); len(envs) != 1 {
		t.Fatal("server flag not set")
	}
	// unsave, then a stale snapshot that still has it saved
	staleList = m.loadList(0, "INBOX", false)()
	m.onKey(key("k"))
	drainOps(t, m)
	m.Update(staleList)
	if savedCount(m.accts) != 0 || m.accts[0].Lists["INBOX"][0].Flagged {
		t.Fatal("a stale refresh brought an unsaved mail back")
	}
	_ = e
}

func TestServerWinsAfterGrace(t *testing.T) {
	m, mem := memModel(t, 1)
	m.onKey(key("k"))
	drainOps(t, m)
	for _, mk := range m.flagMarks {
		mk.confirmed = time.Now().Add(-2 * flagGrace)
	}
	uid := m.accts[0].Lists["INBOX"][0].UID
	mem.SetFlag(t.Context(), "INBOX", uid, backend.FlagFlagged, false) // another client unsaved it
	m.Update(m.loadList(0, "INBOX", false)())
	if m.accts[0].Lists["INBOX"][0].Flagged {
		t.Fatal("a fresh server snapshot must win once the change is settled")
	}
}

func TestRefusedSaveRollsBackWithTurkishError(t *testing.T) {
	m, mem := memModel(t, 1)
	mem.FailFlags(true)
	m.onKey(key("k"))
	drainOps(t, m)
	if savedCount(m.accts) != 0 || m.accts[0].Lists["INBOX"][0].Flagged {
		t.Fatal("refused save not rolled back")
	}
	found := false
	for _, a := range m.toasts.Actions() {
		found = found || strings.HasPrefix(a.Text, "Kaydedilemedi")
	}
	if !found {
		t.Fatal("no Turkish error")
	}
	if len(m.flagMarks) != 0 {
		t.Fatal("a refused change must not keep winning")
	}
}

func TestStaleRefreshKeepsReadMailInOkunanlar(t *testing.T) {
	m, _ := memModel(t, 2)
	stale := m.loadList(0, "INBOX", false)()
	old := openFileFn
	openFileFn = func(string) error { return nil }
	defer func() { openFileFn = old }()
	completeOpen(t, m, m.onKey(key("o"))) // opened: read, in Okunanlar
	drainOps(t, m)
	m.Update(stale)
	if len(m.visible()) != 2 {
		t.Fatalf("stale refresh must keep both inbox mails: %d", len(m.visible()))
	}
}

// The observed flicker: saved → a snapshot asked before the STORE arrives
// while it is still on its way → the server confirms. The mail must never
// leave Kaydedilenler (and an opened mail never leaves Okunanlar).
func TestSaveFlickerSequence(t *testing.T) {
	m, _ := memModel(t, 2)
	staleList := m.loadList(0, "INBOX", false)()
	staleSaved := m.loadSaved(0)()
	m.onKey(key("k"))
	check := func(step string) {
		t.Helper()
		if savedCount(m.accts) != 1 {
			t.Fatalf("%s: mail left Kaydedilenler", step)
		}
	}
	check("saved")
	m.Update(staleSaved) // stale, while the STORE is pending
	check("stale saved list while pending")
	m.Update(staleList)
	check("stale inbox list while pending")
	drainOps(t, m) // server confirms
	check("confirmed")
	m.Update(staleSaved) // a late copy of the old snapshot
	check("stale saved list after confirm")
	m.Update(m.loadSaved(0)()) // a fresh one agrees
	check("fresh saved list")
}

func TestReadFlickerSequence(t *testing.T) {
	m, _ := memModel(t, 2)
	stale := m.loadList(0, "INBOX", false)()
	old := openFileFn
	openFileFn = func(string) error { return nil }
	defer func() { openFileFn = old }()
	completeOpen(t, m, m.onKey(key("o"))) // read → Okunanlar
	inOkunanlar := func(step string) {
		t.Helper()
		m.openFolder(0, "INBOX")
		if len(m.visible()) != 2 {
			t.Fatalf("%s: inbox should still list both mails, got %d", step, len(m.visible()))
		}
	}
	inOkunanlar("opened")
	m.Update(stale) // pending
	inOkunanlar("stale while pending")
	drainOps(t, m)
	m.Update(stale)
	inOkunanlar("stale after confirm")
}
