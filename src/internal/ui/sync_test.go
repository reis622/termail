package ui

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/localstore"
)

func TestApplySyncDeltaUpdatesListAndCache(t *testing.T) {
	m, mem := memModel(t, 5)
	all, _ := mem.List(context.Background(), "INBOX", 0)
	a := m.accts[0]
	a.Lists["INBOX"] = all
	a.Loaded["INBOX"] = true
	a.folderSync = map[string]backend.FolderSync{"INBOX": mem.LastFolderSync("INBOX")}
	st := a.folderSync["INBOX"]

	if err := mem.SetFlag(context.Background(), "INBOX", all[0].UID, backend.FlagSeen, true); err != nil {
		t.Fatal(err)
	}
	if err := mem.Delete(context.Background(), "INBOX", all[1].UID); err != nil {
		t.Fatal(err)
	}
	mem.Add("INBOX", backend.Envelope{Subject: "yeni", Date: time.Now()}, []byte("Subject: yeni\r\n\r\nx"))
	cmd := m.syncOrLoadList(0, "INBOX")
	if cmd == nil {
		t.Fatal("expected a sync")
	}
	m.Update(cmd())
	var hasNew, gone, seen bool
	for _, e := range a.Lists["INBOX"] {
		if e.Subject == "yeni" {
			hasNew = true
		}
		if e.UID == all[1].UID {
			gone = true
		}
		if e.UID == all[0].UID && e.Seen {
			seen = true
		}
	}
	if !hasNew || gone || !seen {
		t.Fatalf("delta not applied: new=%v gone-still=%v seen=%v list=%v", hasNew, gone, seen, subjects(a.Lists["INBOX"]))
	}
	if a.folderSync["INBOX"].HighestModSeq <= st.HighestModSeq && a.folderSync["INBOX"].UIDNext <= st.UIDNext {
		t.Fatalf("bookmark not advanced: %+v -> %+v", st, a.folderSync["INBOX"])
	}
}

func TestUIDValidityChangeTriggersFullReload(t *testing.T) {
	m, mem := memModel(t, 3)
	all, _ := mem.List(context.Background(), "INBOX", 0)
	a := m.accts[0]
	a.Lists["INBOX"] = all
	a.Loaded["INBOX"] = true
	a.folderSync = map[string]backend.FolderSync{"INBOX": mem.LastFolderSync("INBOX")}
	mem.BumpUIDValidity("INBOX")
	_, cmd := m.Update(m.syncOrLoadList(0, "INBOX")())
	runUICmd(t, m, cmd)
	if !a.Loaded["INBOX"] {
		t.Fatal("reload did not complete")
	}
}

func TestWarmStartUsesCacheThenSync(t *testing.T) {
	dir := t.TempDir()
	st := &localstore.Store{Dir: dir}
	mem := backend.NewMemory([]backend.Folder{{Name: "INBOX", Role: backend.RoleInbox}})
	now := time.Now()
	var headers []localstore.Header
	for i := 1; i <= 20; i++ {
		uid := mem.Add("INBOX", backend.Envelope{Subject: "k", Date: now.Add(time.Duration(i) * time.Second)}, []byte("Subject: k\r\n\r\nx"))
		headers = append(headers, localstore.Header{UID: uid, Subject: "k", Date: now.Add(time.Duration(i) * time.Second)})
	}
	fs := mem.LastFolderSync("INBOX")
	if err := st.SaveInbox(localstore.Inbox{Email: "a@b.c", Folder: "INBOX", List: headers, UIDValidity: fs.UIDValidity, UIDNext: fs.UIDNext, HighestModSeq: fs.HighestModSeq}); err != nil {
		t.Fatal(err)
	}
	mem.Add("INBOX", backend.Envelope{Subject: "yeni", Date: now.Add(time.Hour)}, []byte("Subject: yeni\r\n\r\nx"))

	m, _ := memModel(t, 0)
	m.opt.Store = st
	m.accts[0].B = mem
	m.accts[0].Cfg.Email = "a@b.c"
	m.accts[0].Loaded["INBOX"] = false
	m.accts[0].Lists["INBOX"] = nil
	m.accts[0].fromCache = false
	m.Update(m.loadLocal()())
	a := m.accts[0]
	if !a.fromCache || len(a.Lists["INBOX"]) != 20 {
		t.Fatalf("cache not shown: fromCache=%v n=%d", a.fromCache, len(a.Lists["INBOX"]))
	}
	cmd := m.loadListPage(0, "INBOX")
	if cmd == nil {
		t.Fatal("expected sync from warm start")
	}
	m.Update(cmd())
	var hasNew bool
	for _, e := range a.Lists["INBOX"] {
		if e.Subject == "yeni" {
			hasNew = true
		}
	}
	if !hasNew {
		t.Fatalf("warm sync missed new mail, list=%v", subjects(a.Lists["INBOX"]))
	}
}

type overlapPage struct {
	*backend.Memory
	inFlight, maxFlight int32
	release             chan struct{}
}

func (o *overlapPage) ListFlagPage(ctx context.Context, folder string, unseen bool, offset, limit int) ([]backend.Envelope, int, error) {
	n := atomic.AddInt32(&o.inFlight, 1)
	for {
		old := atomic.LoadInt32(&o.maxFlight)
		if n <= old || atomic.CompareAndSwapInt32(&o.maxFlight, old, n) {
			break
		}
	}
	if offset > 0 && o.release != nil {
		select {
		case <-o.release:
		case <-ctx.Done():
		}
	}
	envs, total, err := o.Memory.ListFlagPage(ctx, folder, unseen, offset, limit)
	atomic.AddInt32(&o.inFlight, -1)
	return envs, total, err
}

func TestPipelineFetchesNextPagesInParallel(t *testing.T) {
	const n = 600
	m, mem := memModel(t, n)
	wrap := &overlapPage{Memory: mem, release: make(chan struct{})}
	m.accts[0].B = wrap
	m.accts[0].Lists["INBOX"] = nil
	m.accts[0].Loaded["INBOX"] = false
	first := m.startListLoad(0, "INBOX", false, 0)
	_, rest := m.Update(first())
	if rest == nil {
		t.Fatal("expected pipelined follow-up pages")
	}
	cmds := cmdsOf(rest())
	var pages []tea.Cmd
	for _, c := range cmds {
		if c != nil {
			pages = append(pages, c)
		}
	}
	if len(pages) < 2 {
		t.Fatalf("want two pipelined pages, got %d", len(pages))
	}
	done := make(chan struct{}, len(pages))
	for _, c := range pages {
		c := c
		go func() { c(); done <- struct{}{} }()
	}
	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&wrap.maxFlight) < 2 {
		select {
		case <-deadline:
			t.Fatalf("pages did not overlap, max in-flight %d", atomic.LoadInt32(&wrap.maxFlight))
		case <-time.After(5 * time.Millisecond):
		}
	}
	close(wrap.release)
	for range pages {
		<-done
	}
}
