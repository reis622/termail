package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
)

// startListLoad begins a newest-first progressive load of a folder.
// Cached envelopes already on screen are kept and merged as pages arrive.
func (m *Model) startListLoad(i int, folder string, check bool, req int) tea.Cmd {
	if i >= len(m.accts) || m.accts[i].B == nil {
		return nil
	}
	a := m.accts[i]
	kind := m.listKind(i, folder)
	if check {
		// A quiet refresh (Watch) must not abort an in-flight paged load
		// or skip later pages — that left a 3680-mail inbox stuck on the
		// first 250 until an account switch.
		if a.paging {
			a.needRefresh = true
			return nil
		}
		return m.loadListChunk(i, folder, 0, true, kind, req, a.syncGen)
	}
	a.syncGen++
	a.paging = true
	a.pageFolder, a.pageKind = folder, kind
	a.needRefresh = false
	if a.pageOff == nil {
		a.pageOff = map[string]int{}
	}
	if a.pageQueued == nil {
		a.pageQueued = map[string]int{}
	}
	a.pageOff[pageKey(folder, kind)] = 0
	a.pageQueued[pageKey(folder, kind)] = 0
	return m.loadListChunk(i, folder, 0, false, kind, req, a.syncGen)
}

func (m *Model) loadListChunk(i int, folder string, offset int, check bool, kind string, req, gen int) tea.Cmd {
	if i >= len(m.accts) || m.accts[i].B == nil {
		return nil
	}
	b := m.accts[i].B
	return func() tea.Msg {
		asked := time.Now()
		var (
			envs  []backend.Envelope
			total int
			err   error
		)
		for attempt, to := range []time.Duration{20 * time.Second, 45 * time.Second} {
			ctx, cancel := context.WithTimeout(m.ctx, to)
			if p, ok := b.(backend.PagedLister); ok {
				switch kind {
				case "unseen", "seen":
					envs, total, err = p.ListFlagPage(ctx, folder, kind == "unseen", offset, listBatch)
				default:
					envs, total, err = p.ListPage(ctx, folder, offset, listBatch)
				}
			} else {
				limit := offset + listBatch
				if limit < listBatch {
					limit = listBatch
				}
				envs, err = b.List(ctx, folder, limit)
				if offset > 0 && offset < len(envs) {
					envs = envs[offset:]
				}
				total = offset + len(envs)
				if len(envs) == listBatch {
					total++ // at least one more
				}
			}
			cancel()
			if err == nil || !isTimeout(err) || attempt == 1 {
				break
			}
		}
		return listMsg{
			acct: i, folder: folder, envs: envs, err: err,
			check: check, page: true, req: req, asked: asked,
			offset: offset, total: total, kind: kind, gen: gen,
		}
	}
}

// mergeEnvs unions neu into old by UID (neu wins for flags/headers, ReadAt
// is kept when the incoming page does not have one) and sorts newest first.
func mergeEnvs(old, neu []backend.Envelope) []backend.Envelope {
	byUID := make(map[uint32]int, len(old)+len(neu))
	out := append([]backend.Envelope(nil), old...)
	for i, e := range out {
		byUID[e.UID] = i
	}
	for _, e := range neu {
		if i, ok := byUID[e.UID]; ok {
			rt := out[i].ReadAt
			out[i] = e
			if e.ReadAt.IsZero() && !rt.IsZero() {
				out[i].ReadAt = rt
			}
			continue
		}
		byUID[e.UID] = len(out)
		out = append(out, e)
	}
	sortEnv(out)
	return out
}

func pageKey(folder, kind string) string { return folder + "\x00" + kind }

func (m *Model) listKind(i int, folder string) string {
	if i >= len(m.accts) {
		return ""
	}
	a := m.accts[i]
	if folder == a.inbox() {
		if m.showing(i, folder) && m.readView {
			return "seen"
		}
		return "unseen"
	}
	return ""
}

func (a *Acct) adjustExpect(folder, kind string, delta int) {
	if a == nil || delta == 0 {
		return
	}
	n := a.expectOf(folder, kind) + delta
	if n < 0 {
		n = 0
	}
	a.setExpect(folder, kind, n)
}

func (a *Acct) nextPageOff(folder, kind string) int {
	if a == nil || a.pageOff == nil {
		return 0
	}
	return a.pageOff[pageKey(folder, kind)]
}

