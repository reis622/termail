package ui

import (
	"testing"
	"time"

	"termail/internal/backend"
)

// previewModel: folder panel focused, INBOX loaded, Sent not loaded yet.
func previewModel(t *testing.T) (*Model, *backend.Memory) {
	m, mem := memModel(t, 3)
	mem.Add("Spam", backend.Envelope{From: "Reklam", Subject: "indirim", Date: time.Now()}, []byte("Subject: x\r\n\r\nx"))
	m.onKey(key("left"))
	if m.focus != focusFolders {
		t.Fatal("no folder focus")
	}
	return m, mem
}

func cursorTo(t *testing.T, m *Model, folder string) {
	t.Helper()
	for i, r := range m.folderRowsSelectable() {
		if r.kind == rowFolder && r.folder == folder {
			for m.fcursor < i {
				m.onKey(key("down"))
			}
			for m.fcursor > i {
				m.onKey(key("up"))
			}
			return
		}
	}
	t.Fatalf("no row %s", folder)
}

func TestFolderPreviewDebounceAndArrival(t *testing.T) {
	m, _ := previewModel(t)
	for f := range m.accts[0].Loaded {
		if f != "INBOX" {
			delete(m.accts[0].Loaded, f) // nothing cached: every move debounces
		}
	}
	// INBOX is already cached; rapid moves over uncached folders debounce.
	inbox := subjects(m.visible())
	var ticks []folderPreviewMsg
	for _, k := range []string{"down", "down", "up"} { // Spam, Trash, Spam
		if cmd := m.onKey(key(k)); cmd != nil {
			if msg, ok := cmd().(folderPreviewMsg); ok {
				ticks = append(ticks, msg)
			}
		}
	}
	if len(ticks) < 2 {
		t.Fatalf("need several moves, got %d", len(ticks))
	}
	if m.pendingF == nil || m.pendingF.folder != "Spam" {
		t.Fatal("not previewing Spam")
	}
	if got := subjects(m.visible()); len(got) != len(inbox) {
		t.Fatal("old list must stay in place while loading")
	}
	fetches := 0
	var load func() any
	for _, tk := range ticks {
		if cmd := m.onFolderPreview(tk); cmd != nil {
			fetches++
			c := cmd
			load = func() any { return c() }
		}
	}
	if fetches != 1 {
		t.Fatalf("%d fetches for rapid moves, want 1 (debounce)", fetches)
	}
	m.Update(load().(listMsg))
	if m.folder != "Spam" || m.pendingF != nil || len(m.visible()) != 1 || m.focus != focusFolders {
		t.Fatalf("arrival: folder=%s pending=%v n=%d focus=%v", m.folder, m.pendingF, len(m.visible()), m.focus)
	}
}

func TestFolderPreviewStaleResponseDiscarded(t *testing.T) {
	m, mem := previewModel(t)
	cursorTo(t, m, "Spam")
	first := m.pendingF
	m.listReq++
	first.req = m.listReq
	staleReq := first.req
	cursorTo(t, m, "Trash") // user moved on before Spam arrived
	envs, _ := mem.List(nil, "Spam", 10)
	m.Update(listMsg{acct: 0, folder: "Spam", envs: envs, req: staleReq})
	if m.folder == "Spam" {
		t.Fatal("stale answer switched the view")
	}
	if !m.accts[0].Loaded["Spam"] {
		t.Fatal("stale answer should still fill the cache")
	}
	// coming back to Spam is now an instant cache hit
	cursorTo(t, m, "Spam")
	if m.folder != "Spam" || m.pendingF != nil || len(m.visible()) != 1 {
		t.Fatal("cache hit not shown instantly")
	}
}

func TestFolderPreviewFocusAndNoMarkRead(t *testing.T) {
	m, _ := previewModel(t)
	cursorTo(t, m, "Trash") // loaded (empty) -> cache hit
	cursorTo(t, m, "INBOX")
	if m.focus != focusFolders || m.folder != "INBOX" {
		t.Fatal("preview moved focus")
	}
	// the highlighted first mail must not be marked read by browsing folders
	k := m.pv.key
	m.Update(seenTimerMsg{key: k, seq: m.selSeq})
	if e, _ := m.selected(); e.Seen {
		t.Fatal("folder browsing marked a mail read")
	}
	m.onKey(key("enter"))
	if m.focus != focusList || m.folder != "INBOX" {
		t.Fatal("Enter must step into the previewed list")
	}
	m.onKey(key("left"))
	cursorTo(t, m, "Spam")
	m.onKey(key("right"))
	if m.focus != focusList || m.folder != "Spam" || m.pendingF != nil {
		t.Fatal("Right must enter the (still loading) previewed folder")
	}
}
