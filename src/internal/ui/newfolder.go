package ui

import (
	"context"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/edit"
	"termail/internal/toast"
)

// newFolderBox is the inline name field drawn under an account's folders.
type newFolderBox struct {
	acct int
	in   edit.Editor
	from string // set when renaming an existing folder
}

func (m *Model) startNewFolder() tea.Cmd {
	if m.login != nil || len(m.accts) == 0 {
		return nil
	}
	ai := -1
	rows := m.folderRowsSelectable()
	if m.fcursor >= 0 && m.fcursor < len(rows) {
		r := rows[m.fcursor]
		if r.kind == rowAcct || r.kind == rowFolder {
			ai = r.acct
		}
	}
	if ai < 0 || ai >= len(m.accts) {
		m.toastInfo(toast.Info, "", "Yeni klasör için solda bir hesabın satırını seç.")
		return nil
	}
	m.setOpen(ai, true)
	in := edit.New(edit.Single)
	in.Placeholder = "Klasör adı"
	in.Focus()
	m.newFolder = &newFolderBox{acct: ai, in: in}
	m.focus = focusFolders
	return nil
}

func (m *Model) newFolderKey(k tea.KeyMsg) tea.Cmd {
	if m.newFolder == nil {
		return nil
	}
	key := k.String()
	switch key {
	case "esc":
		m.newFolder = nil
		return nil
	case "enter":
		if m.newFolder != nil && m.newFolder.from != "" {
			return m.commitRenameFolder()
		}
		return m.commitNewFolder()
	}
	_, cmd := m.editKey(&m.newFolder.in, k)
	return cmd
}

func (m *Model) commitNewFolder() tea.Cmd {
	nf := m.newFolder
	if nf == nil {
		return nil
	}
	name := strings.TrimSpace(nf.in.Value())
	if name == "" {
		m.toastInfo(toast.Info, "", "Bir klasör adı yaz.")
		return nil
	}
	for _, r := range name {
		if r < 32 || unicode.IsControl(r) {
			m.toastInfo(toast.Error, "", "Klasör adı geçersiz.")
			return nil
		}
	}
	ai := nf.acct
	if ai < 0 || ai >= len(m.accts) || m.accts[ai].B == nil {
		m.toastInfo(toast.Error, "", "Hesap bağlı değil, klasör oluşturulamadı.")
		return nil
	}
	a := m.accts[ai]
	for _, f := range a.Folders {
		if strings.EqualFold(f.Name, name) {
			m.toastInfo(toast.Error, "", "Bu isimde bir klasör zaten var.")
			return nil
		}
	}
	// show it at once; loadFolders replaces the list when the server answers
	a.Folders = append(a.Folders, backend.Folder{Name: name, Role: backend.RoleByName(name)})
	m.newFolder = nil
	b := a.B
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		err := b.CreateFolder(ctx, name)
		return folderCreatedMsg{acct: ai, name: name, err: err}
	}
}

func (m *Model) removeOptimisticFolder(ai int, name string) {
	if ai < 0 || ai >= len(m.accts) {
		return
	}
	a := m.accts[ai]
	out := a.Folders[:0]
	for _, f := range a.Folders {
		if f.Name != name {
			out = append(out, f)
		}
	}
	a.Folders = out
	if m.folder == name && m.cur == ai {
		m.folder = a.inbox()
	}
}
