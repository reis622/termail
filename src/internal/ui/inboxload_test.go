package ui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
)

func TestWatchOnlineDoesNotAbortInboxPages(t *testing.T) {
	const n = 300
	m, mem := memModel(t, n)
	all, err := mem.List(context.Background(), "INBOX", 0)
	if err != nil || len(all) != n {
		t.Fatalf("list all: %d %v", len(all), err)
	}
	a := m.accts[0]
	a.Lists["INBOX"] = nil
	a.Loaded["INBOX"] = false
	a.inboxAsked = true
	if f := a.folder("INBOX"); f != nil {
		f.Total, f.Unread = n, n
	}
	cmd := m.startListLoad(0, "INBOX", false, 0)
	if cmd == nil {
		t.Fatal("expected a first-page load")
	}
	gen := a.syncGen
	_, cmd = m.Update(cmd())
	if m.haveOf(0, "INBOX", "unseen") != listBatch {
		t.Fatalf("first page: have %d, want %d", m.haveOf(0, "INBOX", "unseen"), listBatch)
	}
	_, _ = m.Update(watchMsg{acct: 0, ev: backend.Event{Kind: backend.EvOnline}})
	if a.syncGen != gen {
		t.Fatalf("EvOnline aborted paging: gen %d -> %d", gen, a.syncGen)
	}
	runUICmd(t, m, cmd)
	if got := m.haveOf(0, "INBOX", "unseen"); got != n {
		t.Fatalf("after EvOnline later pages were dropped: have %d want %d", got, n)
	}
}

func TestF5AndEndLoadMoreInboxPages(t *testing.T) {
	const n = 300
	m, mem := memModel(t, n)
	all, err := mem.List(context.Background(), "INBOX", 0)
	if err != nil || len(all) != n {
		t.Fatalf("list all: %d %v", len(all), err)
	}
	a := m.accts[0]
	a.Lists["INBOX"] = nil
	a.Loaded["INBOX"] = false
	if f := a.folder("INBOX"); f != nil {
		f.Total, f.Unread = n, n
	}
	_, cmd := m.Update(m.startListLoad(0, "INBOX", false, 0)())
	if m.haveOf(0, "INBOX", "unseen") != listBatch {
		t.Fatalf("setup first page: %d", m.haveOf(0, "INBOX", "unseen"))
	}
	// Simulate the old bug: first page is on screen, continuation was lost.
	a.paging = false
	cmd = nil

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	runUICmd(t, m, cmd)
	if got := m.haveOf(0, "INBOX", "unseen"); got != n {
		t.Fatalf("End should load the rest: have %d want %d", got, n)
	}

	// Rebuild a stuck first page and use F5.
	a.Lists["INBOX"] = all[:listBatch]
	sortEnv(a.Lists["INBOX"])
	a.setExpect("INBOX", "unseen", n)
	a.pageOff = map[string]int{pageKey("INBOX", "unseen"): listBatch}
	a.paging = false
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyF5})
	runUICmd(t, m, cmd)
	if got := m.haveOf(0, "INBOX", "unseen"); got != n {
		t.Fatalf("F5 should load the rest: have %d want %d", got, n)
	}
}

func TestListProgressHidesWhenCompleteAndAfterReadOrEmpty(t *testing.T) {
	const n = 40
	m, mem := memModel(t, n)
	all, err := mem.List(context.Background(), "INBOX", 0)
	if err != nil {
		t.Fatal(err)
	}
	a := m.accts[0]
	a.Lists["INBOX"] = all
	a.Loaded["INBOX"] = true
	a.setExpect("INBOX", "unseen", n)
	if f := a.folder("INBOX"); f != nil {
		f.Total, f.Unread = n, n
	}
	a.paging = true
	a.pageFolder, a.pageKind = "INBOX", "unseen"
	a.setPageOff("INBOX", "unseen", n)

	a.paging = false // load finished
	if _, _, ok := m.listProgress(); ok {
		t.Fatal("progress must hide when paging is done")
	}

	a.paging = true // mid-load leftover after a read
	m.setSeenLocal(0, "INBOX", all[0].UID, true)
	have, total, ok := m.listProgress()
	if have >= total && ok {
		t.Fatalf("after a read, progress still %d/%d ok=%v", have, total, ok)
	}
	a.paging = false
	if _, _, ok := m.listProgress(); ok {
		t.Fatal("progress must hide after a read once the list is complete")
	}

	// Empty inbox with a stale 0/1 expect: must not keep showing Yükleniyor.
	a.Lists["INBOX"] = nil
	a.setExpect("INBOX", "unseen", 1)
	if f := a.folder("INBOX"); f != nil {
		f.Total, f.Unread = 0, 1
	}
	a.paging = false
	if _, _, ok := m.listProgress(); ok {
		t.Fatal("empty inbox must not show Yükleniyor 0/1")
	}
	v := m.View()
	if strings.Contains(v, "Yükleniyor 0/1") {
		t.Fatalf("stale progress in view: %s", v)
	}
}