func (a *Acct) setPageOff(folder, kind string, n int) {
	if a.pageOff == nil {
		a.pageOff = map[string]int{}
	}
	if n > a.pageOff[pageKey(folder, kind)] {
		a.pageOff[pageKey(folder, kind)] = n
	}
}

func (a *Acct) setExpect(folder, kind string, total int) {
	if a.expectUnseen == nil {
		a.expectUnseen, a.expectSeen, a.expectAll = map[string]int{}, map[string]int{}, map[string]int{}
	}
	switch kind {
	case "unseen":
		a.expectUnseen[folder] = total
	case "seen":
		a.expectSeen[folder] = total
	default:
		a.expectAll[folder] = total
	}
}

func (a *Acct) expectOf(folder, kind string) int {
	if a == nil {
		return 0
	}
	switch kind {
	case "unseen":
		if a.expectUnseen != nil {
			return a.expectUnseen[folder]
		}
	case "seen":
		if a.expectSeen != nil {
			return a.expectSeen[folder]
		}
	default:
		if a.expectAll != nil {
			return a.expectAll[folder]
		}
	}
	return 0
}

// listProgress is have/total for the list info line while a load is incomplete.
func (m *Model) listProgress() (have, total int, ok bool) {
	a := m.acct()
	if a == nil || m.virt != viewFolder {
		return 0, 0, false
	}
	folder := m.folder
	base := a.Lists[folder]
	if m.inboxView() {
		if m.readView {
			for _, e := range base {
				if e.Seen {
					have++
				}
			}
			total = a.expectOf(folder, "seen")
			if f := a.folder(folder); f != nil && f.Total > f.Unread && total < f.Total-f.Unread {
				total = f.Total - f.Unread
			}
		} else {
			for _, e := range base {
				if !e.Seen {
					have++
				}
			}
			total = a.expectOf(folder, "unseen")
			if f := a.folder(folder); f != nil && f.Unread > total {
				total = f.Unread
			}
		}
	} else {
		have = len(base)
		total = a.expectOf(folder, "")
		if f := a.folder(folder); f != nil && f.Total > total {
			total = f.Total
		}
	}
	if total <= have {
		return have, total, false
	}
	// Hide once paging has finished, even if the last read/delete left
	// expect one ahead of the local count (0/1 after emptying, 3679/3680
	// after marking one read).
	if !a.paging || (a.pageFolder != "" && a.pageFolder != folder) {
		return have, total, false
	}
	return have, total, true
}

// continueListLoad fetches the next page of an incomplete folder without
// aborting an in-flight load (F5 / End).
func (m *Model) continueListLoad(i int, folder string) tea.Cmd {
	if i >= len(m.accts) || m.accts[i].B == nil {
		return nil
	}
	a := m.accts[i]
	kind := m.listKind(i, folder)
	if a.syncGen == 0 {
		a.syncGen++
	}
	a.paging = true
	a.pageFolder, a.pageKind = folder, kind
	off := a.nextPageOff(folder, kind)
	if off == 0 {
		// Stuck after a first page that never recorded an offset: start
		// after whatever we already have of this kind.
		off = m.haveOf(i, folder, kind)
	}
	return m.loadListChunk(i, folder, off, false, kind, 0, a.syncGen)
}

func (m *Model) haveOf(i int, folder, kind string) int {
	if i >= len(m.accts) {
		return 0
	}
	n := 0
	for _, e := range m.accts[i].Lists[folder] {
		switch kind {
		case "unseen":
			if !e.Seen {
				n++
			}
		case "seen":
			if e.Seen {
				n++
			}
		default:
			n++
		}
	}
	return n
}

func (m *Model) listIncomplete(i int, folder string) bool {
	if i >= len(m.accts) {
		return false
	}
	a := m.accts[i]
	kind := m.listKind(i, folder)
	expect := a.expectOf(folder, kind)
	if f := a.folder(folder); f != nil {
		switch kind {
		case "unseen":
			if f.Unread > expect {
				expect = f.Unread
			}
		case "seen":
			if n := f.Total - f.Unread; n > expect {
				expect = n
			}
		default:
			if f.Total > expect {
				expect = f.Total
			}
		}
	}
	if expect == 0 {
		return a.paging && (a.pageFolder == folder || a.pageFolder == "")
	}
	return a.nextPageOff(folder, kind) < expect || m.haveOf(i, folder, kind) < expect
}

func (m *Model) kickOrReloadList(i int, folder string) tea.Cmd {
	if m.listIncomplete(i, folder) {
		return m.continueListLoad(i, folder)
	}
	return m.syncOrLoadList(i, folder)
}
