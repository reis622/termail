package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/toast"
)

// "Kaydedilenler": mail saved for later = the server's \Flagged flag
// (the same as the star in Gmail and the flag in Outlook).

const savedTitle = "Kaydedilenler"

type savedListMsg struct {
	acct  int
	envs  []backend.Envelope
	err   error
	asked time.Time
}

// savedFolders picks where an account's flagged mail is searched: the
// server's own "flagged" folder if it has one (Gmail: Yıldızlı), else the
// all-mail folder, else the inbox, archive, sent and the user's own folders.
func savedFolders(a *Acct) []string {
	if f := a.folderByRole(backend.RoleFlagged); f != "" {
		return []string{f}
	}
	if f := a.folderByRole(backend.RoleAll); f != "" {
		return []string{f}
	}
	out := []string{a.inbox()}
	for _, r := range []backend.Role{backend.RoleArchive, backend.RoleSent} {
		if f := a.folderByRole(r); f != "" {
			out = append(out, f)
		}
	}
	for _, f := range a.Folders {
		if f.Role == backend.RoleNone {
			out = append(out, f.Name)
		}
	}
	return out
}

// loadSaved fetches one account's flagged mail in the background.
func (m *Model) loadSaved(ai int) tea.Cmd {
	if ai >= len(m.accts) || m.accts[ai].B == nil || len(m.accts[ai].Folders) == 0 {
		return nil
	}
	a := m.accts[ai]
	b, folders := a.B, savedFolders(a)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
		defer cancel()
		asked := time.Now()
		var all []backend.Envelope
		var firstErr error
		for _, f := range folders {
			envs, err := b.Flagged(ctx, f, 300)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			for _, e := range envs {
				if e.Folder == "" { // a backend may report the real folder itself
					e.Folder = f
				}
				all = append(all, e)
			}
		}
		if len(all) == 0 && firstErr != nil {
			return savedListMsg{acct: ai, err: firstErr}
		}
		return savedListMsg{acct: ai, envs: all, asked: asked}
	}
}

func (m *Model) onSavedList(msg savedListMsg) tea.Cmd {
	if msg.acct >= len(m.accts) {
		return nil
	}
	if msg.err != nil {
		m.logErr("saved", msg.err)
		return nil
	}
	var keep backend.Envelope
	has := false
	if m.virt == viewSaved {
		keep, has = m.selected()
	}
	a := m.accts[msg.acct]
	a.Saved, a.SavedOK = m.mergeSavedSnapshot(msg.acct, msg.envs, msg.asked), true
	if has {
		for i, e := range m.visible() {
			if e.Acct == keep.Acct && e.Folder == keep.Folder && e.UID == keep.UID {
				m.cursor = i
			}
		}
	}
	m.clampCursor()
	if m.virt == viewSaved {
		return m.previewCmd()
	}
	return nil
}

func savedCount(accts []*Acct) int { return len(mergeSaved(accts)) }

// toggleSaved flips the saved state at once and tells the server; a failure
// rolls the change back (see flagDoneMsg).
func (m *Model) toggleSaved(ai int, folder string, e backend.Envelope) tea.Cmd {
	on := !e.Flagged
	m.setFlaggedLocal(ai, folder, e, on)
	if on {
		m.toasts.Push(toast.Success, "", "Kaydedildi", m.opt.Now(), 3*time.Second)
	} else {
		m.toasts.Push(toast.Info, "", "Kayıt kaldırıldı", m.opt.Now(), 3*time.Second)
	}
	return m.queueFlagUndo(ai, folder, e, backend.FlagFlagged, on)
}

// setFlaggedLocal updates every copy of the mail the UI holds: folder lists
// (matched by UID in its folder or by Message-ID elsewhere) and the saved list.
func (m *Model) setFlaggedLocal(ai int, folder string, e backend.Envelope, on bool) {
	a := m.accts[ai]
	same := func(x backend.Envelope, f string) bool {
		if f == folder && x.UID == e.UID {
			return true
		}
		return e.MessageID != "" && x.MessageID == e.MessageID
	}
	for f, l := range a.Lists {
		for i := range l {
			if same(l[i], f) {
				l[i].Flagged = on
			}
		}
	}
	if m.search != nil {
		for i := range m.search.server {
			if x := &m.search.server[i]; x.Acct == ai && same(*x, x.Folder) {
				x.Flagged = on
			}
		}
	}
	var kept []backend.Envelope
	for _, x := range a.Saved {
		if !same(x, x.Folder) {
			kept = append(kept, x)
		}
	}
	if on {
		c := e
		c.Flagged, c.Folder, c.Acct = true, folder, ai
		kept = append(kept, c)
	}
	a.Saved = kept
	if m.virt == viewSaved {
		m.clampCursor()
	}
}

// setFlagEnv is setFlag that remembers enough to undo the local change.
func (m *Model) setFlagEnv(ai int, folder string, e backend.Envelope, f backend.Flag, on bool) tea.Cmd {
	return m.queueFlag(ai, folder, e, f, on)
}

// onFlagDone rolls an optimistic flag change back when the server refused it.
func (m *Model) onFlagDone(msg flagDoneMsg) tea.Cmd {
	if msg.err == nil {
		return nil
	}
	m.logErr("flag", msg.err)
	if !msg.has || msg.acct >= len(m.accts) {
		m.toastErr("flag", msg.err)
		return nil
	}
	switch msg.flag {
	case backend.FlagFlagged:
		m.setFlaggedLocal(msg.acct, msg.folder, msg.env, !msg.on)
		what := "Kaydedilemedi"
		if !msg.on {
			what = "Kayıt kaldırılamadı"
		}
		m.toasts.Push(toast.Error, "", what+": "+friendly(msg.err), m.opt.Now(), 6*time.Second)
	case backend.FlagSeen:
		m.setSeenLocal(msg.acct, msg.folder, msg.env.UID, !msg.on)
		what := "Okundu yapılamadı"
		if !msg.on {
			what = "Okunmadı yapılamadı"
		}
		m.toasts.Push(toast.Error, "", what+": "+friendly(msg.err), m.opt.Now(), 6*time.Second)
	}
	return nil
}
