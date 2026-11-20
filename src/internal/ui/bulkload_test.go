package ui

import (
	"context"
	"testing"

	"termail/internal/backend"
)

func TestBulkDeleteChunksMovesToTrashNotExpunge(t *testing.T) {
	const n = 250
	m, mem := memModel(t, n)
	// memModel only caches the first 100 headers; load the rest so the
	// bulk delete covers every mail (as a full inbox load would).
	all, err := mem.List(context.Background(), "INBOX", 0)
	if err != nil || len(all) != n {
		t.Fatalf("list all: %d %v", len(all), err)
	}
	m.accts[0].Lists["INBOX"] = all
	if f := m.accts[0].folder("INBOX"); f != nil {
		f.Total, f.Unread = n, n
	}
	inbox := m.accts[0].folder(m.accts[0].inbox())
	if inbox == nil || inbox.Unread != n {
		t.Fatalf("setup unread: %+v", inbox)
	}
	if m.onKey(key("D")); m.confirm == nil {
		t.Fatal("Shift+D should ask")
	}
	m.onKey(key("e"))
	if !hasToast(m, "Siliniyor 0/250") {
		t.Fatalf("want progress toast, got %+v", m.toasts.Actions())
	}
	if len(m.accts[0].Lists["INBOX"]) != 0 {
		t.Fatalf("list should empty immediately, got %d", len(m.accts[0].Lists["INBOX"]))
	}
	if inbox.Unread != 0 {
		t.Fatalf("Gelen Kutusu badge should drop immediately, unread=%d", inbox.Unread)
	}
	if mem.CallCount("Delete") != 0 {
		t.Fatal("bulk delete must MOVE to trash, not expunge")
	}
	// server still has them until the queue runs
	if serverCount(t, mem, "INBOX") != n {
		t.Fatal("server move should be in the background")
	}
	drainOps(t, m)
	if c := mem.CallCount("MoveMany"); c != 2 {
		t.Fatalf("250 mails should be two batches of 200, got %d MoveMany", c)
	}
	if mem.CallCount("Delete") != 0 {
		t.Fatal("Delete must not be used")
	}
	if serverCount(t, mem, "INBOX") != 0 || serverCount(t, mem, "Trash") != n {
		t.Fatalf("want all %d in trash, inbox=%d trash=%d", n, serverCount(t, mem, "INBOX"), serverCount(t, mem, "Trash"))
	}
	if inbox.Unread != 0 {
		t.Fatalf("unread after drain: %d", inbox.Unread)
	}
	if !hasToast(m, "250 ileti Çöp Kutusu'na taşındı") && !hasToast(m, "Çöp Kutusu'na taşındı") {
		t.Fatalf("want a moved-to-trash toast, got %+v", m.toasts.Actions())
	}
}

func TestFolderCountsStayDownWhileMovePending(t *testing.T) {
	m, mem := memModel(t, 5)
	for i := 0; i < 5; i++ {
		m.onKey(key("d"))
	}
	a := m.accts[0]
	if f := a.folder("INBOX"); f == nil || f.Unread != 0 {
		t.Fatalf("counts should drop on delete, got %+v", a.folder("INBOX"))
	}
	// a stale STATUS still sees the mails on the server
	fs, err := mem.Folders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var stale int
	for _, f := range fs {
		if f.Name == "INBOX" {
			stale = f.Unread
		}
	}
	if stale != 5 {
		t.Fatalf("server STATUS should still be 5 before the move, got %d", stale)
	}
	m.Update(foldersMsg{acct: 0, folders: fs})
	if f := a.folder("INBOX"); f.Unread != 0 {
		t.Fatalf("stale STATUS must not restore the badge while moves are queued, unread=%d", f.Unread)
	}
	drainOps(t, m)
	_, cmd := m.Update(m.refreshFolderStatus(0, "INBOX", "Trash")())
	_ = cmd
	if f := a.folder("INBOX"); f == nil || f.Unread != 0 {
		t.Fatalf("STATUS after the queue should be 0, got %+v", f)
	}
}

func TestTrashShowsLocalCopiesBeforeFolderLoad(t *testing.T) {
	m, _ := memModel(t, 3)
	delete(m.accts[0].Loaded, "Trash")
	m.accts[0].Lists["Trash"] = nil
	m.onKey(key("d"))
	m.onKey(key("d"))
	if len(m.accts[0].Lists["Trash"]) != 2 {
		t.Fatalf("trash should show moved mails before a server list, got %d", len(m.accts[0].Lists["Trash"]))
	}
	m.folder = "Trash"
	m.virt = viewFolder
	_ = m.View()
	if len(m.visible()) < 2 {
		t.Fatalf("opening trash should show the cached/pending mails, got %v", visUIDs(m))
	}
}

func TestMoveManyRetriesTimeoutThenSucceeds(t *testing.T) {
	m, mem := memModel(t, 3)
	f := &timeoutOnceMove{Memory: mem, fails: 1}
	m.accts[0].B = f
	m.onKey(key("d"))
	drainOps(t, m)
	if f.calls < 2 {
		t.Fatalf("timed-out MOVE should be retried, calls=%d", f.calls)
	}
	if serverCount(t, mem, "INBOX") != 2 || serverCount(t, mem, "Trash") != 1 {
		t.Fatalf("mail should end in trash after retry, inbox=%d trash=%d", serverCount(t, mem, "INBOX"), serverCount(t, mem, "Trash"))
	}
}

func TestListPageRetriesTimeout(t *testing.T) {
	m, mem := memModel(t, 3)
	f := &timeoutOnceList{Memory: mem, fails: 1}
	m.accts[0].B = f
	cmd := m.loadListChunk(0, "Trash", 0, false, "", 0, m.accts[0].syncGen)
	if cmd == nil {
		t.Fatal("no load")
	}
	_, _ = m.Update(cmd())
	if f.calls < 2 {
		t.Fatalf("timed-out list page should be retried, calls=%d", f.calls)
	}
	if len(m.toasts.Actions()) != 0 {
		t.Fatalf("successful retry should not toast an error: %+v", m.toasts.Actions())
	}
}

type timeoutOnceMove struct {
	*backend.Memory
	fails, calls int
}

func (f *timeoutOnceMove) MoveMany(ctx context.Context, folder string, uids []uint32, dest string) (map[uint32]uint32, error) {
	f.calls++
	if f.fails > 0 {
		f.fails--
		return nil, context.DeadlineExceeded
	}
	return f.Memory.MoveMany(ctx, folder, uids, dest)
}

type timeoutOnceList struct {
	*backend.Memory
	fails, calls int
}

func (f *timeoutOnceList) ListPage(ctx context.Context, folder string, offset, limit int) ([]backend.Envelope, int, error) {
	f.calls++
	if f.fails > 0 {
		f.fails--
		return nil, 0, context.DeadlineExceeded
	}
	return f.Memory.ListPage(ctx, folder, offset, limit)
}
