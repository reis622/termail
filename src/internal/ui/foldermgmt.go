package ui

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/edit"
	"termail/internal/i18n"
	"termail/internal/toast"
	"termail/internal/undo"
)

type folderGoneMsg struct {
	acct int
	name string
	err  error
}

type folderRenamedMsg struct {
	acct     int
	old, neu string
	err      error
}

type copyDoneMsg struct {
	acct, undoID int
	from, dest   string
	env          backend.Envelope
	newUID       uint32
	err          error
}

func (m *Model) customFolderAtCursor() (frow, bool) {
	rows := m.folderRowsSelectable()
	if m.fcursor < 0 || m.fcursor >= len(rows) {
		return frow{}, false
	}
	r := rows[m.fcursor]
	if r.kind == rowFolder && r.custom {
		return r, true
	}
	return r, false
}

func (m *Model) askDeleteFolder(r frow) tea.Cmd {
	if r.kind != rowFolder || !r.custom {
		m.toastInfo(toast.Info, "", i18n.T("Sistem klasörleri silinemez."))
		return nil
	}
	label := strings.TrimSpace(r.label)
	if label == "" {
		label = r.folder
	}
	m.confirm = &confirmBox{
		title:  i18n.T("Klasörü sil"),
		text:   i18n.Tf("\"%s\" klasörü ve içindeki tüm iletiler kalıcı olarak silinecek. Bu geri alınamaz.", label),
		yes:    i18n.T("Sil"),
		strict: true,
		onYes:  func() tea.Cmd { return m.deleteFolder(r.acct, r.folder) },
	}
	return nil
}

func (m *Model) deleteFolder(ai int, name string) tea.Cmd {
	if ai < 0 || ai >= len(m.accts) || m.accts[ai].B == nil {
		return nil
	}
	a := m.accts[ai]
	var keep []backend.Folder
	for _, f := range a.Folders {
		if f.Name != name {
			keep = append(keep, f)
		}
	}
	a.Folders = keep
	delete(a.Lists, name)
	delete(a.Loaded, name)
	if m.virt == viewFolder && m.cur == ai && m.folder == name {
		m.virt = viewFolder
		m.folder = a.inbox()
		m.cursor, m.offset = 0, 0
	}
	m.syncFolderCursor()
	b := a.B
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
		defer cancel()
		return folderGoneMsg{acct: ai, name: name, err: b.DeleteFolder(ctx, name)}
	}
}

func (m *Model) startRenameFolder(r frow) tea.Cmd {
	if r.kind != rowFolder || !r.custom {
		m.toastInfo(toast.Info, "", i18n.T("Sistem klasörleri yeniden adlandırılamaz."))
		return nil
	}
	if r.acct < 0 || r.acct >= len(m.accts) {
		return nil
	}
	in := edit.New(edit.Single)
	val := strings.TrimSpace(r.label)
	if val == "" {
		val = r.folder
	}
	in.SetValue(val)
	in.Focus()
	m.newFolder = &newFolderBox{acct: r.acct, in: in, from: r.folder}
	m.focus = focusFolders
	return nil
}

func (m *Model) startNewFolderFor(ai int) tea.Cmd {
	if ai < 0 || ai >= len(m.accts) {
		return nil
	}
	m.setOpen(ai, true)
	in := edit.New(edit.Single)
	in.Placeholder = i18n.T("Klasör adı")
	in.Focus()
	m.newFolder = &newFolderBox{acct: ai, in: in}
	m.focus = focusFolders
	return nil
}

