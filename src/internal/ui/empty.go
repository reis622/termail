package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/toast"
	"termail/internal/undo"
)

// Emptying the trash or spam folder (Shift+D). Asked once, confirmed only
// with "e", runs in batches with a progress toast, not undoable.

type emptyCountMsg struct {
	acct   int
	folder string
	n      int
	err    error
}

type emptyProgressMsg struct {
	acct        int
	folder      string
	done, total int
}

type emptyDoneMsg struct {
	acct   int
	folder string
	total  int
	err    error
}

type emptyJob struct {
	acct    int
	folder  string
	toastID int
}

// emptyable reports whether a folder is a trash or spam folder (by the
// server's special-use mark or a well-known name like "[Gmail]/Trash",
// "Çöp Kutusu", "Deleted Items", "Junk").
func emptyable(f *backend.Folder) bool {
	if f == nil {
		return false
	}
	switch f.Role {
	case backend.RoleTrash, backend.RoleJunk:
		return true
	case backend.RoleNone:
		r := backend.RoleByName(f.Name)
		return r == backend.RoleTrash || r == backend.RoleJunk
	}
	return false
}

// locativeKi builds "Çöp Kutusu'ndaki" / "Spam'deki" / "Gereksiz'deki" /
// "Deleted Items'taki" for a folder name.
func locativeKi(name string) string {
	switch strings.ToLower(name) {
	case "çöp kutusu":
		return name + "'ndaki"
	case "spam":
		return name + "'deki" // said "spem"
	}
	lower := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "I", "ı"), "İ", "i"))
	back := true
	for _, r := range lower {
		switch r {
		case 'a', 'ı', 'o', 'u':
			back = true
		case 'e', 'i', 'ö', 'ü':
			back = false
		}
	}
	v := "e"
	if back {
		v = "a"
	}
	d := "d"
	if r := []rune(lower); len(r) > 0 && strings.ContainsRune("fstkçşhp", r[len(r)-1]) {
		d = "t"
	}
	return name + "'" + d + v + "ki"
}

// emptyQuestion is the confirmation text.
func emptyQuestion(label string, n int) string {
	if n == 1 {
		return fmt.Sprintf("%s 1 ileti kalıcı silinsin mi? e/h", locativeKi(label))
	}
	return fmt.Sprintf("%s %d iletinin hepsi kalıcı silinsin mi? e/h", locativeKi(label), n)
}

// askEmpty handles Shift+D in the list.
func (m *Model) askEmpty() tea.Cmd {
	if m.virt != viewFolder {
		return nil // Tüm Gelen Kutuları / Kaydedilenler: nothing to empty
	}
	a := m.acct()
	if a == nil || a.B == nil {
		return nil
	}
	f := a.folder(m.folder)
	if !emptyable(f) {
		m.toasts.Push(toast.Info, "", "Sadece Çöp Kutusu ve Spam'de", m.opt.Now(), 3*time.Second)
		return nil
	}
	if m.emptying != nil {
		m.toastInfo(toast.Info, "", "Boşaltma sürüyor…")
		return nil
	}
	ai, folder, b := m.cur, f.Name, a.B
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		n, err := b.Count(ctx, folder)
		return emptyCountMsg{acct: ai, folder: folder, n: n, err: err}
	}
}

func (m *Model) onEmptyCount(msg emptyCountMsg) tea.Cmd {
	if msg.err != nil {
		m.toastErr("empty", msg.err)
		return nil
	}
	if msg.acct >= len(m.accts) {
		return nil
	}
	a := m.accts[msg.acct]
	label := msg.folder
	if f := a.folder(msg.folder); f != nil {
		label = strings.TrimSpace(f.Label())
	}
	if msg.n == 0 {
		m.toasts.Push(toast.Info, "", label+" zaten boş.", m.opt.Now(), 3*time.Second)
		return nil
	}
	m.confirm = &confirmBox{strict: true, title: "Hepsini kalıcı sil",
		text: emptyQuestion(label, msg.n) + "\n\nBu işlem geri alınamaz.",
		yes:  "Evet, hepsini sil", onYes: func() tea.Cmd { return m.startEmpty(msg.acct, msg.folder, msg.n) }}
	return nil
}

func (m *Model) startEmpty(ai int, folder string, n int) tea.Cmd {
	b := m.accts[ai].B
	id := m.toasts.Push(toast.Info, "", fmt.Sprintf("Siliniyor… 0/%d", n), m.opt.Now(), 10*time.Minute)
	m.emptying = &emptyJob{acct: ai, folder: folder, toastID: id}
	events := m.events
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 10*time.Minute)
		defer cancel()
		err := b.Empty(ctx, folder, func(done, total int) {
			select {
			case events <- emptyProgressMsg{acct: ai, folder: folder, done: done, total: total}:
			default: // never block the worker on the UI
			}
		})
		return emptyDoneMsg{acct: ai, folder: folder, total: n, err: err}
	}
}

func (m *Model) onEmptyProgress(msg emptyProgressMsg) {
	if j := m.emptying; j != nil && j.acct == msg.acct && j.folder == msg.folder {
		m.toasts.Update(j.toastID, "", fmt.Sprintf("Siliniyor… %d/%d", msg.done, msg.total), m.opt.Now(), 10*time.Minute)
	}
}

func (m *Model) onEmptyDone(msg emptyDoneMsg) tea.Cmd {
	if j := m.emptying; j != nil {
		m.toasts.Remove(j.toastID)
	}
	m.emptying = nil
	if msg.acct >= len(m.accts) {
		return nil
	}
	a := m.accts[msg.acct]
	if msg.err != nil {
		m.logErr("empty", msg.err)
		m.toasts.Push(toast.Error, "", "Boşaltılamadı: "+friendly(msg.err), m.opt.Now(), 6*time.Second)
		return m.loadList(msg.acct, msg.folder, false)
	}
	// deletes that could have been undone from here are gone for good
	m.undo.DropWhere(func(e undo.Entry) bool { return e.Account == msg.acct && e.Trash == msg.folder })
	for id, tid := range m.undoToasts {
		if !m.undo.Has(id) {
			m.toasts.Remove(tid)
			delete(m.undoToasts, id)
		}
	}
	a.Lists[msg.folder] = []backend.Envelope{}
	var kept []backend.Envelope
	for _, x := range a.Saved {
		if x.Folder != msg.folder {
			kept = append(kept, x)
		}
	}
	a.Saved = kept
	if f := a.folder(msg.folder); f != nil {
		f.Total, f.Unread = 0, 0
	}
	m.clampCursor()
	label := msg.folder
	if f := a.folder(msg.folder); f != nil {
		label = strings.TrimSpace(f.Label())
	}
	m.toasts.Push(toast.Success, "Boşaltıldı:", label, m.opt.Now(), 4*time.Second)
	return tea.Batch(m.loadList(msg.acct, msg.folder, false), m.previewCmd())
}
