package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/i18n"
	"termail/internal/toast"
)

// Shift+D in Gelen Kutusu / Okunanlar (one account or Tüm Gelen
// Kutuları): every mail of the view goes to the trash. Asked once in a red
// box, confirmed only with "e" (Enter cancels), moved in batches through the
// ops queue (UID MOVE), rolled back mail by mail on failure, undone with u.
// Never a permanent delete.

type bulkItem struct {
	acct      int
	from      string
	trash     string
	env       backend.Envelope
	newUID    uint32 // UID in the trash once moved (0: unknown)
	done, bad bool
}

type bulkDel struct {
	items        map[foldKey]*bulkItem // by account, inbox and original UID
	total        int
	done, failed int
	toastID      int
	undoTop      int  // undo stack top when it ran: u undoes the bulk move only while nothing newer is on it
	restore      bool // moving from trash back to the inbox
}

// bulkGroup is one account's share of the view.
type bulkGroup struct {
	acct  int
	from  string
	trash string
	envs  []backend.Envelope
}

// bulkQuestion is the confirmation text.
func bulkQuestion(view string, n int) string {
	if i18n.Get() == i18n.EN {
		return i18n.Tf("%[1]s %[2]d iletinin TAMAMI silinecek. Emin misin? (e/h)", view, n)
	}
	loc := map[string]string{i18n.T("Gelen Kutusu"): i18n.T("Gelen Kutusu'ndaki"), unifiedTitle(): i18n.T("Tüm Gelen Kutuları'ndaki"), readTitle(): i18n.T("Okunanlar'daki"), savedTitle(): i18n.T("Kaydedilenler'deki")}[view]
	if loc == "" {
		loc = locativeKi(view)
	}
	return fmt.Sprintf("%s %d iletinin TAMAMI silinecek. Emin misin? (e/h)", loc, n)
}

func (m *Model) bulkViewName() string {
	switch {
	case m.virt == viewUnified && m.readView:
		return readTitle()
	case m.virt == viewUnified:
		return unifiedTitle()
	case m.virt == viewSaved:
		return savedTitle()
	}
	if a := m.acct(); a != nil {
		if f := a.folder(m.folder); f != nil {
			if lab := strings.TrimSpace(f.Label()); lab != "" {
				return lab
			}
		}
	}
	if m.folder != "" {
		return m.folder
	}
	return i18n.T("Klasör")
}

// askBulkDelete handles Shift+D in an inbox view.
func (m *Model) askBulkDelete() tea.Cmd {
	if m.bulk != nil && m.bulk.done+m.bulk.failed < m.bulk.total {
		m.toastInfo(toast.Info, "", i18n.T("Silme sürüyor…"))
		return nil
	}
	var groups []bulkGroup
	byAcct := map[int]int{}
	for _, e := range m.baseList() {
		ai, fo := m.loc(e)
		if ai >= len(m.accts) || e.UID == 0 || isPlaceholder(e.UID) {
			continue
		}
		gi, ok := byAcct[ai]
		if !ok {
			gi = len(groups)
			byAcct[ai] = gi
			groups = append(groups, bulkGroup{acct: ai, from: fo, trash: m.accts[ai].folderByRole(backend.RoleTrash)})
		}
		groups[gi].envs = append(groups[gi].envs, e)
	}
	view := m.bulkViewName()
	if len(groups) == 0 {
		m.toasts.Push(toast.Info, "", view+i18n.T(" zaten boş."), m.opt.Now(), 3*time.Second)
		return nil
	}
	n := 0
	var lines []string
	for _, g := range groups {
		a := m.accts[g.acct]
		if g.trash == "" {
			m.toastInfo(toast.Error, "", a.Cfg.Email+i18n.T(": Çöp Kutusu bulunamadı, hiçbir şey silinmedi."))
			return nil
		}
		if a.B == nil {
			m.toastInfo(toast.Error, "", a.Cfg.Email+i18n.T(" bağlı değil, hiçbir şey silinmedi."))
			return nil
		}
		n += len(g.envs)
		lines = append(lines, fmt.Sprintf("• %s: %d", a.Cfg.Email, len(g.envs)))
	}
	text := bulkQuestion(view, n)
	if m.virt == viewUnified {
		text += "\n\n" + strings.Join(lines, "\n")
	}
	text += "\n\nİletiler Çöp Kutusu'na taşınır, u ile geri alınabilir."
	m.confirm = &confirmBox{strict: true, title: "Hepsini sil", text: text,
		yes: i18n.T("Evet, hepsini sil"), onYes: func() tea.Cmd { return m.startBulkDelete(groups) }}
	return nil
}

