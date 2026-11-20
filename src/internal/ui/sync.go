package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
)

type syncDeltaMsg struct {
	acct   int
	folder string
	delta  backend.SyncDelta
	err    error
}

// pipelinePages starts up to two overlapping header pages from off, skipping
// offsets that were already requested (so a late first-of-pair cannot
// re-fetch a page the prefetch already started).
func (m *Model) pipelinePages(ai int, folder, kind string, off, total, gen int) []tea.Cmd {
	if ai >= len(m.accts) {
		return nil
	}
	a := m.accts[ai]
	if a.pageQueued == nil {
		a.pageQueued = map[string]int{}
	}
	k := pageKey(folder, kind)
	var cmds []tea.Cmd
	n := 0
	for n < 2 {
		if total > 0 && off >= total {
			break
		}
		if off < a.pageQueued[k] {
			off += listBatch
			continue
		}
		a.pageQueued[k] = off + listBatch
		cmds = append(cmds, m.loadListChunk(ai, folder, off, false, kind, 0, gen))
		off += listBatch
		n++
		if total == 0 {
			break
		}
	}
	return cmds
}

func (a *Acct) knownUIDs(folder string) []uint32 {
	l := a.Lists[folder]
	out := make([]uint32, 0, len(l))
	for _, e := range l {
		if e.UID != 0 && e.UID < 0xFF000000 {
			out = append(out, e.UID)
		}
	}
	return out
}

func (a *Acct) hasSyncState(folder string) bool {
	if a.folderSync == nil {
		return false
	}
	s := a.folderSync[folder]
	return s.UIDValidity != 0 || s.UIDNext != 0 || s.HighestModSeq != 0
}

func (m *Model) captureFolderSync(ai int, folder string) {
	if ai >= len(m.accts) {
		return
	}
	a := m.accts[ai]
	s, ok := a.B.(backend.IncrementalSyncer)
	if !ok {
		return
	}
	if a.folderSync == nil {
		a.folderSync = map[string]backend.FolderSync{}
	}
	st := s.LastFolderSync(folder)
	if st.Folder == "" {
		st.Folder = folder
	}
	a.folderSync[folder] = st
}

// syncOrLoadList applies a CONDSTORE/UID-SEARCH delta when a cached folder
// and bookmark exist; otherwise it starts a paged load.
func (m *Model) syncOrLoadList(i int, folder string) tea.Cmd {
	if i >= len(m.accts) || m.accts[i].B == nil {
		return nil
	}
	a := m.accts[i]
	s, ok := a.B.(backend.IncrementalSyncer)
	if !ok || !a.Loaded[folder] || len(a.Lists[folder]) == 0 || !a.hasSyncState(folder) {
		return m.startListLoad(i, folder, false, 0)
	}
	state := a.folderSync[folder]
	state.Folder = folder
	known := a.knownUIDs(folder)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 45*time.Second)
		defer cancel()
		d, err := s.SyncFolder(ctx, state, known)
		return syncDeltaMsg{acct: i, folder: folder, delta: d, err: err}
	}
}

func (m *Model) onSyncDelta(msg syncDeltaMsg) tea.Cmd {
	if msg.acct >= len(m.accts) {
		return nil
	}
	a := m.accts[msg.acct]
	if msg.err != nil {
		m.logErr("sync", msg.err)
		return m.startListLoad(msg.acct, msg.folder, true, 0) // quiet refresh
	}
	if a.folderSync == nil {
		a.folderSync = map[string]backend.FolderSync{}
	}
	a.folderSync[msg.folder] = msg.delta.Sync
	if msg.delta.FullReset {
		a.Lists[msg.folder] = nil
		a.Loaded[msg.folder] = false
		a.fromCache = false
		return m.startListLoad(msg.acct, msg.folder, false, 0)
	}
	changed := false
	if len(msg.delta.Vanished) > 0 {
		gone := map[uint32]bool{}
		for _, u := range msg.delta.Vanished {
			gone[u] = true
		}
		var kept []backend.Envelope
		f := a.folder(msg.folder)
		for _, e := range a.Lists[msg.folder] {
			if gone[e.UID] {
				changed = true
				if f != nil && f.Total > 0 {
					f.Total--
					if !e.Seen && f.Unread > 0 {
						f.Unread--
					}
				}
				continue
			}
			kept = append(kept, e)
		}
		a.Lists[msg.folder] = kept
	}
	if len(msg.delta.Flags) > 0 {
		by := map[uint32]backend.Envelope{}
		for _, e := range msg.delta.Flags {
			by[e.UID] = e
		}
		for i := range a.Lists[msg.folder] {
			e := &a.Lists[msg.folder][i]
			if u, ok := by[e.UID]; ok {
				if e.Seen != u.Seen {
					if f := a.folder(msg.folder); f != nil {
						if u.Seen && f.Unread > 0 {
							f.Unread--
						} else if !u.Seen {
							f.Unread++
						}
					}
					if msg.folder == a.inbox() {
						if u.Seen {
							a.adjustExpect(msg.folder, "unseen", -1)
							a.adjustExpect(msg.folder, "seen", 1)
						} else {
							a.adjustExpect(msg.folder, "unseen", 1)
							a.adjustExpect(msg.folder, "seen", -1)
						}
					}
				}
				e.Seen, e.Flagged = u.Seen, u.Flagged
				changed = true
			}
		}
	}
	if len(msg.delta.New) > 0 {
		a.Lists[msg.folder] = mergeEnvs(a.Lists[msg.folder], m.overlayPending(msg.acct, msg.folder, msg.delta.New))
		if f := a.folder(msg.folder); f != nil {
			for _, e := range msg.delta.New {
				f.Total++
				if !e.Seen {
					f.Unread++
				}
			}
		}
		changed = true
	}
	a.Loaded[msg.folder] = true
	a.fromCache = false
	a.paging = false
	if msg.folder == a.inbox() && changed {
		unseen, seen := 0, 0
		for _, e := range a.Lists[msg.folder] {
			if e.Seen {
				seen++
			} else {
				unseen++
			}
		}
		a.setExpect(msg.folder, "unseen", unseen)
		a.setExpect(msg.folder, "seen", seen)
		m.saveInboxCache(msg.acct)
	}
	m.clampCursor()
	return m.previewCmd()
}