func (m *Model) commitRenameFolder() tea.Cmd {
	nf := m.newFolder
	if nf == nil || nf.from == "" {
		return m.commitNewFolder()
	}
	name := strings.TrimSpace(nf.in.Value())
	if name == "" {
		m.toastInfo(toast.Info, "", i18n.T("Bir klasör adı yaz."))
		return nil
	}
	for _, r := range name {
		if r < 32 || unicode.IsControl(r) {
			m.toastInfo(toast.Error, "", i18n.T("Klasör adı geçersiz."))
			return nil
		}
	}
	ai := nf.acct
	old := nf.from
	if ai < 0 || ai >= len(m.accts) || m.accts[ai].B == nil {
		m.newFolder = nil
		return nil
	}
	if name == old {
		m.newFolder = nil
		return nil
	}
	a := m.accts[ai]
	for _, f := range a.Folders {
		if f.Name == name {
			m.toastInfo(toast.Error, "", i18n.T("Bu adda bir klasör zaten var."))
			return nil
		}
	}
	m.newFolder = nil
	for i, f := range a.Folders {
		if f.Name == old {
			f.Name = name
			a.Folders[i] = f
		}
	}
	if l, ok := a.Lists[old]; ok {
		a.Lists[name] = l
		delete(a.Lists, old)
	}
	if a.Loaded[old] {
		a.Loaded[name] = true
		delete(a.Loaded, old)
	}
	if m.virt == viewFolder && m.cur == ai && m.folder == old {
		m.folder = name
	}
	b := a.B
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		return folderRenamedMsg{acct: ai, old: old, neu: name, err: b.RenameFolder(ctx, old, name)}
	}
}

func (m *Model) isInboxFolder(ai int, folder string) bool {
	if ai < 0 || ai >= len(m.accts) {
		return false
	}
	if folder == m.accts[ai].inbox() {
		return true
	}
	if f := m.accts[ai].folder(folder); f != nil && f.Role == backend.RoleInbox {
		return true
	}
	return backend.RoleByName(folder) == backend.RoleInbox
}

func isUserFolder(f backend.Folder) bool {
	return f.Role == backend.RoleNone && !backend.NonMailFolder(f.Name)
}

// copyToFolder copies a mail (IMAP COPY) and leaves the original in place.
func (m *Model) copyToFolder(ai int, folder string, e backend.Envelope, dest, label string) tea.Cmd {
	id := m.undo.Push(undo.Entry{Op: undo.OpCopy, Account: ai, From: folder, Trash: dest, OrigUID: e.UID, MessageID: e.MessageID, Subject: e.Subject, Data: e})
	m.linkUndoToast(id, m.toasts.Push(toast.Deleted, i18n.T("Kopyalandı:"), strings.TrimSpace(label), m.opt.Now(), 0))
	c := e
	c.UID = placeholderUID(id)
	m.insertEnv(ai, dest, c)
	if ai >= len(m.accts) || m.accts[ai].B == nil {
		return nil
	}
	b := m.accts[ai].B
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
		defer cancel()
		newUID, err := b.Copy(ctx, folder, e.UID, dest, e.MessageID)
		return copyDoneMsg{acct: ai, undoID: id, from: folder, dest: dest, env: e, newUID: newUID, err: err}
	}
}

func (m *Model) onCopyDone(d copyDoneMsg) tea.Cmd {
	if d.err != nil {
		if d.undoID != 0 {
			m.undo.Drop(d.undoID)
			m.dismissUndoToast(d.undoID)
		}
		m.removeEnv(d.acct, d.dest, placeholderUID(d.undoID))
		m.toastErr("copy", d.err)
		return nil
	}
	if d.newUID != 0 {
		m.retargetUID(d.acct, d.dest, placeholderUID(d.undoID), d.newUID)
		if d.undoID != 0 {
			m.undo.Confirm(d.undoID, d.newUID)
		}
	} else if d.undoID != 0 {
		m.undo.Confirm(d.undoID, 0)
	}
	return nil
}

func (m *Model) undoCopy(e undo.Entry) tea.Cmd {
	if e.Account >= len(m.accts) {
		return nil
	}
	uid := e.TrashUID
	if uid != 0 {
		m.removeEnv(e.Account, e.Trash, uid)
	} else if env, ok := e.Data.(backend.Envelope); ok {
		m.removeEnv(e.Account, e.Trash, placeholderUID(e.ID))
		_ = env
	}
	b := m.accts[e.Account].B
	if b == nil {
		return nil
	}
	dest := e.Trash
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
		defer cancel()
		uid := e.TrashUID
		if uid == 0 && e.MessageID != "" {
			res, err := b.Search(ctx, dest, e.MessageID, 5)
			if err == nil && len(res) > 0 {
				uid = res[0].UID
			}
		}
		if uid == 0 {
			return undoDoneMsg{e: e, err: errors.New(i18n.T("ileti bulunamadı"))}
		}
		return undoDoneMsg{e: e, err: b.Delete(ctx, dest, uid)}
	}
}