// startBulkDelete removes the mails from the list and queues their moves
// (the ops queue sends them as batched UID MOVEs).
func (m *Model) startBulkDelete(groups []bulkGroup) tea.Cmd {
	b := &bulkDel{items: map[foldKey]*bulkItem{}}
	if top, ok := m.undo.Peek(); ok {
		b.undoTop = top.ID
	}
	for _, g := range groups {
		for _, e := range g.envs {
			b.items[foldKey{g.acct, g.from, e.UID}] = &bulkItem{acct: g.acct, from: g.from, trash: g.trash, env: e}
			b.total++
		}
	}
	label := fmt.Sprintf("Siliniyor 0/%d", b.total)
	if b.restore {
		label = i18n.Tf("Geri yükleniyor 0/%d", b.total)
	}
	b.toastID = m.toasts.Push(toast.Info, "", label, m.opt.Now(), 10*time.Minute)
	m.bulk = b
	var cmds []tea.Cmd
	for _, g := range groups {
		for _, e := range g.envs {
			m.removeEnv(g.acct, g.from, e.UID)
			m.toasts.MarkRead(g.acct, g.from, e.UID)
			cmds = append(cmds, m.queueMove(g.acct, g.from, e, g.trash, "bulk", 0))
		}
	}
	m.cursor, m.offset = 0, 0
	cmds = append(cmds, m.previewCmd())
	return tea.Batch(cmds...)
}

// startBulkRestore moves trashed mails back to each account's inbox.
func (m *Model) startBulkRestore(groups []bulkGroup) tea.Cmd {
	b := &bulkDel{items: map[foldKey]*bulkItem{}, restore: true}
	if top, ok := m.undo.Peek(); ok {
		b.undoTop = top.ID
	}
	for _, g := range groups {
		for _, e := range g.envs {
			b.items[foldKey{g.acct, g.from, e.UID}] = &bulkItem{acct: g.acct, from: g.from, trash: g.trash, env: e}
			b.total++
		}
	}
	b.toastID = m.toasts.Push(toast.Info, "", i18n.Tf("Geri yükleniyor 0/%d", b.total), m.opt.Now(), 10*time.Minute)
	m.bulk = b
	var cmds []tea.Cmd
	for _, g := range groups {
		for _, e := range g.envs {
			m.removeEnv(g.acct, g.from, e.UID)
			m.toasts.MarkRead(g.acct, g.from, e.UID)
			cmds = append(cmds, m.queueMove(g.acct, g.from, e, g.trash, "bulk", 0))
		}
	}
	m.cursor, m.offset = 0, 0
	cmds = append(cmds, m.previewCmd())
	return tea.Batch(cmds...)
}

// onBulkMoved records one mail's result (called from the ops batch; a
// failed one is already back in its list).
func (m *Model) onBulkMoved(ai int, from string, uid, newUID uint32, err error) {
	b := m.bulk
	if b == nil {
		return
	}
	it := b.items[foldKey{ai, from, uid}]
	if it == nil || it.done || it.bad {
		return
	}
	if err != nil {
		it.bad = true
		b.failed++
	} else {
		it.done, it.newUID = true, newUID
		b.done++
	}
}

