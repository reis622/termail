package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Live folder preview: moving through the folder panel shows the folder's
// mails in the middle list right away, focus stays in the panel.
//
//   - cached folder (already loaded once): shown instantly;
//   - otherwise the current list stays in place with a quiet
//     "Yükleniyor…" on its info line until the server answers;
//   - rapid moves are debounced (folderPreviewDelay): only the folder the
//     cursor rests on is fetched; answers for older requests only fill the
//     cache and never switch the view.

const folderPreviewDelay = 150 * time.Millisecond

type folderPreviewMsg struct {
	seq    int
	acct   int
	folder string
}

type pendingFolder struct {
	acct   int
	folder string
	read   bool // Okunanlar
	label  string
	seq    int // debounce generation
	req    int // list request id (0 = not sent yet)
}

// folderPreview reacts to the folder cursor landing on a row.
func (m *Model) folderPreview() tea.Cmd {
	rows := m.folderRowsSelectable()
	if m.fcursor >= len(rows) {
		return nil
	}
	r := rows[m.fcursor]
	m.previewSeq++
	switch r.kind {
	case rowFolder:
		a := m.accts[r.acct]
		if m.virt == viewFolder && m.cur == r.acct && m.folder == r.folder && m.readView == r.read && m.pendingF == nil {
			return nil
		}
		if a.Loaded[r.folder] {
			m.pendingF = nil
			m.switchFolderView(r.acct, r.folder, r.read)
			seq, ai, fo := m.previewSeq, r.acct, r.folder
			return tea.Batch(m.previewCmd(), // cache hit: instant; refresh once the cursor rests
				tea.Tick(folderPreviewDelay, func(time.Time) tea.Msg { return folderPreviewMsg{seq: seq, acct: ai, folder: fo} }))
		}
		label := r.folder
		if f := a.folder(r.folder); f != nil {
			label = f.Label()
		}
		if r.read {
			label = readTitle
		}
		m.pendingF = &pendingFolder{acct: r.acct, folder: r.folder, read: r.read, label: label, seq: m.previewSeq}
	case rowUnified, rowSaved:
		m.pendingF = nil
		focus := m.focus
		v := viewUnified
		if r.kind == rowSaved {
			v = viewSaved
		}
		cmd := m.showVirtualView(v, r.read)
		m.focus = focus
		return cmd
	default:
		m.pendingF = nil
		return nil
	}
	seq, ai, fo := m.previewSeq, r.acct, r.folder
	return tea.Tick(folderPreviewDelay, func(time.Time) tea.Msg { return folderPreviewMsg{seq: seq, acct: ai, folder: fo} })
}

// onFolderPreview fires after the debounce; only the newest one fetches.
func (m *Model) onFolderPreview(msg folderPreviewMsg) tea.Cmd {
	if msg.seq != m.previewSeq {
		return nil // superseded by a later move
	}
	p := m.pendingF
	if p == nil || p.seq != msg.seq {
		if m.virt == viewFolder && m.cur == msg.acct && m.folder == msg.folder {
			return m.loadList(msg.acct, msg.folder, false) // refresh the cached folder
		}
		return nil
	}
	m.listReq++
	p.req = m.listReq
	return m.loadListReq(p.acct, p.folder, p.req)
}

// switchFolderView shows a folder's (cached) list without moving focus.
func (m *Model) switchFolderView(ai int, folder string, read bool) {
	m.virt = viewFolder
	m.cur, m.folder = ai, folder
	m.readView = read
	m.stickySec, m.opened = nil, nil
	m.cursor, m.offset = 0, 0
	m.dropSearch()
}

// commitPending switches to the folder being previewed right away (used
// when the user moves focus into the list before it has loaded).
func (m *Model) commitPending() tea.Cmd {
	p := m.pendingF
	if p == nil {
		return nil
	}
	m.pendingF = nil
	m.switchFolderView(p.acct, p.folder, p.read)
	if p.req == 0 {
		return m.loadList(p.acct, p.folder, false)
	}
	return nil
}

// loadListReq is loadList tagged with a request id.
func (m *Model) loadListReq(i int, folder string, req int) tea.Cmd {
	c := m.loadList(i, folder, false)
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		msg := c()
		if l, ok := msg.(listMsg); ok {
			l.req = req
			return l
		}
		return msg
	}
}