// bulkProgress updates the toast after a batch.
func (m *Model) bulkProgress() {
	b := m.bulk
	if b == nil {
		return
	}
	now := m.opt.Now()
	if b.done+b.failed < b.total {
		prog := "Siliniyor"
		if b.restore {
			prog = i18n.T("Geri yükleniyor")
		}
		m.toasts.Update(b.toastID, "", fmt.Sprintf("%s %d/%d", prog, b.done+b.failed, b.total), now, 10*time.Minute)
		return
	}
	m.toasts.Remove(b.toastID)
	switch {
	case b.done == 0:
		m.toasts.Push(toast.Error, "", i18n.T("Hiçbir ileti taşınamadı, hepsi yerinde."), now, 6*time.Second)
		m.bulk = nil
	case b.restore && b.failed > 0:
		b.toastID = m.toasts.Push(toast.Deleted, i18n.T("Geri yüklendi:"), i18n.Tf("%d ileti Gelen Kutusu'nda, %d ileti taşınamadı (yerinde)", b.done, b.failed), now, 0)
	case b.restore:
		b.toastID = m.toasts.Push(toast.Deleted, i18n.T("Geri yüklendi:"), i18n.Tf("%d ileti Gelen Kutusu'na taşındı", b.done), now, 0)
	case b.failed > 0:
		b.toastID = m.toasts.Push(toast.Deleted, i18n.T("Silindi:"), i18n.Tf("%d ileti Çöp Kutusu'nda, %d ileti taşınamadı (yerinde)", b.done, b.failed), now, 0)
	default:
		b.toastID = m.toasts.Push(toast.Deleted, i18n.T("Silindi:"), i18n.Tf("%d ileti Çöp Kutusu'na taşındı", b.done), now, 0)
	}
}

// bulkUndoable: u should undo the bulk move (nothing newer on the stack).
func (m *Model) bulkUndoable() bool {
	if m.bulk == nil {
		return false
	}
	top := 0
	if t, ok := m.undo.Peek(); ok {
		top = t.ID
	}
	return top == m.bulk.undoTop
}

type bulkUndoDoneMsg struct {
	accts map[int][]string // folders to reload
	n     int
	err   error
}

// undoBulk moves the bulk-deleted mails back from the trash.
func (m *Model) undoBulk() tea.Cmd {
	b := m.bulk
	if b.done+b.failed < b.total {
		m.toastInfo(toast.Info, "", i18n.T("İşlem sürüyor, birazdan tekrar dene."))
		return nil
	}
	m.bulk = nil
	m.toasts.Remove(b.toastID)
	type key struct {
		acct        int
		trash, from string
	}
	uids := map[key][]uint32{}
	var lost []*bulkItem // no UID from the server: found by Message-ID
	reload := map[int][]string{}
	n := 0
	for _, it := range b.items {
		if !it.done {
			continue
		}
		n++
		m.insertEnv(it.acct, it.from, it.env) // back at once; reloaded with the new UIDs after
		k := key{it.acct, it.trash, it.from}
		if it.newUID != 0 {
			uids[k] = append(uids[k], it.newUID)
		} else {
			lost = append(lost, it)
		}
		if _, ok := uids[k]; !ok {
			uids[k] = nil
		}
	}
	for k := range uids {
		reload[k.acct] = append(reload[k.acct], k.from, k.trash)
	}
	m.toastInfo(toast.Success, "", i18n.Tf("Geri alınıyor: %d ileti", n))
	accts := m.accts
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
		defer cancel()
		var firstErr error
		for k, l := range uids {
			be := accts[k.acct].B
			if be == nil {
				firstErr = errNotConnected
				continue
			}
			if len(l) > 0 {
				if _, err := be.MoveMany(ctx, k.trash, l, k.from); err != nil && firstErr == nil {
					firstErr = err
				}
			}
		}
		for _, it := range lost {
			be := accts[it.acct].B
			if be == nil || it.env.MessageID == "" {
				continue
			}
			res, err := be.Search(ctx, it.trash, it.env.MessageID, 5)
			if err == nil && len(res) > 0 {
				_, err = be.Move(ctx, it.trash, res[0].UID, it.from, it.env.MessageID)
			}
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return bulkUndoDoneMsg{accts: reload, n: n, err: firstErr}
	}
}

func (m *Model) onBulkUndoDone(msg bulkUndoDoneMsg) tea.Cmd {
	if msg.err != nil {
		m.toastErr("bulk undo", msg.err)
	} else {
		m.toastInfo(toast.Success, "", i18n.Tf("Geri alındı: %d ileti", msg.n))
	}
	var cmds []tea.Cmd
	for ai, fs := range msg.accts {
		if ai >= len(m.accts) {
			continue
		}
		for _, f := range fs {
			if m.accts[ai].Loaded[f] {
				cmds = append(cmds, m.loadList(ai, f, false))
			}
		}
	}
	return tea.Batch(cmds...)
}

// isPlaceholder: the stand-in UID of a mail whose move is still queued.
func isPlaceholder(uid uint32) bool { return uid > 0xFFFFFFFF-1_000_000 }
